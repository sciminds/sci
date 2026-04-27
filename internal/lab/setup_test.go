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

// publickeyStderr is what ssh prints to stderr when key auth is rejected
// under BatchMode=yes — the canonical signal from issue #2.
const publickeyStderr = "Warning: Permanently added 'ssrde.ucsd.edu' (ED25519) to the list of known hosts.\nuser@ssrde.ucsd.edu: Permission denied (publickey).\n"

func TestTestFailureMessage_PublicKey_AgentLoaded(t *testing.T) {
	got := testFailureMessage(publickeyStderr, "/home/u/.ssh/id_ed25519", agentHasKeys)
	if !strings.Contains(got, "forced-password-reset") {
		t.Errorf("with a loaded agent, publickey rejection should point at SSRDE; got %q", got)
	}
	if strings.Contains(got, "ssh-add") {
		t.Errorf("with a loaded agent, don't tell the user to ssh-add; got %q", got)
	}
}

func TestTestFailureMessage_PublicKey_AgentEmpty(t *testing.T) {
	keyPath := "/home/u/.ssh/id_ed25519"
	got := testFailureMessage(publickeyStderr, keyPath, agentNoKeys)
	if !strings.Contains(got, "ssh-add "+keyPath) {
		t.Errorf("empty agent → message must include `ssh-add <keyPath>`; got %q", got)
	}
	if !strings.Contains(got, "forced-password-reset") {
		t.Errorf("still mention the server-side fallback in case the key has no passphrase; got %q", got)
	}
}

func TestTestFailureMessage_PublicKey_AgentUnavailable(t *testing.T) {
	keyPath := "/home/u/.ssh/id_ed25519"
	got := testFailureMessage(publickeyStderr, keyPath, agentUnavailable)
	if !strings.Contains(got, "ssh-agent") {
		t.Errorf("unavailable agent → message must mention ssh-agent; got %q", got)
	}
	if !strings.Contains(got, keyPath) {
		t.Errorf("unavailable agent → message should reference the key path; got %q", got)
	}
}

func TestTestFailureMessage_OtherStderrSurfaced(t *testing.T) {
	stderr := "ssh: connect to host ssrde.ucsd.edu port 22: Connection timed out"
	got := testFailureMessage(stderr, "/key", agentHasKeys)
	if !strings.Contains(got, "Connection timed out") {
		t.Errorf("non-publickey stderr should be surfaced verbatim; got %q", got)
	}
}

func TestTestFailureMessage_EmptyStderrFallback(t *testing.T) {
	got := testFailureMessage("", "/key", agentHasKeys)
	if got == "" {
		t.Error("empty stderr should still produce a message")
	}
}

// ── upgradeIdentityFile (issue #2 follow-up A) ──

func TestUpgradeIdentityFile_RewritesMissingTarget(t *testing.T) {
	home := t.TempDir()
	want := writeFile(t, home, ".ssh/id_ed25519", "PRIVATE KEY")
	cfg := `Host scilab-foo
    Hostname ` + Host + `
    IdentityFile ~/.ssh/gone
    ControlPersist 12h
`
	out, changed := upgradeIdentityFile(cfg, "scilab-foo", want, home)
	if !changed {
		t.Fatal("missing IdentityFile target → expected changed=true")
	}
	if !strings.Contains(out, "    IdentityFile "+want+"\n") {
		t.Errorf("IdentityFile not rewritten to %s; got:\n%s", want, out)
	}
}

func TestUpgradeIdentityFile_PreservesExistingTarget(t *testing.T) {
	home := t.TempDir()
	existing := writeFile(t, home, ".ssh/already_works", "PRIVATE KEY")
	want := writeFile(t, home, ".ssh/id_ed25519", "PRIVATE KEY")
	cfg := `Host scilab-foo
    Hostname ` + Host + `
    IdentityFile ` + existing + `
`
	out, changed := upgradeIdentityFile(cfg, "scilab-foo", want, home)
	if changed {
		t.Error("existing IdentityFile resolves → must not overwrite (could be a hand-edit)")
	}
	if !strings.Contains(out, "    IdentityFile "+existing+"\n") {
		t.Errorf("existing IdentityFile altered; got:\n%s", out)
	}
}

func TestUpgradeIdentityFile_OnlyTouchesAliasBlock(t *testing.T) {
	home := t.TempDir()
	want := writeFile(t, home, ".ssh/id_ed25519", "PRIVATE KEY")
	cfg := `Host other
    IdentityFile ~/.ssh/also_gone

Host scilab-foo
    IdentityFile ~/.ssh/gone
`
	out, changed := upgradeIdentityFile(cfg, "scilab-foo", want, home)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(out, "Host other\n    IdentityFile ~/.ssh/also_gone\n") {
		t.Errorf("other host's IdentityFile must be untouched; got:\n%s", out)
	}
}

func TestUpgradeIdentityFile_AliasMissingNoChange(t *testing.T) {
	home := t.TempDir()
	want := writeFile(t, home, ".ssh/id_ed25519", "PRIVATE KEY")
	cfg := `Host other
    IdentityFile ~/.ssh/gone
`
	out, changed := upgradeIdentityFile(cfg, "scilab-foo", want, home)
	if changed {
		t.Error("alias not present → no-op")
	}
	if out != cfg {
		t.Error("output must equal input when alias not present")
	}
}

func TestUpgradeIdentityFile_NoIdentityFileNoChange(t *testing.T) {
	home := t.TempDir()
	want := writeFile(t, home, ".ssh/id_ed25519", "PRIVATE KEY")
	cfg := `Host scilab-foo
    Hostname ` + Host + `
    ControlPersist 12h
`
	out, changed := upgradeIdentityFile(cfg, "scilab-foo", want, home)
	if changed {
		t.Error("no IdentityFile in block → leave alone (insertion is out of scope)")
	}
	if out != cfg {
		t.Error("output must equal input when nothing to rewrite")
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
