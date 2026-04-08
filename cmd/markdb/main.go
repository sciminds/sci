// Package main is the entry point for the markdb standalone binary,
// which ingests directories of Markdown files into a SQLite database
// with dynamic frontmatter columns, link tracking, and full-text search.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/markdb"
)

func main() {
	var jsonOutput bool
	root := markdb.BuildCommand(&jsonOutput)
	if err := root.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		os.Exit(1)
	}
}
