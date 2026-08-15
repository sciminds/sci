package help

// longDescs holds wiki-style overviews for each command group. These are
// displayed as a fixed block above the subcommand list when a user opens a
// "book" in the interactive help. Keep each entry to 1-2 short paragraphs.
var longDescs = map[string]string{
	"cloud": "Easily share any file/folder and get a PUBLIC link that anyone can use to download. You can also download any publicly shared files by other lab members (10 GB limit). " +
		"Files are organized per-user and accessed by name. Use this to share " +
		"datasets, results, or any file with collaborators without leaving the terminal.",

	"db": "A toolkit for SQLite and DuckDB databases and for flat tabular files. " +
		"Import CSV or spreadsheet files into queryable tables, inspect schemas, and manage " +
		"tables; peek at any csv/json/parquet file with head, glimpse, shape or summarize, " +
		"convert between formats, or run a read-only SQL query over any of them.",

	"lab": "Connect to the SciMinds UCSD lab storage (must be on campus or connected to VPN) to browse, upload, and " +
		"download files. Run setup once to configure your connection, then use " +
		"familiar ls/get/put commands — get and put with no argument browse interactively, " +
		"and connect opens an ssh shell.",

	"proj": "Scaffold a full Python/Web project with everything setup and configured for you. " +
		"For Python projects this includes the lab's preferred folder structure, recommended libraries, " +
		"and configuration to create notebooks, figures, and PDF reports. " +
		"Smart enough to add/remove/run packages to existing pixi OR uv projects using the same commands.",

	"py": "Create a temporary Python interpreter or notebook with all libraries setup for scratchpad work. " +
		"You can also convert between various notebook file formats. Great for exploratory work, teaching, and " +
		"one-off scripts.",

	"vid": "Everyday video editing from the command line — powered by FFmpeg under-the-hood " +
		"so you don't have to remember the complicated commands. Trim clips, resize, " +
		"extract audio, generate GIFs, adjust speed, strip subtitles, and more. " +
		"Each operation produces a new file; originals are never modified.",

	"view": "General purpose interactive tabular file-viewer straight from the terminal. " +
		"Supports CSV, JSON, Parquet, SQLite and DuckDB databases, and markdown documents. Sort columns, scroll " +
		"through large datasets, and search — all without opening up google-sheets/excel.",

	"tools": "A single layer over Homebrew and uv to keep your tools in sync with a Brewfile. " +
		"Install, uninstall, list, and update packages — type detection is automatic. " +
		"The reccs subcommand suggests useful optional tools — and apps narrows that to recommended GUI apps; " +
		"like a lab-vetted 'App Store' for your terminal :)",

	"setup": "One front door over every tool that needs configuring — lab storage, your Zotero library, Canvas. Shows what is already set up and what is not, then walks you through whichever you pick. Each tool still has its own setup command (`sci lab setup`, `sci zot setup`); this is the menu over all of them.",

	"doctor": "Run a health check on your Mac to verify that required tools, " +
		"runtimes, and configurations are present and correctly set up.",

	"update": "Update sci to the latest released version. Downloads the newest " +
		"binary from GitHub and replaces the current one in-place. No additional steps required.",

	"version": "Print which build of sci you are running — the released version, the commit it was built from, " +
		"and whether it is a development build. This is the first question of every bug report, so it is worth " +
		"pasting the output into one. `sci --version` is the same answer, and `--json` makes it scriptable.",

	"learn": "Interactive guides to the terminal, git, and Python, with live demos " +
		"you can watch and replay. A good starting point if you're new to the " +
		"command-line or need a refresher — for sci's own commands, see `sci help`.",

	"cass": "(Experimental) Manage Canvas LMS courses and GitHub Classroom assignments from the terminal. " +
		"Pull student rosters, assignments, and submissions into a local SQLite database, " +
		"edit grades locally, then push changes back to Canvas with conflict detection. " +
		"GitHub Classroom is optional — works with Canvas-only courses too.",

	"zot": "Query and cite your Zotero library, read-only. Reads your local " +
		"Zotero database directly — fast search over every metadata field, " +
		"a bibliography built from exactly what a manuscript cites, " +
		"BibLaTeX/CSL-JSON export, and health checks that report and stop. " +
		"Run `sci zot setup` once to point it at your Zotero data directory — " +
		"there is no account to connect and no API key to paste, and nothing here " +
		"ever writes to your library.",
}
