package helptui

import "embed"

//go:embed casts
var castFS embed.FS

// loadCast reads a cast file from the embedded filesystem.
func loadCast(name string) ([]byte, error) {
	return castFS.ReadFile("casts/" + name)
}

// hasCast reports whether a cast file exists in the embedded filesystem.
func hasCast(name string) bool {
	_, err := castFS.Open("casts/" + name)
	return err == nil
}
