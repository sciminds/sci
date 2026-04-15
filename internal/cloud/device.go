package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sciminds/cli/internal/netutil"
)

const (
	// DefaultWorkerURL is the production auth worker endpoint.
	DefaultWorkerURL = "https://sci-auth.sciminds.workers.dev"

	// GitHubClientID is the public OAuth App client ID for the sci CLI.
	GitHubClientID = "Ov23litp5REQ1zoyeNge"
)

// DeviceCodeResponse is returned by POST /auth/device.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// TokenResponse is returned by POST /auth/token.
type TokenResponse struct {
	Status      string        `json:"status"` // "ok", "pending", "slow_down", "error"
	Message     string        `json:"message,omitempty"`
	Username    string        `json:"username,omitempty"`
	GitHubLogin string        `json:"github_login,omitempty"`
	AccountID   string        `json:"account_id,omitempty"`
	Public      *BucketConfig `json:"public,omitempty"`
}

// RequestDeviceCode initiates the device flow by calling the auth worker.
func RequestDeviceCode(ctx context.Context, workerURL string) (*DeviceCodeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, workerURL+"/auth/device", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, netutil.Wrap("contacting auth server", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth server returned %s", resp.Status)
	}

	var result DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding device code response: %w", err)
	}
	return &result, nil
}

// PollForToken polls the auth worker until the user approves, the code
// expires, or the context is cancelled.
func PollForToken(ctx context.Context, workerURL, deviceCode string, interval time.Duration) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			result, err := pollOnce(ctx, workerURL, deviceCode)
			if err != nil {
				return nil, err
			}
			switch result.Status {
			case "ok":
				return result, nil
			case "pending":
				continue
			case "slow_down":
				// Back off by 5 seconds as per the spec.
				ticker.Reset(interval + 5*time.Second)
				continue
			default:
				// "error", "expired_token", "access_denied", etc.
				msg := result.Message
				if msg == "" {
					msg = "authorization failed: " + result.Status
				}
				return nil, fmt.Errorf("%s", msg)
			}
		}
	}
}

func pollOnce(ctx context.Context, workerURL, deviceCode string) (*TokenResponse, error) {
	body, err := json.Marshal(map[string]string{"device_code": deviceCode})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, workerURL+"/auth/token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, netutil.Wrap("contacting auth server", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth server returned %s", resp.Status)
	}

	var result TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &result, nil
}
