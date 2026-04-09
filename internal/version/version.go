// Package version holds build-time metadata injected via ldflags.
//
// The justfile sets these during "just build":
//
//	go build -ldflags="-X .../version.Commit=abc1234"
//
// In development builds, Commit is "unknown".
package version

// Commit is set at build time via -ldflags.
var Commit = "unknown"
