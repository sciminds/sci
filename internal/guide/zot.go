package guide

// ZotEntries is the full list of zot guide items, one per top-level command.
// Parent commands (item, collection, tags, notes, llm, doctor) list their
// children in Desc so users can see the full surface without drilling in;
// the companion cast demos the children in sequence.
var ZotEntries = []Entry{
	{
		Name:     "zot setup",
		Cmd:      "zot setup — configure API access",
		Desc:     "Save Zotero API key and library, validate connection",
		Category: "Getting Started",
		CastFile: "zot-setup.cast",
	},
	{
		Name:     "zot info",
		Cmd:      "zot info — library summary",
		Desc:     "Show item counts, collections, tags, and current config (alias: stats)",
		Category: "Getting Started",
		CastFile: "zot-info.cast",
	},
	{
		Name:     "zot view",
		Cmd:      "zot view — interactive table viewer",
		Desc:     "Browse your library read-only with sortable, filterable columns",
		Category: "Browsing",
		CastFile: "zot-view.cast",
	},
	{
		Name:     "zot search",
		Cmd:      "zot search — find items",
		Desc:     "Cross-field search with --limit and --export to BibTeX",
		Category: "Browsing",
		CastFile: "zot-search.cast",
	},
	{
		Name:     "zot export",
		Cmd:      "zot export — library export",
		Desc:     "Full-library BibTeX/CSL-JSON export with collection, tag, and type filters",
		Category: "Export",
		CastFile: "zot-export.cast",
	},
	{
		Name:     "zot item",
		Cmd:      "zot item — per-item operations",
		Desc:     "read, add, update, delete, list, children, open, export",
		Category: "Items",
		CastFile: "zot-item.cast",
	},
	{
		Name:     "zot collection",
		Cmd:      "zot collection — manage collections",
		Desc:     "list, create, delete, add, remove",
		Category: "Organize",
		CastFile: "zot-collection.cast",
	},
	{
		Name:     "zot tags",
		Cmd:      "zot tags — manage tags",
		Desc:     "list, add, remove, delete",
		Category: "Organize",
		CastFile: "zot-tags.cast",
	},
	{
		Name:     "zot notes",
		Cmd:      "zot notes — docling extraction notes",
		Desc:     "list, read, add, update, delete",
		Category: "Notes",
		CastFile: "zot-notes.cast",
	},
	{
		Name:     "zot llm",
		Cmd:      "zot llm — LLM agent tools (experimental)",
		Desc:     "catalog, read, query — query docling notes via LLM agents",
		Category: "Notes",
		CastFile: "zot-llm.cast",
	},
	{
		Name:     "zot doctor",
		Cmd:      "zot doctor — library health",
		Desc:     "invalid, missing, orphans, duplicates — aggregate dashboard + per-check drill-in",
		Category: "Hygiene",
		CastFile: "zot-doctor.cast",
	},
	{
		Name:     "zot extract",
		Cmd:      "zot extract — PDF → note (experimental)",
		Desc:     "Run docling extraction on a single parent item's PDF, post as child note",
		Category: "Extract",
		CastFile: "zot-extract.cast",
	},
	{
		Name:     "zot extract-lib",
		Cmd:      "zot extract-lib — bulk PDF extraction (experimental)",
		Desc:     "Extract every PDF in the library to child notes (resumable)",
		Category: "Extract",
		CastFile: "zot-extract-lib.cast",
	},
}
