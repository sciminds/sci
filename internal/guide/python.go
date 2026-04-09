package guide

// PythonEntries is the list of Python guide pages.
var PythonEntries = []Entry{
	{
		Name:     "basics",
		Cmd:      "Python Basics",
		Desc:     "Variables, loops, functions, strings, file I/O",
		Category: "Fundamentals",
		PageFile: "python-basics.md",
	},
	{
		Name:     "polars",
		Cmd:      "Polars (DataFrames)",
		Desc:     "Select, filter, group, reshape with Polars",
		Category: "Data",
		PageFile: "python-polars.md",
	},
	{
		Name:     "seaborn",
		Cmd:      "Seaborn (Data Viz)",
		Desc:     "relplot, displot, catplot, themes, saving figures",
		Category: "Data",
		PageFile: "python-seaborn.md",
	},
}
