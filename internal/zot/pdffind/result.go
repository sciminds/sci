package pdffind

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
)

// CLIResult is the cmdutil.Result shell around a Scan (and optional Download).
// Lives in the pdffind package — same pattern as enrich.FromMissingResult —
// so the command-layer wiring stays thin and the renderer can reach into
// Finding fields directly.
type CLIResult struct {
	Collection  string    `json:"collection,omitempty"` // collection name/key the scan targeted
	Scanned     int       `json:"scanned"`
	CacheHits   int       `json:"cache_hits"`
	CacheMisses int       `json:"cache_misses"`
	Findings    []Finding `json:"findings"`
	Downloaded  bool      `json:"downloaded"` // true when --download ran
	Attached    bool      `json:"attached"`   // true when --attach ran

	// Limit is the render cap; 0 means show everything. Not emitted in JSON
	// because it's purely a human-render knob.
	Limit int `json:"-"`
}

// JSON implements cmdutil.Result.
func (r CLIResult) JSON() any {
	return struct {
		Collection  string    `json:"collection,omitempty"`
		Scanned     int       `json:"scanned"`
		CacheHits   int       `json:"cache_hits"`
		CacheMisses int       `json:"cache_misses"`
		Findings    []Finding `json:"findings"`
		Downloaded  bool      `json:"downloaded"`
		Attached    bool      `json:"attached"`
	}{r.Collection, r.Scanned, r.CacheHits, r.CacheMisses, r.Findings, r.Downloaded, r.Attached}
}

// Human implements cmdutil.Result.
func (r CLIResult) Human() string {
	var b strings.Builder

	header := "PDF lookup"
	if r.Collection != "" {
		header += " — collection " + r.Collection
	}
	fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.TextBlueBold().Render(header))
	fmt.Fprintf(&b, "  %s %d item(s) scanned  %s %d cached, %d fetched\n\n",
		uikit.TUI.Dim().Render("·"), r.Scanned,
		uikit.TUI.Dim().Render("·"), r.CacheHits, r.CacheMisses)

	if r.Scanned == 0 {
		fmt.Fprintf(&b, "  %s no items in collection\n", uikit.SymArrow)
		return b.String()
	}

	withPDF := lo.CountBy(r.Findings, func(f Finding) bool { return f.PDFURL != "" })
	withOADOI := lo.CountBy(r.Findings, func(f Finding) bool { return f.LocalDOI == "" && f.OADOI != "" })
	withLanding := lo.CountBy(r.Findings, func(f Finding) bool { return f.LandingPageURL != "" })
	notFound := lo.CountBy(r.Findings, func(f Finding) bool { return f.LookupError != "" })

	fmt.Fprintf(&b, "  %s %s  %s %s  %s %s  %s %s\n\n",
		uikit.TUI.TextGreen().Render("✓"),
		uikit.TUI.TextGreen().Render(fmt.Sprintf("%d pdf url", withPDF)),
		uikit.TUI.Dim().Render("·"),
		uikit.TUI.Dim().Render(fmt.Sprintf("%d landing", withLanding)),
		uikit.TUI.Dim().Render("·"),
		uikit.TUI.Dim().Render(fmt.Sprintf("%d new doi", withOADOI)),
		uikit.SymWarn,
		uikit.TUI.Warn().Render(fmt.Sprintf("%d not found", notFound)),
	)

	show := r.Findings
	truncated := 0
	if r.Limit > 0 && len(show) > r.Limit {
		truncated = len(show) - r.Limit
		show = show[:r.Limit]
	}

	for _, f := range show {
		writeFindingBlock(&b, f, r.Downloaded, r.Attached)
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "    %s %d more (use --limit 0 or --json for all)\n",
			uikit.TUI.Dim().Render("…"), truncated)
	}

	// Footer: action hint.
	switch {
	case r.Attached:
		att := lo.CountBy(r.Findings, func(f Finding) bool { return f.AttachmentKey != "" && f.AttachError == "" })
		fail := lo.CountBy(r.Findings, func(f Finding) bool { return f.AttachError != "" })
		fmt.Fprintf(&b, "\n  %s %d attached to Zotero, %d failed\n", uikit.SymArrow, att, fail)
	case r.Downloaded:
		dl := lo.CountBy(r.Findings, func(f Finding) bool { return f.DownloadedPath != "" })
		fail := lo.CountBy(r.Findings, func(f Finding) bool { return f.DownloadError != "" })
		fmt.Fprintf(&b, "\n  %s %d downloaded, %d failed  (pass --attach to upload as child attachments)\n", uikit.SymArrow, dl, fail)
	case withPDF > 0:
		fmt.Fprintf(&b, "\n  %s rerun with --download <dir> to retrieve %d PDF(s)\n",
			uikit.SymArrow, withPDF)
	default:
		fmt.Fprintf(&b, "\n  %s no fetchable PDF URLs found\n", uikit.SymArrow)
	}
	return b.String()
}

func writeFindingBlock(b *strings.Builder, f Finding, showDownload, showAttach bool) {
	title := f.Title
	if title == "" {
		title = uikit.TUI.Dim().Render("(untitled)")
	}

	// Header line: key + title + lookup method / OpenAlex ID.
	tail := ""
	switch {
	case f.LookupError != "":
		tail = uikit.TUI.Warn().Render("✗ " + f.LookupError)
	case f.OpenAlexID != "":
		method := f.LookupMethod
		if method == "title" {
			method = uikit.TUI.Warn().Render("title-match")
		}
		tail = uikit.TUI.Dim().Render(fmt.Sprintf("← %s (%s)", f.OpenAlexID, method))
	}
	fmt.Fprintf(b, "  %s  %s  %s\n",
		uikit.TUI.TextBlue().Render(f.ItemKey),
		title,
		tail,
	)

	if f.PDFURL != "" {
		fmt.Fprintf(b, "    %s pdf:     %s\n",
			uikit.TUI.TextGreen().Render("+"),
			uikit.TUI.Dim().Render(f.PDFURL),
		)
	}
	if f.LandingPageURL != "" {
		fmt.Fprintf(b, "    %s landing: %s\n",
			uikit.TUI.TextGreen().Render("+"),
			uikit.TUI.Dim().Render(f.LandingPageURL),
		)
	}
	// Only show the DOI when OpenAlex surfaced something new to Zotero.
	if f.LocalDOI == "" && f.OADOI != "" {
		fmt.Fprintf(b, "    %s doi:     %s\n",
			uikit.TUI.TextGreen().Render("+"),
			uikit.TUI.Dim().Render(f.OADOI),
		)
	}
	if f.IsOA && f.OAStatus != "" {
		fmt.Fprintf(b, "    %s oa:      %s\n",
			uikit.TUI.Dim().Render("·"),
			f.OAStatus,
		)
	}
	if showDownload {
		switch {
		case f.DownloadedPath != "":
			fmt.Fprintf(b, "    %s saved:   %s\n",
				uikit.TUI.TextGreen().Render("↓"),
				f.DownloadedPath,
			)
		case f.DownloadError != "":
			fmt.Fprintf(b, "    %s fetch failed: %s\n",
				uikit.SymWarn,
				uikit.TUI.Warn().Render(f.DownloadError),
			)
		}
	}
	if showAttach {
		switch {
		case f.AttachmentKey != "" && f.AttachError == "":
			fmt.Fprintf(b, "    %s attached: %s\n",
				uikit.TUI.TextGreen().Render("↑"),
				uikit.TUI.Dim().Render(f.AttachmentKey),
			)
		case f.AttachmentKey != "" && f.AttachError != "":
			// Created on Zotero but file upload failed — the attachment
			// item exists without bytes. Call it out so the user can
			// retry rather than wonder why it appears empty.
			fmt.Fprintf(b, "    %s attach partial: item %s created, %s\n",
				uikit.SymWarn,
				f.AttachmentKey,
				uikit.TUI.Warn().Render(f.AttachError),
			)
		case f.AttachError != "":
			fmt.Fprintf(b, "    %s attach failed: %s\n",
				uikit.SymWarn,
				uikit.TUI.Warn().Render(f.AttachError),
			)
		}
	}
}
