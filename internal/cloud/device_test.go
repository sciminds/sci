package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestDeviceCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/device" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
			DeviceCode:      "dc_test123",
			UserCode:        "ABCD-1234",
			VerificationURI: "https://github.com/login/device",
			ExpiresIn:       900,
			Interval:        5,
		})
	}))
	defer srv.Close()

	resp, err := RequestDeviceCode(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeviceCode != "dc_test123" {
		t.Errorf("DeviceCode = %q, want %q", resp.DeviceCode, "dc_test123")
	}
	if resp.UserCode != "ABCD-1234" {
		t.Errorf("UserCode = %q, want %q", resp.UserCode, "ABCD-1234")
	}
	if resp.Interval != 5 {
		t.Errorf("Interval = %d, want 5", resp.Interval)
	}
}

func TestPollForToken_Success(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/token" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if n < 3 {
			// First two calls: pending.
			_ = json.NewEncoder(w).Encode(TokenResponse{Status: "pending"})
			return
		}
		// Third call: success.
		_ = json.NewEncoder(w).Encode(TokenResponse{
			Status:      "ok",
			Username:    "alice",
			GitHubLogin: "alice",
			AccountID:   "acct123",
			Public: &BucketConfig{
				AccessKey:  "AKPUB",
				SecretKey:  "SKPUB",
				BucketName: "sci-public",
				PublicURL:  "https://pub.r2.dev",
			},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := PollForToken(ctx, srv.URL, "dc_test", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want %q", resp.Status, "ok")
	}
	if resp.Username != "alice" {
		t.Errorf("Username = %q, want %q", resp.Username, "alice")
	}
	if resp.Public == nil || resp.Public.AccessKey != "AKPUB" {
		t.Error("Public bucket not returned correctly")
	}
	if c := calls.Load(); c != 3 {
		t.Errorf("expected 3 poll calls, got %d", c)
	}
}

func TestPollForToken_Denied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TokenResponse{
			Status:  "error",
			Message: "not a member of sciminds org",
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := PollForToken(ctx, srv.URL, "dc_test", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for denied auth")
	}
	if got := err.Error(); got != "not a member of sciminds org" {
		t.Errorf("error = %q, want denial message", got)
	}
}

func TestPollForToken_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TokenResponse{Status: "pending"})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := PollForToken(ctx, srv.URL, "dc_test", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error on timeout")
	}
}
