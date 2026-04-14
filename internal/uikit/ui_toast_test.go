package uikit

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── ToastLevel ─────────────────────────────────────────────────────────────

func TestToastLevel_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level ToastLevel
		want  string
	}{
		{ToastInfo, "info"},
		{ToastSuccess, "success"},
		{ToastWarning, "warning"},
		{ToastError, "error"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("ToastLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestToastLevel_Icon(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level ToastLevel
		want  string
	}{
		{ToastInfo, IconArrow},
		{ToastSuccess, IconPass},
		{ToastWarning, IconWarn},
		{ToastError, IconFail},
	}
	for _, tt := range tests {
		if got := tt.level.Icon(); got != tt.want {
			t.Errorf("ToastLevel(%d).Icon() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// ── Toast value type ───────────────────────────────────────────────────────

func TestToast_Builders(t *testing.T) {
	t.Parallel()

	toast := Toast{Message: "hello", Level: ToastInfo, Duration: time.Second}

	got := toast.WithLevel(ToastError)
	if got.Level != ToastError {
		t.Errorf("WithLevel: got %v, want %v", got.Level, ToastError)
	}
	if got.Message != "hello" {
		t.Error("WithLevel should preserve Message")
	}

	got2 := toast.WithMessage("bye")
	if got2.Message != "bye" {
		t.Errorf("WithMessage: got %q, want %q", got2.Message, "bye")
	}

	got3 := toast.WithDuration(5 * time.Second)
	if got3.Duration != 5*time.Second {
		t.Errorf("WithDuration: got %v, want %v", got3.Duration, 5*time.Second)
	}
}

// ── ToastModel ─────────────────────────────────────────────────────────────

func TestNewToastModel_Empty(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	if m.Active() {
		t.Error("new model should not be active")
	}
	if got := m.View(80); got != "" {
		t.Errorf("empty model View should be empty, got %q", got)
	}
}

func TestToastModel_Push(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	toast := Toast{Message: "saved", Level: ToastSuccess, Duration: 3 * time.Second}
	m, cmd := m.Push(toast)

	if !m.Active() {
		t.Error("model should be active after Push")
	}
	if cmd == nil {
		t.Error("Push should return a tick Cmd")
	}
}

func TestToastModel_ViewContainsMessage(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "file saved", Level: ToastSuccess, Duration: time.Minute})

	got := m.View(80)
	if !strings.Contains(got, "file saved") {
		t.Errorf("View should contain message, got %q", got)
	}
}

func TestToastModel_ViewContainsIcon(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "ok", Level: ToastSuccess, Duration: time.Minute})

	got := m.View(80)
	if !strings.Contains(got, IconPass) {
		t.Errorf("View should contain success icon %q, got %q", IconPass, got)
	}
}

func TestToastModel_MultipleToasts(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "first", Level: ToastInfo, Duration: time.Minute})
	m, _ = m.Push(Toast{Message: "second", Level: ToastWarning, Duration: time.Minute})

	got := m.View(80)
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("View should contain both messages, got %q", got)
	}
}

func TestToastModel_MaxVisible(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m.MaxVisible = 2
	m, _ = m.Push(Toast{Message: "a", Level: ToastInfo, Duration: time.Minute})
	m, _ = m.Push(Toast{Message: "b", Level: ToastInfo, Duration: time.Minute})
	m, _ = m.Push(Toast{Message: "c", Level: ToastInfo, Duration: time.Minute})

	got := m.View(80)
	// Only the two newest ("b" and "c") should render.
	if strings.Contains(got, "a") {
		t.Error("oldest toast should be hidden when MaxVisible exceeded")
	}
	if !strings.Contains(got, "b") || !strings.Contains(got, "c") {
		t.Errorf("newest toasts should be visible, got %q", got)
	}
}

func TestToastModel_TickExpiresOld(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	// Push a toast with a very short duration, then a long one.
	m, _ = m.Push(Toast{Message: "ephemeral", Level: ToastInfo, Duration: 10 * time.Millisecond})
	m, _ = m.Push(Toast{Message: "sticky", Level: ToastInfo, Duration: time.Minute})

	// Simulate time passing — set the short toast's createdAt to the past.
	m.toasts[0].createdAt = time.Now().Add(-time.Second)

	// Send a toastTickMsg.
	m, _ = m.Update(toastTickMsg{})

	got := m.View(80)
	if strings.Contains(got, "ephemeral") {
		t.Error("expired toast should be removed after tick")
	}
	if !strings.Contains(got, "sticky") {
		t.Error("non-expired toast should survive tick")
	}
}

func TestToastModel_TickReturnsNilWhenEmpty(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "gone", Level: ToastInfo, Duration: 10 * time.Millisecond})
	m.toasts[0].createdAt = time.Now().Add(-time.Second)

	var cmd tea.Cmd
	m, cmd = m.Update(toastTickMsg{})

	if m.Active() {
		t.Error("model should not be active after all toasts expired")
	}
	if cmd != nil {
		t.Error("tick should return nil cmd when no toasts remain")
	}
}

func TestToastModel_TickContinuesWhenActive(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "alive", Level: ToastInfo, Duration: time.Minute})

	var cmd tea.Cmd
	m, cmd = m.Update(toastTickMsg{})

	if !m.Active() {
		t.Error("model should remain active")
	}
	if cmd == nil {
		t.Error("tick should schedule another tick when toasts remain")
	}
}

func TestToastModel_Dismiss(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "a", Level: ToastInfo, Duration: time.Minute})
	m, _ = m.Push(Toast{Message: "b", Level: ToastInfo, Duration: time.Minute})

	m = m.Dismiss()
	got := m.View(80)
	if strings.Contains(got, "b") {
		t.Error("newest toast should be dismissed")
	}
	if !strings.Contains(got, "a") {
		t.Error("older toast should remain")
	}
}

func TestToastModel_DismissAll(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "a", Level: ToastInfo, Duration: time.Minute})
	m, _ = m.Push(Toast{Message: "b", Level: ToastInfo, Duration: time.Minute})

	m = m.DismissAll()
	if m.Active() {
		t.Error("model should not be active after DismissAll")
	}
}

func TestToastModel_ViewZeroWidth(t *testing.T) {
	t.Parallel()
	m := NewToastModel()
	m, _ = m.Push(Toast{Message: "hello", Level: ToastInfo, Duration: time.Minute})

	// Zero or negative width should not panic.
	_ = m.View(0)
}
