package mdview

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/ui"
)

// Run launches the markdown viewer TUI for a file or directory of .md files.
func Run(path string) error {
	DetectStyle() // probe terminal before bubbletea takes over stdin

	pages, err := loadPages(path)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return fmt.Errorf("no markdown files found in %s", path)
	}

	m := New(pages)
	if m.multi {
		m.initPicker()
	}
	p := tea.NewProgram(m)
	_, err = p.Run()
	ui.DrainStdin()
	return err
}

func (m *Model) initPicker() {
	items := lo.Map(m.pages, func(p Page, _ int) list.Item { return p })
	d := ui.NewListDelegate()
	l := list.New(items, d, 0, 0)
	l.Title = "Markdown Files"
	l.Styles.Title = ui.TUI.AccentBold()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	m.picker = l
}

func loadPages(path string) ([]Page, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return []Page{{Name: name, Content: string(data)}}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var pages []Page
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		pages = append(pages, Page{Name: name, Content: string(data)})
	}
	slices.SortFunc(pages, func(a, b Page) int { return cmp.Compare(a.Name, b.Name) })
	return pages, nil
}
