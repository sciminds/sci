package guide

// SciEntries is the full list of sci guide items, one per top-level command group.
var SciEntries = []Entry{
	{
		Name:     "sci doctor",
		Cmd:      "sci doctor — system check",
		Desc:     "Verify your Mac is set up with all required tools",
		Category: "Getting Started",
		CastFile: "sci-doctor.cast",
	},
	{
		Name:     "sci proj",
		Cmd:      "sci proj — Python projects",
		Desc:     "Create, manage packages, and render documents",
		Category: "Projects",
		CastFile: "sci-proj.cast",
	},
	{
		Name:     "sci db",
		Cmd:      "sci db — database management",
		Desc:     "Create SQLite databases and import CSVs",
		Category: "Data",
		CastFile: "sci-db.cast",
	},
	{
		Name:     "sci vid",
		Cmd:      "sci vid — video editing",
		Desc:     "Cut, compress, resize, and convert videos via ffmpeg",
		Category: "Media",
		CastFile: "sci-vid.cast",
	},
	{
		Name:     "sci cloud",
		Cmd:      "sci cloud — cloud storage",
		Desc:     "Upload, download, and share files via SciMinds cloud",
		Category: "Storage",
		CastFile: "sci-cloud.cast",
	},
	{
		Name:     "sci lab",
		Cmd:      "sci lab — lab storage (SFTP)",
		Desc:     "Browse, upload, and download from lab storage",
		Category: "Storage",
		CastFile: "sci-lab.cast",
	},
	{
		Name:     "sci tools",
		Cmd:      "sci tools — Homebrew & uv",
		Desc:     "Install, update, and manage packages via Brewfile",
		Category: "Setup",
		CastFile: "sci-tools.cast",
	},
	{
		Name:     "sci cass",
		Cmd:      "sci cass — Canvas LMS",
		Desc:     "Sync grades, assignments, and submissions with Canvas",
		Category: "Teaching",
		CastFile: "sci-cass.cast",
	},
}
