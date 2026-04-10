package guide

// Book is a top-level guide category shown in the book picker.
type Book struct {
	Name    string  // short identifier
	Heading string  // display title
	Desc    string  // one-line description
	Entries []Entry // items inside this book
}

// list.DefaultItem interface for bubbles/list.
func (b Book) Title() string       { return b.Heading }
func (b Book) Description() string { return b.Desc }
func (b Book) FilterValue() string { return b.Name + " " + b.Heading + " " + b.Desc }

// Books is the registry of all guide books.
var Books = []Book{
	{Name: "basic", Heading: "Terminal Guide", Desc: "Learn basic terminal commands (ls, cd, cp, mv, …)", Entries: BasicEntries},
	{Name: "git", Heading: "Git Guide", Desc: "Learn essential Git commands (init, add, commit, push, …)", Entries: GitEntries},
	{Name: "python", Heading: "Python Guide", Desc: "Python essentials: basics, Polars DataFrames, Seaborn viz", Entries: PythonEntries},
}

// Entry holds a single guide item — a terminal command demo, a markdown page,
// or both. At least one of CastFile or PageFile should be set.
//   - PageFile only → full-width markdown overlay
//   - CastFile only → cast player overlay
//   - Both set      → side-by-side: scrollable markdown (left) + cast player (right)
type Entry struct {
	Name     string // command name, e.g. "ls"
	Cmd      string // display title for the list
	Desc     string // one-line description
	Category string // grouping: "Navigation", "Files", "Search"
	CastFile string // filename in casts/, e.g. "ls.cast"
	PageFile string // filename in pages/, e.g. "python-basics.md"
}

// list.Item interface for bubbles/list.
func (e Entry) Title() string       { return e.Cmd }
func (e Entry) Description() string { return e.Desc }
func (e Entry) FilterValue() string { return e.Name + " " + e.Cmd + " " + e.Desc }

// BasicEntries is the full list of basic terminal guide items.
var BasicEntries = []Entry{
	{
		Name:     "ls",
		Cmd:      "ls — list files",
		Desc:     "Show the contents of a directory",
		Category: "Navigation",
		CastFile: "ls.cast",
		PageFile: "ls.md",
	},
	{
		Name:     "cd",
		Cmd:      "cd — change directory",
		Desc:     "Move into a different folder",
		Category: "Navigation",
		CastFile: "cd.cast",
		PageFile: "cd.md",
	},
	{
		Name:     "pwd",
		Cmd:      "pwd — print working directory",
		Desc:     "Show where you are in the filesystem",
		Category: "Navigation",
		CastFile: "pwd.cast",
		PageFile: "pwd.md",
	},
	{
		Name:     "cat",
		Cmd:      "cat — concatenate and print",
		Desc:     "Display the contents of a file",
		Category: "Files",
		CastFile: "cat.cast",
		PageFile: "cat.md",
	},
	{
		Name:     "mkdir",
		Cmd:      "mkdir — make directory",
		Desc:     "Create a new folder",
		Category: "Files",
		CastFile: "mkdir.cast",
		PageFile: "mkdir.md",
	},
	{
		Name:     "cp",
		Cmd:      "cp — copy",
		Desc:     "Copy files or directories",
		Category: "Files",
		CastFile: "cp.cast",
		PageFile: "cp.md",
	},
	{
		Name:     "mv",
		Cmd:      "mv — move / rename",
		Desc:     "Move or rename files and directories",
		Category: "Files",
		CastFile: "mv.cast",
		PageFile: "mv.md",
	},
	{
		Name:     "rm",
		Cmd:      "rm — remove",
		Desc:     "Delete files (use with caution!)",
		Category: "Files",
		CastFile: "rm.cast",
		PageFile: "rm.md",
	},
	{
		Name:     "grep",
		Cmd:      "grep — search text",
		Desc:     "Find lines matching a pattern in files",
		Category: "Search",
		CastFile: "grep.cast",
		PageFile: "grep.md",
	},
	{
		Name:     "head",
		Cmd:      "head — show beginning",
		Desc:     "Display the first lines of a file",
		Category: "Search",
		CastFile: "head.cast",
		PageFile: "head.md",
	},
	{
		Name:     "echo",
		Cmd:      "echo — print text",
		Desc:     "Print text to the screen or redirect it to a file",
		Category: "Files",
		CastFile: "echo.cast",
		PageFile: "echo.md",
	},
	{
		Name:     "touch",
		Cmd:      "touch — create file",
		Desc:     "Create an empty file or update its timestamp",
		Category: "Files",
		CastFile: "touch.cast",
		PageFile: "touch.md",
	},
	{
		Name:     "bat",
		Cmd:      "bat — better cat",
		Desc:     "View files with syntax highlighting and line numbers",
		Category: "Search",
		CastFile: "bat.cast",
		PageFile: "bat.md",
	},
	{
		Name:     "which",
		Cmd:      "which — locate command",
		Desc:     "Show the full path of a command",
		Category: "Help",
		CastFile: "which.cast",
		PageFile: "which.md",
	},
	{
		Name:     "man",
		Cmd:      "man — manual pages",
		Desc:     "Read the built-in manual for any command",
		Category: "Help",
		CastFile: "man.cast",
		PageFile: "man.md",
	},
}
