package lab

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestNonInteractiveSSH_AlwaysBatchMode is a regression guard for the bug
// reported in issue #2: an ssh command run inside a spinner without
// BatchMode=yes lets sshd silently fall back to password auth, drawing a
// Password: prompt on /dev/tty *while* the Bubbletea spinner is also reading
// os.Stdin and writing to stderr. The two race for the terminal — output is
// corrupted and keystrokes the user thinks are typing into one context can
// land in the other. The only safe contract is "ssh under a spinner can never
// prompt", and BatchMode=yes is what enforces that. If this test ever fails,
// some caller is one step away from re-introducing the corrupted-password
// prompt.
func TestNonInteractiveSSH_AlwaysBatchMode(t *testing.T) {
	cmd := nonInteractiveSSH("scilab-foo", "echo", "ok")
	if !slices.Contains(cmd.Args, "BatchMode=yes") {
		t.Fatalf("nonInteractiveSSH must include BatchMode=yes; got %v", cmd.Args)
	}
	if !slices.Contains(cmd.Args, "ConnectTimeout=10") {
		t.Errorf("nonInteractiveSSH should include ConnectTimeout=10; got %v", cmd.Args)
	}
	// Remote args must come after the alias, not be eaten as ssh options.
	aliasIdx := slices.Index(cmd.Args, "scilab-foo")
	echoIdx := slices.Index(cmd.Args, "echo")
	if aliasIdx < 0 || echoIdx < 0 || echoIdx < aliasIdx {
		t.Errorf("remote args must follow the alias; got %v", cmd.Args)
	}
}

func TestTestFailureMessage_PublicKeyRejection(t *testing.T) {
	stderr := "Warning: Permanently added 'ssrde.ucsd.edu' (ED25519) to the list of known hosts.\nuser@ssrde.ucsd.edu: Permission denied (publickey).\n"
	got := testFailureMessage(stderr)
	if !strings.Contains(got, "forced-password-reset") {
		t.Errorf("publickey rejection should hint at the SSRDE password-reset state; got %q", got)
	}
}

func TestTestFailureMessage_OtherStderrSurfaced(t *testing.T) {
	stderr := "ssh: connect to host ssrde.ucsd.edu port 22: Connection timed out"
	got := testFailureMessage(stderr)
	if !strings.Contains(got, "Connection timed out") {
		t.Errorf("non-publickey stderr should be surfaced verbatim; got %q", got)
	}
}

func TestTestFailureMessage_EmptyStderrFallback(t *testing.T) {
	got := testFailureMessage("")
	if got == "" {
		t.Error("empty stderr should still produce a message")
	}
}

// writeFile is a tiny test helper: write content to home/relPath, creating
// any parent dirs and the private key target if asked.
func writeFile(t *testing.T, home, relPath, content string) string {
	t.Helper()
	full := filepath.Join(home, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestFindSSHKey_PrefersIdentityFileFromConfig(t *testing.T) {
	home := t.TempDir()
	keyPath := writeFile(t, home, ".ssh/ssrde_ed25519", "PRIVATE KEY")
	writeFile(t, home, ".ssh/config", `Host scilab
    Hostname `+Host+`
    User someone
    IdentityFile ~/.ssh/ssrde_ed25519
`)
	got := findSSHKey(home, Host)
	if got != keyPath {
		t.Errorf("want IdentityFile from matching config block (%s); got %q", keyPath, got)
	}
}

func TestFindSSHKey_MatchesHostPatternLiteral(t *testing.T) {
	home := t.TempDir()
	keyPath := writeFile(t, home, ".ssh/lab_key", "PRIVATE KEY")
	writeFile(t, home, ".ssh/config", `Host `+Host+`
    IdentityFile ~/.ssh/lab_key
`)
	got := findSSHKey(home, Host)
	if got != keyPath {
		t.Errorf("want IdentityFile when Host pattern matches; got %q", got)
	}
}

func TestFindSSHKey_FallsBackWhenIdentityFileMissing(t *testing.T) {
	home := t.TempDir()
	canonical := writeFile(t, home, ".ssh/id_ed25519", "CANONICAL KEY")
	writeFile(t, home, ".ssh/config", `Host scilab
    Hostname `+Host+`
    IdentityFile ~/.ssh/does_not_exist
`)
	got := findSSHKey(home, Host)
	if got != canonical {
		t.Errorf("config IdentityFile points at missing file → should fall back to canonical %s; got %q", canonical, got)
	}
}

func TestFindSSHKey_FallsBackWhenNoMatchingBlock(t *testing.T) {
	home := t.TempDir()
	canonical := writeFile(t, home, ".ssh/id_ed25519", "CANONICAL KEY")
	writeFile(t, home, ".ssh/lab_key", "UNRELATED LAB KEY")
	writeFile(t, home, ".ssh/config", `Host github.com
    IdentityFile ~/.ssh/lab_key
`)
	got := findSSHKey(home, Host)
	if got != canonical {
		t.Errorf("unrelated Host block should not match; got %q", got)
	}
}

func TestFindSSHKey_NoConfigStillUsesCanonical(t *testing.T) {
	home := t.TempDir()
	canonical := writeFile(t, home, ".ssh/id_rsa", "RSA KEY")
	got := findSSHKey(home, Host)
	if got != canonical {
		t.Errorf("no config → canonical fallback; got %q", got)
	}
}

func TestFindSSHKey_HandlesEqualsSeparator(t *testing.T) {
	home := t.TempDir()
	keyPath := writeFile(t, home, ".ssh/ssrde_ed25519", "PRIVATE KEY")
	writeFile(t, home, ".ssh/config", `Host scilab
    Hostname=`+Host+`
    IdentityFile=~/.ssh/ssrde_ed25519
`)
	got := findSSHKey(home, Host)
	if got != keyPath {
		t.Errorf("want '=' separator parsed; got %q", got)
	}
}

func TestFindSSHKey_SkipsCommentsAndBlankLines(t *testing.T) {
	home := t.TempDir()
	keyPath := writeFile(t, home, ".ssh/ssrde_ed25519", "PRIVATE KEY")
	writeFile(t, home, ".ssh/config", `# top comment

Host scilab
    # inner comment
    Hostname `+Host+`

    IdentityFile ~/.ssh/ssrde_ed25519
`)
	got := findSSHKey(home, Host)
	if got != keyPath {
		t.Errorf("comments/blank lines should be ignored; got %q", got)
	}
}

func TestFindSSHKey_NothingFoundReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	got := findSSHKey(home, Host)
	if got != "" {
		t.Errorf("no key anywhere → \"\"; got %q", got)
	}
}

func TestUpgradeControlPersist_RewritesStaleValue(t *testing.T) {
	in := `Host other
    ControlPersist 1h

Host sciminds
    Hostname ssrde.ucsd.edu
    ControlPersist 4h
    User alice
`
	out, changed := upgradeControlPersist(in, "sciminds", "12h")
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(out, "    ControlPersist 12h\n") {
		t.Errorf("expected sciminds block to have ControlPersist 12h; got:\n%s", out)
	}
	if !strings.Contains(out, "Host other\n    ControlPersist 1h\n") {
		t.Errorf("other host's ControlPersist should be untouched; got:\n%s", out)
	}
}

func TestUpgradeControlPersist_NoChangeWhenAlreadyTarget(t *testing.T) {
	in := `Host sciminds
    ControlPersist 12h
`
	out, changed := upgradeControlPersist(in, "sciminds", "12h")
	if changed {
		t.Error("expected changed=false")
	}
	if out != in {
		t.Error("output should equal input when no change needed")
	}
}

func TestUpgradeControlPersist_NoBlockNoChange(t *testing.T) {
	in := `Host other
    ControlPersist 4h
`
	_, changed := upgradeControlPersist(in, "sciminds", "12h")
	if changed {
		t.Error("expected changed=false when alias not present")
	}
}
