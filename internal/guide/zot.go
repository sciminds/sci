package guide

// ZotEntries is the full list of zot guide items, one per top-level command.
var ZotEntries = []Entry{
	{
		Name:     "zot setup",
		Cmd:      "zot setup + info — configure and inspect",
		Desc:     "Save credentials, view library stats, and manage config",
		Category: "Getting Started",
		CastFile: "zot-setup.cast",
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
		Desc:     "List, read, add, update, delete, and inspect children",
		Category: "Items",
		CastFile: "zot-item.cast",
	},
	{
		Name:     "zot collection",
		Cmd:      "zot collection — manage collections",
		Desc:     "List, create, add/remove items, and delete collections",
		Category: "Organize",
		CastFile: "zot-collection.cast",
	},
	{
		Name:     "zot tags",
		Cmd:      "zot tags — manage tags",
		Desc:     "List, add/remove per item, and delete library-wide",
		Category: "Organize",
		CastFile: "zot-tags.cast",
	},
	{
		Name:     "zot doctor",
		Cmd:      "zot doctor — library health + fix",
		Desc:     "Dashboard, drill into checks, fuzzy duplicates, and cite-key repair",
		Category: "Hygiene",
		CastFile: "zot-doctor.cast",
	},
}
