package mdview

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/samber/lo"
)

var (
	rendererMu     sync.Mutex
	cachedRenderer *glamour.TermRenderer
	cachedWidth    int

	contentMu    sync.RWMutex
	contentCache = map[renderCacheKey]string{}

	styleOnce sync.Once
	styleName string // "dark" or "light", detected once before TUI starts
)

// renderCacheKey uniquely identifies a (width, content) pair for the render cache.
// Using the full content string avoids prefix-based collisions.
type renderCacheKey struct {
	width   int
	content string
}

// DetectStyle probes the terminal for dark/light background.
// Must be called before bubbletea takes over stdin, otherwise the terminal
// response escape sequences get misread as keyboard input.
func DetectStyle() {
	styleOnce.Do(func() {
		styleName = lo.Ternary(termenv.HasDarkBackground(), "dark", "light")
	})
}

// renderLocked builds (or reuses) the cached renderer and runs it.
// Caller must hold rendererMu — glamour.TermRenderer is not safe for
// concurrent use, so the lock spans the Render call itself.
func renderLocked(markdown string, width int) (string, error) {
	if cachedRenderer == nil || cachedWidth != width {
		style, _ := lo.Coalesce(styleName, "dark")
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "", err
		}
		cachedRenderer = r
		cachedWidth = width
	}
	return cachedRenderer.Render(markdown)
}

// Render converts markdown to terminal-styled output at the given width.
// Results are cached so repeated calls with the same input are instant.
func Render(markdown string, width int) (string, error) {
	key := renderCacheKey{width: width, content: markdown}

	contentMu.RLock()
	if cached, ok := contentCache[key]; ok {
		contentMu.RUnlock()
		return cached, nil
	}
	contentMu.RUnlock()

	rendererMu.Lock()
	out, err := renderLocked(markdown, width)
	rendererMu.Unlock()
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

// ── Search highlighting ─────────────────────────────────────────────────────

const (
	hlOn  = "\x1b[7m"  // reverse video on
	hlOff = "\x1b[27m" // reverse video off
)

// HighlightMatches injects reverse-video ANSI escapes around case-insensitive
// matches of query within an already-styled (glamour) string. It maps
// plain-text byte offsets back through ANSI sequences so highlights wrap the
// correct visible characters without disturbing existing styling.
func HighlightMatches(styled, query string) string {
	if query == "" {
		return styled
	}

	plain := ansi.Strip(styled)
	lowerPlain := strings.ToLower(plain)
	lowerQuery := strings.ToLower(query)

	// Collect match byte-ranges in plain text.
	type span struct{ start, end int }
	var matches []span
	pos := 0
	for {
		idx := strings.Index(lowerPlain[pos:], lowerQuery)
		if idx < 0 {
			break
		}
		s := pos + idx
		matches = append(matches, span{s, s + len(lowerQuery)})
		pos = s + len(lowerQuery)
	}
	if len(matches) == 0 {
		return styled
	}

	// Walk styled string, injecting highlight escapes at the right spots.
	var out strings.Builder
	out.Grow(len(styled) + len(matches)*(len(hlOn)+len(hlOff)))

	plainIdx := 0 // current byte offset into plain text
	mi := 0       // current match index
	inHL := false

	for i := 0; i < len(styled); {
		// Pass ANSI escape sequences through unchanged.
		if styled[i] == '\x1b' {
			seq := ansiSeqLen(styled[i:])
			if seq > 0 {
				out.WriteString(styled[i : i+seq])
				// Re-assert highlight after any SGR (glamour may reset attrs mid-match).
				if inHL {
					out.WriteString(hlOn)
				}
				i += seq
				continue
			}
		}

		// Before writing a plain byte, check whether a highlight should start.
		if !inHL && mi < len(matches) && plainIdx == matches[mi].start {
			out.WriteString(hlOn)
			inHL = true
		}

		out.WriteByte(styled[i])
		plainIdx++
		i++

		// After advancing, check whether a highlight should end.
		if inHL && mi < len(matches) && plainIdx == matches[mi].end {
			out.WriteString(hlOff)
			inHL = false
			mi++
		}
	}

	if inHL {
		out.WriteString(hlOff)
	}
	return out.String()
}

// ansiSeqLen returns the byte length of the ANSI escape sequence at the start
// of s, or 0 if s does not begin with one.
func ansiSeqLen(s string) int {
	if len(s) < 2 || s[0] != '\x1b' {
		return 0
	}
	switch s[1] {
	case '[': // CSI: ends at first byte in 0x40-0x7E
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7E {
				return i + 1
			}
		}
		return len(s)
	case ']': // OSC: ends at BEL or ST
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default:
		return 2
	}
}
