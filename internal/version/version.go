// Package version holds build-time metadata injected via ldflags.
//
// The justfile sets these during "just build":
//
//	go build -ldflags="-X .../version.Version=0.1.0 -X .../version.Commit=abc1234"
//
// In development builds, Version is "dev" and Commit is "unknown".
package version

// Version and Commit are set at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
)
