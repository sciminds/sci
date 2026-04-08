package guide

// Entry holds a single guide item — a terminal command with its demo recording.
type Entry struct {
	Name     string // command name, e.g. "ls"
	Cmd      string // display title for the list
	Desc     string // one-line description
	Category string // grouping: "Navigation", "Files", "Search"
	CastFile string // filename in casts/, e.g. "ls.cast"
}

// list.Item interface for bubbles/list.
func (e Entry) Title() string       { return e.Cmd }
func (e Entry) Description() string { return e.Desc }
func (e Entry) FilterValue() string { return e.Name + " " + e.Cmd + " " + e.Desc }

// Entries is the full list of guide items.
var Entries = []Entry{
	{
		Name:     "ls",
		Cmd:      "ls — list files",
		Desc:     "Show the contents of a directory",
		Category: "Navigation",
		CastFile: "ls.cast",
	},
	{
		Name:     "cd",
		Cmd:      "cd — change directory",
		Desc:     "Move into a different folder",
		Category: "Navigation",
		CastFile: "cd.cast",
	},
	{
		Name:     "pwd",
		Cmd:      "pwd — print working directory",
		Desc:     "Show where you are in the filesystem",
		Category: "Navigation",
		CastFile: "pwd.cast",
	},
	{
		Name:     "cat",
		Cmd:      "cat — concatenate and print",
		Desc:     "Display the contents of a file",
		Category: "Files",
		CastFile: "cat.cast",
	},
	{
		Name:     "mkdir",
		Cmd:      "mkdir — make directory",
		Desc:     "Create a new folder",
		Category: "Files",
		CastFile: "mkdir.cast",
	},
	{
		Name:     "cp",
		Cmd:      "cp — copy",
		Desc:     "Copy files or directories",
		Category: "Files",
		CastFile: "cp.cast",
	},
	{
		Name:     "mv",
		Cmd:      "mv — move / rename",
		Desc:     "Move or rename files and directories",
		Category: "Files",
		CastFile: "mv.cast",
	},
	{
		Name:     "rm",
		Cmd:      "rm — remove",
		Desc:     "Delete files (use with caution!)",
		Category: "Files",
		CastFile: "rm.cast",
	},
	{
		Name:     "grep",
		Cmd:      "grep — search text",
		Desc:     "Find lines matching a pattern in files",
		Category: "Search",
		CastFile: "grep.cast",
	},
	{
		Name:     "head",
		Cmd:      "head — show beginning",
		Desc:     "Display the first lines of a file",
		Category: "Search",
		CastFile: "head.cast",
	},
	{
		Name:     "echo",
		Cmd:      "echo — print text",
		Desc:     "Print text to the screen or redirect it to a file",
		Category: "Files",
		CastFile: "echo.cast",
	},
	{
		Name:     "touch",
		Cmd:      "touch — create file",
		Desc:     "Create an empty file or update its timestamp",
		Category: "Files",
		CastFile: "touch.cast",
	},
	{
		Name:     "bat",
		Cmd:      "bat — better cat",
		Desc:     "View files with syntax highlighting and line numbers",
		Category: "Search",
		CastFile: "bat.cast",
	},
	{
		Name:     "which",
		Cmd:      "which — locate command",
		Desc:     "Show the full path of a command",
		Category: "Help",
		CastFile: "which.cast",
	},
	{
		Name:     "man",
		Cmd:      "man — manual pages",
		Desc:     "Read the built-in manual for any command",
		Category: "Help",
		CastFile: "man.cast",
	},
}
