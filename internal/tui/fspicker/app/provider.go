package app

// provider.go — browser.Provider over os.ReadDir. Hidden files (names
// beginning with '.') are filtered by default and toggled via the
// toggle-hidden action, which flips State.ShowHidden and emits a
// RefreshMsg so the listing re-renders.
//
// State is a small struct shared between the Provider (which reads
// ShowHidden during Children) and the actions (which write to Picked
// and toggle ShowHidden). The root fspicker package reads State.Picked
// after the Bubbletea program exits to know what the user chose.

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"

	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/uikit/browser"
)

// State holds picker state shared between the provider and the actions.
type State struct {
	mu         sync.Mutex
	showHidden bool

	// Picked is the absolute path the user selected, or "" if they
	// quit without picking. Written by the upload action on the
	// Bubbletea event-loop goroutine; read by the root fspicker
	// package after the program exits.
	Picked string

	// Force is set by the force-upload action (uppercase U). The
	// caller uses it to skip the overwrite confirmation.
	Force bool
}

// ShowHidden returns whether hidden files should be listed.
func (s *State) ShowHidden() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.showHidden
}

// ToggleHidden flips ShowHidden and returns the new value.
func (s *State) ToggleHidden() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.showHidden = !s.showHidden
	return s.showHidden
}

// Provider implements browser.Provider against a real filesystem rooted
// at root. Parent clamps at the actual filesystem root; users can
// browse upward past the starting directory if they want to.
type Provider struct {
	root   string
	home   string                 // for breadcrumb shortening (~/...)
	filter func(os.DirEntry) bool // optional; nil = no filter
	state  *State
}

// NewProvider wraps root. filter may be nil. state must be non-nil.
func NewProvider(root string, filter func(os.DirEntry) bool, state *State) *Provider {
	home, _ := os.UserHomeDir()
	return &Provider{root: root, home: home, filter: filter, state: state}
}

// Root is the directory the picker opened at.
func (p *Provider) Root() string { return p.root }

// Parent returns the parent of path, clamped at the filesystem root.
func (p *Provider) Parent(path string) string {
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return parent
}

// Breadcrumb collapses the home prefix to "~" for readability.
func (p *Provider) Breadcrumb(path string) string {
	if p.home != "" {
		if path == p.home {
			return "~"
		}
		if strings.HasPrefix(path, p.home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(path, p.home)
		}
	}
	return path
}

// Children reads path with os.ReadDir, filters and sorts entries, and
// emits a browser.ChildrenMsg. Errors surface as a status toast — the
// previous listing stays in place per the browser primitive's contract.
func (p *Provider) Children(path string) tea.Cmd {
	return uikit.SafeCmd(func() tea.Msg {
		des, err := os.ReadDir(path)
		if err != nil {
			return browser.ChildrenMsg{Path: path, Err: err}
		}
		show := p.state.ShowHidden()
		kept := lo.Filter(des, func(de os.DirEntry, _ int) bool {
			if !show && strings.HasPrefix(de.Name(), ".") {
				return false
			}
			if p.filter != nil && !p.filter(de) {
				return false
			}
			return true
		})
		slices.SortStableFunc(kept, func(a, b os.DirEntry) int {
			if a.IsDir() != b.IsDir() {
				if a.IsDir() {
					return -1
				}
				return 1
			}
			return cmp.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
		})
		entries := lo.Map(kept, func(de os.DirEntry, _ int) browser.Entry {
			info, _ := de.Info() // best-effort; nil on broken symlinks
			return Entry{
				Abs:  filepath.Join(path, de.Name()),
				Name: de.Name(),
				Dir:  de.IsDir(),
				Info: info,
			}
		})
		return browser.ChildrenMsg{Path: path, Entries: entries}
	})
}
