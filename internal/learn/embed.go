package learn

import "embed"

//go:embed casts/*.cast
var castFS embed.FS

// LoadCast reads a .cast file from the embedded filesystem.
func LoadCast(name string) ([]byte, error) {
	return castFS.ReadFile("casts/" + name)
}

//go:embed pages/*.md
var pageFS embed.FS

// LoadPage reads a .md file from the embedded filesystem.
func LoadPage(name string) ([]byte, error) {
	return pageFS.ReadFile("pages/" + name)
}
