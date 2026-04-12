package app

// table_list_browse.go — file browser sub-feature of the table list overlay.
// Lets the user navigate the filesystem and select a CSV/TSV to import as a
// new table.

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// importableExts lists file extensions shown in the file browser.
var importableExts = map[string]bool{".csv": true, ".tsv": true}

// tableListAdd opens the file browser so the user can select a CSV to import.
func (m *Model) tableListAdd() tea.Cmd {
	tl := m.tableList
	if tl == nil {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		tl.Status = fmt.Sprintf("Cannot read cwd: %v", err)
		return nil
	}

	browser, err := newFileBrowser(cwd)
	if err != nil {
		tl.Status = fmt.Sprintf("Cannot list files: %v", err)
		return nil
	}

	tl.Adding = true
	tl.Browser = browser
	tl.Status = ""
	return nil
}

// newFileBrowser reads a directory and returns only dirs + importable files.
func newFileBrowser(dir string) (*fileBrowserState, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []fileBrowserEntry
	for _, de := range dirEntries {
		// Skip hidden files.
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		if de.IsDir() {
			if dirHasImportable(filepath.Join(dir, de.Name())) {
				entries = append(entries, fileBrowserEntry{Name: de.Name(), IsDir: true})
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(de.Name()))
		if importableExts[ext] {
			info, err := de.Info()
			size := int64(0)
			if err == nil {
				size = info.Size()
			}
			entries = append(entries, fileBrowserEntry{Name: de.Name(), Size: size})
		}
	}

	// Sort: directories first, then files, alphabetical within each group.
	slices.SortFunc(entries, func(a, b fileBrowserEntry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})

	return &fileBrowserState{Dir: dir, Entries: entries}, nil
}

// dirHasImportable returns true if dir (recursively) contains at least one
// importable file. Stops early on first match and skips hidden directories.
func dirHasImportable(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if !d.IsDir() && importableExts[strings.ToLower(filepath.Ext(d.Name()))] {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// handleTableListAddKey handles key events in the file browser.
func (m *Model) handleTableListAddKey(msg tea.KeyPressMsg) tea.Cmd {
	tl := m.tableList
	if tl == nil || tl.Browser == nil {
		return nil
	}
	br := tl.Browser

	switch msg.String() {
	case keyEsc:
		tl.Adding = false
		tl.Browser = nil
		return nil

	case keyJ, keyDown:
		if br.Cursor < len(br.Entries)-1 {
			br.Cursor++
			tl.Status = ""
		}

	case keyK, keyUp:
		if br.Cursor > 0 {
			br.Cursor--
			tl.Status = ""
		}

	case keyH, keyLeft, keyBackspace:
		// Navigate up.
		parent := filepath.Dir(br.Dir)
		if parent == br.Dir {
			return nil // already at root
		}
		prev := filepath.Base(br.Dir)
		nb, err := newFileBrowser(parent)
		if err != nil {
			tl.Status = fmt.Sprintf("Cannot read dir: %v", err)
			return nil
		}
		tl.Browser = nb
		// Position cursor on the directory we came from.
		for i, e := range nb.Entries {
			if e.IsDir && e.Name == prev {
				nb.Cursor = i
				break
			}
		}

	case keyL, keyRight, keyEnter:
		if len(br.Entries) == 0 {
			return nil
		}
		entry := br.Entries[br.Cursor]
		if entry.IsDir {
			nb, err := newFileBrowser(filepath.Join(br.Dir, entry.Name))
			if err != nil {
				tl.Status = fmt.Sprintf("Cannot read dir: %v", err)
				return nil
			}
			tl.Browser = nb
		} else {
			m.tableListImportFile(filepath.Join(br.Dir, entry.Name))
		}
	}

	return nil
}

// tableListImportFile imports the selected file as a new table.
func (m *Model) tableListImportFile(path string) {
	tl := m.tableList
	if tl == nil {
		return
	}

	// Derive table name from filename stem.
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Check for collision.
	for _, e := range tl.Tables {
		if e.Name == name {
			tl.Status = fmt.Sprintf("Table %q already exists", name)
			return
		}
	}

	if err := m.store.ImportFile(path, name); err != nil {
		tl.Status = fmt.Sprintf("Import failed: %v", err)
		return
	}

	// Rebuild the tab for the new table.
	newTab, err := buildTab(m.store, name)
	if err != nil {
		tl.Status = fmt.Sprintf("Import ok, load failed: %v", err)
		return
	}
	m.tabs = append(m.tabs, newTab)
	m.resizeTables()

	// Refresh overlay entries.
	entries, err := m.buildTableListEntries()
	if err == nil {
		tl.Tables = entries
		// Position cursor on the new table.
		for i, e := range entries {
			if e.Name == name {
				tl.Cursor = i
				break
			}
		}
	}

	// Exit file browser.
	tl.Adding = false
	tl.Browser = nil
	tl.Status = fmt.Sprintf("Imported %q from %s", name, base)
}

// buildAddFileOverlay renders the file browser sub-view within the table list overlay.
func (m *Model) buildAddFileOverlay(contentW, innerW int) string {
	tl := m.tableList
	br := tl.Browser
	if br == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.styles.HeaderSection().Render(" Add Table "))

	// Show current directory.
	dirLabel := truncateLeft(shortenHome(br.Dir), innerW)
	b.WriteString("  ")
	b.WriteString(m.styles.HeaderHint().Render(dirLabel))
	b.WriteString("\n\n")

	if len(br.Entries) == 0 {
		b.WriteString(m.styles.Empty().Render("No importable files"))
		b.WriteString("\n")
	} else {
		// Compute max name width for alignment.
		maxNameW := 0
		for _, e := range br.Entries {
			if len(e.Name) > maxNameW {
				maxNameW = len(e.Name)
			}
		}
		if maxNameW > innerW-fileBrowserNameAlignReserve {
			maxNameW = innerW - fileBrowserNameAlignReserve
		}

		// Visible window.
		maxVisible := ui.OverlayBodyHeight(m.height, fileBrowserExtraChrome)
		if maxVisible > len(br.Entries) {
			maxVisible = len(br.Entries)
		}
		start := br.Cursor - maxVisible/2
		if start < 0 {
			start = 0
		}
		end := start + maxVisible
		if end > len(br.Entries) {
			end = len(br.Entries)
			start = end - maxVisible
			if start < 0 {
				start = 0
			}
		}

		for i := start; i < end; i++ {
			entry := br.Entries[i]
			selected := i == br.Cursor

			name := entry.Name
			if len(name) > maxNameW {
				name = name[:maxNameW-1] + symEllipsis
			}

			// Size label (right-aligned, files only).
			var sizeLabel string
			if entry.IsDir {
				name += "/"
			} else {
				sizeLabel = humanize.Bytes(uint64(entry.Size)) //nolint:gosec
			}

			if selected {
				pointer := m.styles.AccentBold().Render(symTriRight + " ")
				if entry.IsDir {
					nameStyled := m.styles.AccentBold().Render(name)
					b.WriteString(pointer + nameStyled)
				} else {
					nameStyled := m.styles.AccentBold().Render(name)
					b.WriteString(pointer + nameStyled)
				}
			} else {
				if entry.IsDir {
					b.WriteString("  " + m.styles.Info().Render(name))
				} else {
					b.WriteString("  " + name)
				}
			}

			if sizeLabel != "" {
				// Pad between name and size.
				nameW := lipgloss.Width(name) + 2 // 2 for pointer/indent
				gap := innerW - nameW - lipgloss.Width(sizeLabel) - 1
				if gap < 1 {
					gap = 1
				}
				b.WriteString(strings.Repeat(" ", gap))
				b.WriteString(m.styles.HeaderHint().Render(sizeLabel))
			}

			if i < end-1 {
				b.WriteString("\n")
			}
		}
	}

	// Status message.
	b.WriteString("\n\n")
	if tl.Status != "" {
		b.WriteString(m.styles.Info().Render(tl.Status))
	} else {
		b.WriteString(m.styles.HeaderHint().Render("Select a CSV or TSV file"))
	}
	b.WriteString("\n\n")

	// Hints.
	hints := []string{
		m.helpItem(keyEnter, "import"),
		m.helpItem(keyEsc, "back"),
	}
	b.WriteString(joinWithSeparator(m.helpSeparator(), hints...))

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}
