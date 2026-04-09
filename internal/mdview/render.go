package mdview

import (
	"fmt"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
)

var (
	rendererMu     sync.Mutex
	cachedRenderer *glamour.TermRenderer
	cachedWidth    int

	contentMu    sync.RWMutex
	contentCache = map[string]string{} // key: "width:markdown" hash → rendered

	styleOnce sync.Once
	styleName string // "dark" or "light", detected once before TUI starts
)

func cacheKey(markdown string, width int) string {
	// Use length + prefix as a fast-enough key; collisions are harmless (just a re-render).
	prefix := markdown
	if len(prefix) > 128 {
		prefix = prefix[:128]
	}
	return fmt.Sprintf("%d:%d:%s", width, len(markdown), prefix)
}

// DetectStyle probes the terminal for dark/light background.
// Must be called before bubbletea takes over stdin, otherwise the terminal
// response escape sequences get misread as keyboard input.
func DetectStyle() {
	styleOnce.Do(func() {
		if termenv.HasDarkBackground() {
			styleName = "dark"
		} else {
			styleName = "light"
		}
	})
}

func getRenderer(width int) (*glamour.TermRenderer, error) {
	rendererMu.Lock()
	defer rendererMu.Unlock()

	if cachedRenderer != nil && cachedWidth == width {
		return cachedRenderer, nil
	}

	// Fall back to dark if DetectStyle was never called.
	style := styleName
	if style == "" {
		style = "dark"
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	cachedRenderer = r
	cachedWidth = width
	return r, nil
}

// Render converts markdown to terminal-styled output at the given width.
// Results are cached so repeated calls with the same input are instant.
func Render(markdown string, width int) (string, error) {
	key := cacheKey(markdown, width)

	contentMu.RLock()
	if cached, ok := contentCache[key]; ok {
		contentMu.RUnlock()
		return cached, nil
	}
	contentMu.RUnlock()

	r, err := getRenderer(width)
	if err != nil {
		return "", err
	}
	out, err := r.Render(markdown)
	if err != nil {
		return "", err
	}

	contentMu.Lock()
	contentCache[key] = out
	contentMu.Unlock()

	return out, nil
}

// PreRender renders and caches multiple markdown documents at the given width.
// Intended to be called from a background goroutine so results are warm
// by the time the user needs them.
func PreRender(docs []string, width int) {
	for _, md := range docs {
		_, _ = Render(md, width)
	}
}
