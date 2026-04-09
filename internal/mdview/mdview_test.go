package mdview

import (
	"bytes"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	testW    = 100
	testH    = 30
	testWait = 2 * time.Second
	testFin  = 3 * time.Second
)

var testPages = []Page{
	{Name: "alpha", Content: "# Alpha\n\nFirst page."},
	{Name: "beta", Content: "# Beta\n\nSecond page."},
}

func startSinglePage(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := New([]Page{{Name: "test", Content: "# Test\n\nHello world."}})
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testW, testH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Hello"))
	}, teatest.WithDuration(testWait))
	return tm
}

func startMultiPage(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := New(testPages)
	m.initPicker()
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testW, testH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("alpha"))
	}, teatest.WithDuration(testWait))
	return tm
}

func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	return tm.FinalModel(t, teatest.WithFinalTimeout(testFin)).(*Model)
}

func TestSinglePageRenders(t *testing.T) {
	tm := startSinglePage(t)
	fm := finalModel(t, tm)
	if fm.level != levelViewer {
		t.Errorf("single page should start at viewer level, got %d", fm.level)
	}
	if fm.multi {
		t.Error("single page should not be in multi mode")
	}
}

func TestMultiPageShowsPicker(t *testing.T) {
	tm := startMultiPage(t)
	fm := finalModel(t, tm)
	if fm.level != levelPicker {
		t.Errorf("multi page should start at picker level, got %d", fm.level)
	}
}

func TestMultiPageOpenAndClose(t *testing.T) {
	tm := startMultiPage(t)

	// Open first page
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Alpha"))
	}, teatest.WithDuration(testWait))

	// Close with esc
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("alpha"))
	}, teatest.WithDuration(testWait))

	fm := finalModel(t, tm)
	if fm.level != levelPicker {
		t.Errorf("should be back at picker after esc, got %d", fm.level)
	}
}

func TestViewerScrollPercent(t *testing.T) {
	v := NewViewer("test", "# Test\n\nLine 1\nLine 2\nLine 3")
	v.SetSize(80, 50) // tall enough to fit all content
	pct := v.ScrollPercent()
	if pct != 100 {
		t.Errorf("short content should be 100%%, got %d%%", pct)
	}
}
