package lab

import (
	"bufio"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

func TestScanTransferOutputParsesProgressAndCapsTail(t *testing.T) {
	input := strings.Join([]string{
		"sending incremental file list",
		"      1,234,567  42%   12.34MB/s    0:00:05",
		"file-1",
		"file-2",
		"file-3",
		"file-4",
		"file-5",
		"file-6",
		"file-7",
		"file-8",
		"file-9",
		"  9,999,999 100%  100.00MB/s    0:00:00 (xfr#3, to-chk=0/3)",
	}, "\r")
	progress := make(chan Progress, 2)

	tail, err := scanTransferOutput(context.Background(), strings.NewReader(input), progress)
	if err != nil {
		t.Fatalf("scanTransferOutput error = %v, want nil", err)
	}

	wantFirst := Progress{Bytes: 1234567, Percent: 42, Rate: "12.34MB/s", ETA: "0:00:05"}
	if got := <-progress; got != wantFirst {
		t.Errorf("first progress = %#v, want %#v", got, wantFirst)
	}
	wantLast := Progress{Bytes: 9999999, Percent: 100, Rate: "100.00MB/s", ETA: "0:00:00"}
	if got := <-progress; got != wantLast {
		t.Errorf("last progress = %#v, want %#v", got, wantLast)
	}
	wantTail := []string{"file-2", "file-3", "file-4", "file-5", "file-6", "file-7", "file-8", "file-9"}
	if !slices.Equal(tail, wantTail) {
		t.Errorf("tail = %#v, want %#v", tail, wantTail)
	}
}

func TestScanTransferOutputReturnsReaderErrors(t *testing.T) {
	errRead := errors.New("read failed")
	tests := []struct {
		name   string
		reader io.Reader
		want   error
	}{
		{name: "error reader", reader: iotest.ErrReader(errRead), want: errRead},
		{name: "timeout reader", reader: iotest.TimeoutReader(strings.NewReader("diagnostic\n")), want: iotest.ErrTimeout},
		{name: "token too long", reader: strings.NewReader(strings.Repeat("x", bufio.MaxScanTokenSize+1)), want: bufio.ErrTooLong},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scanTransferOutput(context.Background(), tc.reader, make(chan Progress))
			if !errors.Is(err, tc.want) {
				t.Fatalf("scanTransferOutput error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestScanTransferOutputReturnsWhenCanceledWithBlockedProgress(t *testing.T) {
	ctx := newSendBlockingContext()
	result := make(chan error, 1)
	go func() {
		_, err := scanTransferOutput(ctx, strings.NewReader("1,234 42% 12.34MB/s 0:00:05\n"), make(chan Progress))
		result <- err
	}()

	select {
	case <-ctx.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("scanTransferOutput did not reach the blocked progress send")
	}
	ctx.cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("scanTransferOutput error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scanTransferOutput did not return after cancellation")
	}
}

func TestResolveTransferErrorOrdering(t *testing.T) {
	waitErr := errors.New("wait failed")
	scanErr := errors.New("scan failed")

	t.Run("context cancellation wins", func(t *testing.T) {
		err := resolveTransferError("/remote", context.Canceled, waitErr, scanErr, "stderr detail", []string{"stdout detail"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if errors.Is(err, waitErr) || errors.Is(err, scanErr) {
			t.Fatalf("cancellation should win without wrapping secondary errors: %v", err)
		}
	})

	t.Run("wait error wins and mentions scan error", func(t *testing.T) {
		err := resolveTransferError("/remote", nil, waitErr, scanErr, "stderr detail", []string{"stdout detail"})
		if !errors.Is(err, waitErr) {
			t.Fatalf("error = %v, want wrapped wait error", err)
		}
		if !errors.Is(err, scanErr) {
			t.Fatalf("error = %v, want wrapped secondary scan error", err)
		}
		if !strings.Contains(err.Error(), scanErr.Error()) {
			t.Fatalf("error %q should mention scan error %q", err, scanErr)
		}
		if !strings.Contains(err.Error(), "stderr detail") {
			t.Fatalf("error %q should include stderr detail", err)
		}
	})

	t.Run("scan error fails successful process", func(t *testing.T) {
		err := resolveTransferError("/remote", nil, nil, scanErr, "", []string{"stdout detail"})
		if !errors.Is(err, scanErr) {
			t.Fatalf("error = %v, want wrapped scan error", err)
		}
		if !strings.Contains(err.Error(), "stdout detail") {
			t.Fatalf("error %q should include stdout tail", err)
		}
	})
}

type sendBlockingContext struct {
	done        chan struct{}
	sendStarted chan struct{}
	once        sync.Once
}

func newSendBlockingContext() *sendBlockingContext {
	return &sendBlockingContext{
		done:        make(chan struct{}),
		sendStarted: make(chan struct{}),
	}
}

func (c *sendBlockingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *sendBlockingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.sendStarted) })
	return c.done
}

func (c *sendBlockingContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (c *sendBlockingContext) Value(any) any { return nil }

func (c *sendBlockingContext) cancel() { close(c.done) }
