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

// Dev marks a binary that was NOT produced by the release workflow. It
// defaults to "true" and only the release build stamps it "false", so every
// other way of producing a binary — `just build`, `go build`, a CI scenario
// run — is a dev build by default.
//
// It exists because [Commit] cannot tell the two apart. `just build` stamps a
// real working-tree SHA, which looks exactly like a release SHA to the update
// checker; the checker compares the two for inequality (it has no way to ask
// which is the ancestor) and concludes an update is available. On a tree that
// is *ahead* of the last release — the normal state while dogfooding — that
// advises a downgrade. Opting dev builds out is the honest fix; a real
// ancestry test would need git metadata the binary doesn't carry.
var Dev = "true"

// IsDev reports whether this binary is a development build and should
// therefore stay quiet about published releases.
func IsDev() bool { return Dev == "true" || Commit == "unknown" }
