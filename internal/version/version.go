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

// Version is the CalVer release tag (e.g. "v2026.08.03", or "v2026.08.03.1"
// for a same-day follow-up), stamped only by the release workflow. It is the
// binary's orderable identity: unlike [Commit], two versions can be compared
// to tell ahead from behind. Empty in every non-release build.
var Version = ""

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

// String renders the human-facing version: "v2026.08.03 (1234567)" for a
// release build, "dev (1234567)" otherwise.
func String() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	c := Commit
	if len(c) > 7 {
		c = c[:7]
	}
	return v + " (" + c + ")"
}
