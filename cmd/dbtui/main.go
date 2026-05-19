// Package main is the entry point for the dbtui standalone binary,
// an interactive terminal UI for browsing and editing SQLite databases.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/store/sqlite"
	"github.com/sciminds/cli/internal/tui/dbtui/app"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: dbtui <database>\n")
		os.Exit(1)
	}

	dbPath := os.Args[1]
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	s, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = s.Close() }()

	if err := app.Run(s, dbPath); err != nil {
		if errors.Is(err, app.ErrInterrupted) {
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
