package guide

// ZotEntries is the full list of zot guide items.
var ZotEntries = []Entry{
	{
		Name:     "zot setup",
		Cmd:      "zot setup — configure credentials",
		Desc:     "Save your Zotero API key, library ID, and data directory",
		Category: "Getting Started",
		CastFile: "zot-setup.cast",
	},
	{
		Name:     "zot info",
		Cmd:      "zot info — library summary",
		Desc:     "Show item counts, coverage, and type breakdown for your library",
		Category: "Getting Started",
		CastFile: "zot-info.cast",
	},
	{
		Name:     "zot search",
		Cmd:      "zot search — find items",
		Desc:     "Cross-field search by title, DOI, or publication",
		Category: "Browsing",
		CastFile: "zot-search.cast",
	},
	{
		Name:     "zot item list",
		Cmd:      "zot item list — browse items",
		Desc:     "List items with filters for type, collection, tag, and order",
		Category: "Browsing",
		CastFile: "zot-item-list.cast",
	},
	{
		Name:     "zot item read",
		Cmd:      "zot item read — item details",
		Desc:     "Show full metadata, abstract, tags, and attachments for one item",
		Category: "Browsing",
		CastFile: "zot-item-read.cast",
	},
	{
		Name:     "zot item export",
		Cmd:      "zot item export — emit citation",
		Desc:     "Export a single item as BibTeX or CSL-JSON",
		Category: "Export",
		CastFile: "zot-item-export.cast",
	},
	{
		Name:     "zot doctor",
		Cmd:      "zot doctor — library health",
		Desc:     "Run every hygiene check and print a one-line dashboard",
		Category: "Hygiene",
		CastFile: "zot-doctor.cast",
	},
	{
		Name:     "zot doctor duplicates",
		Cmd:      "zot doctor duplicates — find dupes",
		Desc:     "Cluster duplicate items by DOI and title (fuzzy opt-in)",
		Category: "Hygiene",
		CastFile: "zot-doctor-duplicates.cast",
	},
}
