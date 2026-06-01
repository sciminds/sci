package doctor

// reccs_tui.go — multi-select picker for optional tool recommendations.

import (
	"fmt"

	"charm.land/huh/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/uikit"
)

// toolDescs maps optional-tool names to one-line, user-facing descriptions
// shown in the recommendations picker. Names without an entry fall back to a
// bare label. Keyed by BrewfileEntry.Name (no [extras] suffix).
var toolDescs = map[string]string{
	// CLI tools (brew formulae)
	"helix":                      "Modal terminal editor with built-in LSP",
	"neovim":                     "Hyperextensible Vim-based terminal editor",
	"msedit":                     "Microsoft's simple terminal text editor",
	"starship":                   "Fast, customizable cross-shell prompt",
	"lsd":                        "Modern ls with colors, icons, and tree view",
	"bat":                        "cat clone with syntax highlighting and Git integration",
	"just":                       "Command runner for project task recipes",
	"fzf":                        "Fuzzy finder for files, history, and pipes",
	"zoxide":                     "Smarter cd that learns your most-used dirs",
	"fd":                         "Fast, user-friendly alternative to find",
	"sd":                         "Intuitive find-and-replace (sed alternative)",
	"jaq":                        "Fast jq clone for querying JSON",
	"xan":                        "CSV toolkit for slicing and analyzing tables",
	"mq":                         "jq for Markdown — query and transform .md files",
	"git-delta":                  "Syntax-highlighted pager for git diffs",
	"ripgrep-all":                "ripgrep across PDFs, archives, and more",
	"ast-grep":                   "Structural code search and rewrite via AST patterns",
	"semgrep":                    "Pattern-based static analysis and linting",
	"visidata":                   "Terminal spreadsheet for exploring tabular data",
	"pandoc":                     "Universal document converter",
	"typst":                      "Modern markup-based typesetting system",
	"tinymist":                   "Language server for Typst",
	"glow":                       "Render Markdown beautifully in the terminal",
	"dust":                       "Intuitive du — see what's using disk space",
	"yazi":                       "Blazing-fast terminal file manager",
	"harper":                     "Fast, private grammar checker",
	"poppler":                    "PDF rendering tools (pdftotext, pdfimages, …)",
	"markdown-oxide":             "Knowledge-base language server for Markdown",
	"tig":                        "Text-mode interface for Git",
	"yadm":                       "Dotfiles manager built on Git",
	"atuin":                      "Searchable, synced shell history",
	"zsh-syntax-highlighting":    "Fish-like syntax highlighting for zsh",
	"typescript-language-server": "Language server for TypeScript/JavaScript",

	// GUI apps (casks)
	"1password":          "Password manager and secrets vault",
	"brave-browser":      "Privacy-focused Chromium web browser",
	"kitty":              "Fast, GPU-accelerated terminal emulator",
	"obsidian":           "Markdown knowledge base and note-taking",
	"reflect":            "Networked note-taking with backlinks",
	"notion":             "All-in-one notes, docs, and wikis",
	"tailscale-app":      "Zero-config WireGuard VPN mesh",
	"dropbox":            "Cloud file sync and storage",
	"google-drive":       "Google cloud file sync and storage",
	"fantastical":        "Natural-language calendar and scheduling",
	"quarto":             "Scientific and technical publishing system",
	"raycast":            "Extensible launcher and Spotlight replacement",
	"amethyst":           "Tiling window manager for macOS",
	"karabiner-elements": "Powerful keyboard customizer and remapper",
	"visual-studio-code": "Popular graphical code editor by Microsoft",
	"zed":                "High-performance graphical code editor",
	"zoom":               "Video conferencing and meetings",
	"zotero":             "Reference manager for research and citations",
	"slack":              "Team chat and messaging",
	"vlc":                "Plays nearly any audio or video format",

	// Python tools (uv)
	"symbex":       "Find Python symbols (functions, classes) from the CLI",
	"sqlite-utils": "CLI for creating and querying SQLite databases",
	"markitdown":   "Convert documents (PDF, DOCX, …) to Markdown",
	"datasette":    "Instant web UI and JSON API for SQLite",
	"docling-slim": "Parse PDFs and documents into structured data",
	"rodney":       "Chrome automation for scraping and testing",
	"llm":          "Talk to LLMs from the command line",
	"pdf2doi":      "Look up DOIs and metadata for PDF papers",
}

// pickOptionalTools presents the given (missing) entries in a multi-select
// picker and returns the entries the user ticked. apps tailors the wording and
// drops the redundant per-row type tag (every row is a cask).
func pickOptionalTools(entries []brew.BrewfileEntry, apps bool) ([]brew.BrewfileEntry, error) {
	noun := lo.Ternary(apps, "apps", "tools")
	title := fmt.Sprintf("Recommended %s — %d available to install", noun, len(entries))
	desc := "Space to tick, enter to install, / to filter, esc to cancel."

	chosen, err := uikit.MultiSelect(title, desc, optionalToolOptions(entries, apps))
	if err != nil {
		return nil, err
	}

	byName := lo.KeyBy(entries, func(e brew.BrewfileEntry) string { return e.Name })
	return lo.FilterMap(chosen, func(name string, _ int) (brew.BrewfileEntry, bool) {
		e, ok := byName[name]
		return e, ok
	}), nil
}

// optionalToolOptions builds huh options whose label carries an inline
// description (and, in the mixed catalog view, the package type) so users can
// recognize each tool without a separate detail pane. The option value is the
// tool name, which maps back to a BrewfileEntry after selection.
func optionalToolOptions(entries []brew.BrewfileEntry, apps bool) []huh.Option[string] {
	return lo.Map(entries, func(e brew.BrewfileEntry, _ int) huh.Option[string] {
		label := e.Name
		if desc := toolDescs[e.Name]; desc != "" {
			label += " — " + desc
		}
		// In the mixed catalog, tag the type so an app and a CLI tool are
		// distinguishable at a glance. The apps view is all casks, so the tag
		// would just be noise.
		if !apps {
			label += fmt.Sprintf("  (%s)", e.Type)
		}
		return huh.NewOption(label, e.Name)
	})
}
