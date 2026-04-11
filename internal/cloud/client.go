package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Client wraps the S3 client for R2 operations.
type Client struct {
	S3       *s3.Client
	Bucket   string
	Username string
	// PublicURL is the r2.dev base URL for public downloads (no trailing slash).
	PublicURL string
}

// NewClient creates an R2 client for a specific bucket.
func NewClient(accountID, username string, bucket *BucketConfig) *Client {
	bucketName := bucket.BucketName
	if bucketName == "" {
		bucketName = defaultBucket
	}

	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	s3Client := s3.New(s3.Options{
		Region: "auto",
		Credentials: credentials.NewStaticCredentialsProvider(
			bucket.AccessKey, bucket.SecretKey, "",
		),
		BaseEndpoint: aws.String(endpoint),
	})

	return &Client{
		S3:        s3Client,
		Bucket:    bucketName,
		Username:  username,
		PublicURL: bucket.PublicURL,
	}
}

// Setup loads config and returns a client for the public bucket.
func Setup() (*Config, *Client, error) {
	cfg, err := RequireConfig()
	if err != nil {
		return nil, nil, err
	}
	if cfg.Public == nil || cfg.Public.AccessKey == "" {
		return nil, nil, fmt.Errorf("public bucket not configured — run 'sci cloud setup' to reconfigure")
	}
	return cfg, NewClient(cfg.AccountID, cfg.Username, cfg.Public), nil
}

// SetupBoard loads config and returns a client for the private board bucket.
// Returns a friendly error if the board credentials haven't been populated
// yet — older credentials files predate the board bucket and need a fresh
// login to pick it up.
func SetupBoard() (*Config, *Client, error) {
	cfg, err := RequireConfig()
	if err != nil {
		return nil, nil, err
	}
	if cfg.Board == nil || cfg.Board.AccessKey == "" {
		return nil, nil, fmt.Errorf("board bucket not configured — run 'sci cloud login' to refresh credentials")
	}
	return cfg, NewClient(cfg.AccountID, cfg.Username, cfg.Board), nil
}

// objectKey returns the full key for a file: "<username>/<filename>".
func (c *Client) objectKey(filename string) string {
	return c.Username + "/" + filename
}

// PublicObjectURL returns the public download URL for a file.
func (c *Client) PublicObjectURL(filename string) string {
	if c.PublicURL != "" {
		return c.PublicURL + "/" + c.objectKey(filename)
	}
	return fmt.Sprintf("https://%s.r2.cloudflarestorage.com/%s/%s", c.Bucket, c.Bucket, c.objectKey(filename))
}

// Upload uploads a file to R2 with optional user metadata.
func (c *Client) Upload(ctx context.Context, filename string, body io.Reader, contentType string, metadata map[string]string) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(c.objectKey(filename)),
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if len(metadata) > 0 {
		input.Metadata = metadata
	}
	_, err := c.S3.PutObject(ctx, input)
	return err
}

// HeadObject returns user metadata for a single object.
func (c *Client) HeadObject(ctx context.Context, filename string) (map[string]string, error) {
	resp, err := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(c.objectKey(filename)),
	})
	if err != nil {
		return nil, err
	}
	return resp.Metadata, nil
}

// Download streams an object from R2 into dst.
func (c *Client) Download(ctx context.Context, filename string, dst io.Writer) error {
	resp, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(c.objectKey(filename)),
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(dst, resp.Body)
	return err
}

// DownloadByKey streams an object by full key (for cross-user downloads).
func (c *Client) DownloadByKey(ctx context.Context, key string, dst io.Writer) error {
	resp, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(dst, resp.Body)
	return err
}

// Delete removes an object from R2.
func (c *Client) Delete(ctx context.Context, filename string) error {
	_, err := c.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(c.objectKey(filename)),
	})
	return err
}

// List returns objects under the current user's prefix.
func (c *Client) List(ctx context.Context) ([]ObjectInfo, error) {
	prefix := c.Username + "/"
	return c.ListPrefix(ctx, prefix)
}

// ListPrefix returns objects under an arbitrary prefix.
func (c *Client) ListPrefix(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.Bucket),
		Prefix: aws.String(prefix),
	}

	var objects []ObjectInfo
	paginator := s3.NewListObjectsV2Paginator(c.S3, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			url := ""
			if c.PublicURL != "" {
				url = c.PublicURL + "/" + aws.ToString(obj.Key)
			}
			modified := ""
			if obj.LastModified != nil {
				modified = obj.LastModified.Format(time.RFC3339)
			}
			objects = append(objects, ObjectInfo{
				Key:          aws.ToString(obj.Key),
				Size:         aws.ToInt64(obj.Size),
				LastModified: modified,
				URL:          url,
			})
		}
	}
	return objects, nil
}

// Exists checks whether an object exists.
func (c *Client) Exists(ctx context.Context, filename string) (bool, error) {
	_, err := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(c.objectKey(filename)),
	})
	if err != nil {
		var nsk *types.NotFound
		if ok := errors.As(err, &nsk); ok {
			return false, nil
		}
		// Also check for NoSuchKey via the API error code.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
