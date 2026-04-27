package lab

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// nonInteractiveSSH builds an ssh invocation that is guaranteed never to
// prompt the user. BatchMode=yes makes ssh fail fast on key-auth rejection
// instead of silently falling back to password auth.
//
// This contract is non-negotiable for any ssh command run under a spinner:
// ssh's password prompt is written to /dev/tty (not stderr), so redirecting
// stdio doesn't suppress it. A spinner running over a hidden Password: prompt
// races for terminal input and produces both corrupted output and the very
// real risk of the user typing a password into the wrong context. Routing all
// non-interactive probes through this helper makes that whole class of bug
// impossible by construction.
func nonInteractiveSSH(alias string, remoteArgs ...string) *exec.Cmd {
	args := slices.Concat([]string{"-o", "ConnectTimeout=10", "-o", "BatchMode=yes", alias}, remoteArgs)
	return exec.Command("ssh", args...)
}

// Setup runs the interactive lab configuration flow:
//  1. Validate username
//  2. Ensure ~/.ssh/ exists
//  3. Find or generate SSH key
//  4. Configure SSH alias with ControlMaster
//  5. Test if key auth already works (skip ssh-copy-id if so)
//  6. Copy key to server via ssh-copy-id
//  7. Test connection
//  8. Save config
func Setup(user string) (*SetupResult, error) {
	if err := ValidateUser(user); err != nil {
		return nil, err
	}

	cfg := &Config{User: user}
	alias := cfg.SSHAlias()

	// 1. Ensure ~/.ssh/ exists with correct permissions.
	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", sshDir, err)
	}

	// 2. Find or generate SSH key.
	keyPath := findSSHKey(home, Host)
	if keyPath == "" {
		keyPath = filepath.Join(sshDir, "id_ed25519")
		uikit.Header("Generating SSH key")
		fmt.Fprintf(os.Stderr, "  No key found — generating %s\n", keyPath)
		fmt.Fprintf(os.Stderr, "  Press Enter to accept defaults (empty passphrase is OK).\n\n")
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("ssh-keygen failed: %w", err)
		}
	} else {
		uikit.OK("SSH key found: " + keyPath)
	}

	// 3. Configure SSH alias with ControlMaster (needed before testing connection).
	if err := configureSSH(alias, user, keyPath, home); err != nil {
		return nil, err
	}

	// 4. Test if key auth already works — skip ssh-copy-id if so.
	if nonInteractiveSSH(alias, "echo", "ok").Run() == nil {
		uikit.OK("SSH key auth already works — skipping ssh-copy-id")
	} else {
		// 5. Copy key to server.
		uikit.Header("Copying SSH key to " + Host)
		fmt.Fprintf(os.Stderr, "  You'll be prompted for your password (and possibly Duo 2FA).\n\n")
		copyCmd := exec.Command("ssh-copy-id", "-i", keyPath+".pub", user+"@"+Host)
		copyCmd.Stdin = os.Stdin
		copyCmd.Stdout = os.Stdout
		copyCmd.Stderr = os.Stderr
		if err := copyCmd.Run(); err != nil {
			return nil, fmt.Errorf("ssh-copy-id failed: %w", err)
		}
	}

	// 6. Test connection. Must use nonInteractiveSSH (BatchMode=yes) — running
	//    a prompting ssh inside the spinner corrupts output and races for
	//    terminal input with the spinner's stdin reader. See helper comment.
	var testErr error
	var testStderr bytes.Buffer
	if err := uikit.RunWithSpinnerStatus("Testing SSH connection", func(setStatus func(string)) error {
		cmd := nonInteractiveSSH(alias, "echo", "ok")
		cmd.Stderr = &testStderr
		testErr = cmd.Run()
		if testErr != nil {
			setStatus("failed")
		} else {
			setStatus("connected")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if testErr != nil {
		return &SetupResult{OK: false, User: user, Message: testFailureMessage(testStderr.String())}, nil
	}

	// 7. Ensure remote write directory exists. Also non-interactive — at this
	//    point key auth has just succeeded, but BatchMode=yes guarantees we
	//    can't get an unexpected prompt if state changes between calls.
	_ = nonInteractiveSSH(alias, "mkdir", "-p", cfg.WriteDir()).Run()

	// 8. Save config.
	if err := SaveConfig(cfg); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return &SetupResult{OK: true, User: user, Message: "lab configured for " + user + "@" + Host}, nil
}

// testFailureMessage turns the captured stderr of a failed key-auth probe
// into a message a user can act on. The two cases that actually happen in the
// field:
//   - "Permission denied (publickey)" — the key didn't authenticate. Most often
//     this is the SSRDE forced-password-reset state from issue #2 (key auth
//     gets through sshd but PAM's account stage rejects). We can't tell that
//     apart from a genuinely bad key from the client side, so we point at both.
//   - anything else — surface the raw stderr so the user has something to
//     paste into a support ticket instead of "test connection failed".
func testFailureMessage(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if strings.Contains(stderr, "Permission denied (publickey)") {
		return "SSH key was copied but the server rejected it on the test connection. " +
			"If you're sure the key is correct, your SSRDE account may be in a " +
			"forced-password-reset state — contact SSRDE support to verify."
	}
	if stderr != "" {
		return "SSH key copied but test connection failed: " + stderr
	}
	return "SSH key copied but test connection failed — check your SSH config"
}

// findSSHKey returns the path to an existing SSH private key for host, or ""
// if none found. It first scans ~/.ssh/config for a Host block whose patterns
// or Hostname match host and uses its IdentityFile if the file exists. This
// catches the common case (issue #2) where a user already set up the lab key
// under a non-canonical name like ~/.ssh/ssrde_ed25519 — without this, sci
// would silently miss it and offer to generate a new key.
//
// Falls back to the OpenSSH canonical names when nothing in the config
// resolves to an existing file.
func findSSHKey(home, host string) string {
	if p := scanSSHConfigForKey(home, host); p != "" {
		return p
	}
	sshDir := filepath.Join(home, ".ssh")
	for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		p := filepath.Join(sshDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// scanSSHConfigForKey parses ~/.ssh/config and returns the first IdentityFile
// (resolved + existing) from a Host block matching host. Match rule: the
// block's Hostname directive equals host, OR one of its Host patterns equals
// host literally. Wildcard patterns (`*`, `*.ucsd.edu`) are intentionally
// ignored — a `Host *` IdentityFile is a global default the user may not
// actually associate with the lab, and treating it as canonical here would
// misidentify keys.
func scanSSHConfigForKey(home, host string) string {
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return ""
	}
	for _, b := range parseSSHConfig(string(data)) {
		if !b.matchesHost(host) {
			continue
		}
		for _, id := range b.identityFiles {
			expanded := expandTilde(id, home)
			if _, err := os.Stat(expanded); err == nil {
				return expanded
			}
		}
	}
	return ""
}

// sshHostBlock is the minimal slice of an ~/.ssh/config Host block we need:
// the patterns from the `Host` line, an optional Hostname override, and any
// IdentityFile entries in declaration order.
type sshHostBlock struct {
	patterns      []string
	hostname      string
	identityFiles []string
}

func (b sshHostBlock) matchesHost(host string) bool {
	if strings.EqualFold(b.hostname, host) {
		return true
	}
	return slices.ContainsFunc(b.patterns, func(p string) bool {
		return strings.EqualFold(p, host)
	})
}

// parseSSHConfig is a small line-based parser. It does NOT implement the full
// OpenSSH grammar — only what's needed to spot a literal-host IdentityFile.
// Specifically: directives outside any Host block, Match blocks, Include
// directives, and pattern globbing are all ignored.
func parseSSHConfig(data string) []sshHostBlock {
	var blocks []sshHostBlock
	var cur *sshHostBlock
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val := splitConfigLine(line)
		switch strings.ToLower(key) {
		case "host":
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			cur = &sshHostBlock{patterns: strings.Fields(val)}
		case "hostname":
			if cur != nil {
				cur.hostname = val
			}
		case "identityfile":
			if cur != nil && val != "" {
				cur.identityFiles = append(cur.identityFiles, val)
			}
		}
	}
	if cur != nil {
		blocks = append(blocks, *cur)
	}
	return blocks
}

// splitConfigLine splits a directive line on the first run of whitespace or
// `=` separators. OpenSSH accepts both `Key Value` and `Key=Value`.
func splitConfigLine(s string) (key, val string) {
	i := strings.IndexAny(s, " \t=")
	if i < 0 {
		return s, ""
	}
	key = s[:i]
	val = strings.TrimLeft(s[i:], " \t=")
	val = strings.TrimSpace(val)
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	return key, val
}

// expandTilde resolves a leading `~` or `~/` against home. SSH config also
// accepts `~user`, but that's rare in user configs and not worth the
// /etc/passwd lookup here — we'd just fail the os.Stat and fall back.
func expandTilde(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// configureSSH adds an SSH config Host block if the alias doesn't already exist.
func configureSSH(alias, user, keyPath, home string) error {
	sshDir := filepath.Join(home, ".ssh")
	configPath := filepath.Join(sshDir, "config")
	socketsDir := filepath.Join(sshDir, "sockets")

	// Ensure sockets directory exists.
	if err := os.MkdirAll(socketsDir, 0o700); err != nil {
		return fmt.Errorf("create sockets dir: %w", err)
	}

	// Check if alias already exists in SSH config.
	data, _ := os.ReadFile(configPath)
	re := regexp.MustCompile(`(?m)^Host\s+` + regexp.QuoteMeta(alias) + `\s*$`)
	if re.Match(data) {
		updated, changed := upgradeControlPersist(string(data), alias, "12h")
		if changed {
			if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
				return fmt.Errorf("rewrite SSH config: %w", err)
			}
			uikit.OK("SSH config alias " + alias + " already exists (bumped ControlPersist → 12h)")
		} else {
			uikit.OK("SSH config alias " + alias + " already exists")
		}
		return nil
	}

	block := fmt.Sprintf(`
Host %s
    Hostname %s
    User %s
    IdentityFile %s
    ControlMaster auto
    ControlPath %s/%%r@%%h-%%p
    ControlPersist 12h
    ConnectTimeout 10
    ServerAliveInterval 60
    ServerAliveCountMax 3
`, alias, Host, user, keyPath, socketsDir)

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open SSH config: %w", err)
	}
	defer func() { _ = f.Close() }()

	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		block = "\n" + block
	}
	if _, err := f.WriteString(block); err != nil {
		return fmt.Errorf("write SSH config: %w", err)
	}

	uikit.OK("Added SSH config alias: " + alias)
	return nil
}

// upgradeControlPersist rewrites the ControlPersist line inside the given
// alias's Host block to want. The block runs from `Host <alias>` up to the
// next `Host ` line (or EOF). Returns the new text and whether anything
// changed. Lines outside the block are untouched.
func upgradeControlPersist(cfg, alias, want string) (string, bool) {
	lines := strings.Split(cfg, "\n")
	hostRE := regexp.MustCompile(`(?m)^Host\s+` + regexp.QuoteMeta(alias) + `\s*$`)
	nextHostRE := regexp.MustCompile(`(?m)^Host\s+\S`)
	cpRE := regexp.MustCompile(`^(\s*)ControlPersist\s+(\S+)\s*$`)

	inBlock := false
	changed := false
	for i, line := range lines {
		if hostRE.MatchString(line) {
			inBlock = true
			continue
		}
		if inBlock && nextHostRE.MatchString(line) {
			inBlock = false
		}
		if !inBlock {
			continue
		}
		if m := cpRE.FindStringSubmatch(line); m != nil && m[2] != want {
			lines[i] = m[1] + "ControlPersist " + want
			changed = true
		}
	}
	return strings.Join(lines, "\n"), changed
}
