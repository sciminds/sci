package board

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sciminds/cli/internal/cloud"
)

// CloudAdapter implements [ObjectStore] against a [cloud.Client]. It uses
// the raw S3 client directly so keys are passed through untouched — unlike
// cloud.Client's Upload/Download which auto-prefix with "{username}/".
type CloudAdapter struct {
	client *cloud.Client
}

// NewCloudAdapter wraps a cloud.Client (typically returned by
// [cloud.SetupBoard]) as an ObjectStore.
func NewCloudAdapter(c *cloud.Client) *CloudAdapter {
	return &CloudAdapter{client: c}
}

func (a *CloudAdapter) PutObject(ctx context.Context, key string, body []byte, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(a.client.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	_, err := a.client.S3.PutObject(ctx, input)
	return err
}

func (a *CloudAdapter) GetObject(ctx context.Context, key string) ([]byte, error) {
	resp, err := a.client.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.client.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, fmt.Errorf("not found: %s", key)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func (a *CloudAdapter) DeleteObject(ctx context.Context, key string) error {
	_, err := a.client.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(a.client.Bucket),
		Key:    aws.String(key),
	})
	return err
}

func (a *CloudAdapter) ListObjects(ctx context.Context, prefix, startAfter string) ([]string, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(a.client.Bucket),
		Prefix: aws.String(prefix),
	}
	if startAfter != "" {
		input.StartAfter = aws.String(startAfter)
	}
	var out []string
	paginator := s3.NewListObjectsV2Paginator(a.client.S3, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			out = append(out, aws.ToString(obj.Key))
		}
	}
	return out, nil
}

func (a *CloudAdapter) ListCommonPrefixes(ctx context.Context, prefix, delimiter string) ([]string, error) {
	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(a.client.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(delimiter),
	}
	var out []string
	paginator := s3.NewListObjectsV2Paginator(a.client.S3, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, cp := range page.CommonPrefixes {
			out = append(out, aws.ToString(cp.Prefix))
		}
	}
	return out, nil
}
