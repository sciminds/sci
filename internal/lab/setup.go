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
	keyPath := findSSHKey(home)
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

// findSSHKey returns the path to the first existing SSH key, or "" if none found.
func findSSHKey(home string) string {
	sshDir := filepath.Join(home, ".ssh")
	for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		p := filepath.Join(sshDir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
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
