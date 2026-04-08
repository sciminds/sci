package app

// keymap_help.go — key binding definitions for the help overlay (? key).
// These are display-only keymaps used by bubbles/help to render the help panel.

import (
	"charm.land/bubbles/v2/key"
)

// ── Navigation key map (shown in help overlay) ──────────────────────────────

type navKeyMap struct {
	Rows       key.Binding
	Cols       key.Binding
	TopBottom  key.Binding
	FirstLast  key.Binding
	HalfPage   key.Binding
	Tabs       key.Binding
	Sort       key.Binding
	Search     key.Binding
	ToggleCol  key.Binding
	ExpandCol  key.Binding
	Space      key.Binding
	Filter     key.Binding
	InvertFilt key.Binding
	ClearPins  key.Binding
	Preview    key.Binding
	Visual     key.Binding
	Edit       key.Binding
	TableMgr   key.Binding
	Help       key.Binding
	Quit       key.Binding
}

func newNavKeyMap() navKeyMap {
	return navKeyMap{
		Rows:       key.NewBinding(key.WithKeys("j", "k", "up", "down"), key.WithHelp("j/k/↑/↓", "rows")),
		Cols:       key.NewBinding(key.WithKeys("h", "l", "left", "right"), key.WithHelp("h/l/←/→", "columns")),
		TopBottom:  key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "first/last row")),
		FirstLast:  key.NewBinding(key.WithKeys("^", "$"), key.WithHelp("^/$", "first/last col")),
		HalfPage:   key.NewBinding(key.WithKeys("d", "u"), key.WithHelp("d/u", "half page")),
		Tabs:       key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "switch tabs")),
		Sort:       key.NewBinding(key.WithKeys("s", "S"), key.WithHelp("s/S", "sort / clear")),
		Search:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search rows")),
		ToggleCol:  key.NewBinding(key.WithKeys("c", "C"), key.WithHelp("c/C", "toggle col")),
		ExpandCol:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "expand col")),
		Space:      key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "pin/unpin")),
		Filter:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		InvertFilt: key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "invert filter")),
		ClearPins:  key.NewBinding(key.WithKeys("shift+space"), key.WithHelp("shift+space", "clear pins")),
		Preview:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "preview")),
		Visual:     key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "visual mode")),
		Edit:       key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "edit mode")),
		TableMgr:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "table manager")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:       key.NewBinding(key.WithKeys("ctrl+q"), key.WithHelp("ctrl+q", "quit")),
	}
}

func (k navKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k navKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Rows, k.Cols, k.TopBottom, k.FirstLast, k.HalfPage, k.Tabs},
		{k.Sort, k.Search, k.ToggleCol, k.ExpandCol, k.Space, k.Filter, k.InvertFilt, k.ClearPins},
		{k.Preview, k.Visual, k.Edit, k.TableMgr, k.Help, k.Quit},
	}
}

// ── Visual mode key map ─────────────────────────────────────────────────────

type visualKeyMap struct {
	Enter  key.Binding
	Toggle key.Binding
	Extend key.Binding
	Delete key.Binding
	Cut    key.Binding
	Yank   key.Binding
	Copy   key.Binding
	Paste  key.Binding
	Exit   key.Binding
}

func newVisualKeyMap() visualKeyMap {
	return visualKeyMap{
		Enter:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "visual mode")),
		Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle row")),
		Extend: key.NewBinding(key.WithKeys("J", "K"), key.WithHelp("J/K", "extend")),
		Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Cut:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "cut")),
		Yank:   key.NewBinding(key.WithKeys("y", "c"), key.WithHelp("y/c", "yank")),
		Copy:   key.NewBinding(key.WithKeys("Y", "C"), key.WithHelp("Y/C", "copy")),
		Paste:  key.NewBinding(key.WithKeys("p", "P"), key.WithHelp("p/P", "paste")),
		Exit:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "exit")),
	}
}

func (k visualKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Exit}
}

func (k visualKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Enter, k.Toggle, k.Extend, k.Delete, k.Cut, k.Yank, k.Copy, k.Paste, k.Exit},
	}
}

// ── Edit mode key map ───────────────────────────────────────────────────────

type editKeyMap struct {
	Edit key.Binding
	Exit key.Binding
}

func newEditKeyMap() editKeyMap {
	return editKeyMap{
		Edit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit cell")),
		Exit: key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("esc/n", "nav mode")),
	}
}

func (k editKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Exit}
}

func (k editKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Edit, k.Exit},
	}
}
