package guide

import "embed"

//go:embed casts/*.cast
var castFS embed.FS

// LoadCast reads a .cast file from the embedded filesystem.
func LoadCast(name string) ([]byte, error) {
	return castFS.ReadFile("casts/" + name)
}
