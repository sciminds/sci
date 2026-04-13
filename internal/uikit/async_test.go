package uikit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── AsyncCmd ──────────────────────────────────────────────────────────

func TestAsyncCmdSuccess(t *testing.T) {
	cmd := AsyncCmd(func() (string, error) {
		return "hello", nil
	})
	msg := cmd()
	r, ok := msg.(Result[string])
	if !ok {
		t.Fatalf("expected Result[string], got %T", msg)
	}
	if r.Value != "hello" {
		t.Errorf("Value = %q, want %q", r.Value, "hello")
	}
	if r.Err != nil {
		t.Errorf("Err = %v, want nil", r.Err)
	}
}

func TestAsyncCmdError(t *testing.T) {
	cmd := AsyncCmd(func() (int, error) {
		return 0, errors.New("boom")
	})
	msg := cmd()
	r, ok := msg.(Result[int])
	if !ok {
		t.Fatalf("expected Result[int], got %T", msg)
	}
	if r.Err == nil || r.Err.Error() != "boom" {
		t.Errorf("Err = %v, want boom", r.Err)
	}
	if r.Value != 0 {
		t.Errorf("Value = %d, want 0", r.Value)
	}
}

// ── AsyncCmdCtx ───────────────────────────────────────────────────────

func TestAsyncCmdCtxSuccess(t *testing.T) {
	cmd := AsyncCmdCtx(context.Background(), time.Second, func(_ context.Context) (string, error) {
		return "ok", nil
	})
	msg := cmd()
	r, ok := msg.(Result[string])
	if !ok {
		t.Fatalf("expected Result[string], got %T", msg)
	}
	if r.Value != "ok" {
		t.Errorf("Value = %q, want %q", r.Value, "ok")
	}
	if r.Err != nil {
		t.Errorf("Err = %v, want nil", r.Err)
	}
}

func TestAsyncCmdCtxTimeout(t *testing.T) {
	cmd := AsyncCmdCtx(context.Background(), time.Millisecond, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	msg := cmd()
	r, ok := msg.(Result[string])
	if !ok {
		t.Fatalf("expected Result[string], got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestAsyncCmdCtxRespectsParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cmd := AsyncCmdCtx(ctx, time.Minute, func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	})
	msg := cmd()
	r, ok := msg.(Result[int])
	if !ok {
		t.Fatalf("expected Result[int], got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected canceled error, got nil")
	}
}
