package lab

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

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
		ui.Header("Generating SSH key")
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
		ui.OK("SSH key found: " + keyPath)
	}

	// 3. Configure SSH alias with ControlMaster (needed before testing connection).
	if err := configureSSH(alias, user, keyPath, home); err != nil {
		return nil, err
	}

	// 4. Test if key auth already works — skip ssh-copy-id if so.
	if exec.Command("ssh", "-o", "ConnectTimeout=10", "-o", "BatchMode=yes", alias, "echo", "ok").Run() == nil {
		ui.OK("SSH key auth already works — skipping ssh-copy-id")
	} else {
		// 5. Copy key to server.
		ui.Header("Copying SSH key to " + Host)
		fmt.Fprintf(os.Stderr, "  You'll be prompted for your password (and possibly Duo 2FA).\n\n")
		copyCmd := exec.Command("ssh-copy-id", "-i", keyPath+".pub", user+"@"+Host)
		copyCmd.Stdin = os.Stdin
		copyCmd.Stdout = os.Stdout
		copyCmd.Stderr = os.Stderr
		if err := copyCmd.Run(); err != nil {
			return nil, fmt.Errorf("ssh-copy-id failed: %w", err)
		}
	}

	// 6. Test connection.
	var testErr error
	if err := ui.RunWithSpinner("Testing SSH connection", func(sc ui.SpinnerControls) error {
		testErr = exec.Command("ssh", "-o", "ConnectTimeout=10", alias, "echo", "ok").Run()
		if testErr != nil {
			sc.SetStatus("failed")
		} else {
			sc.SetStatus("connected")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if testErr != nil {
		return &SetupResult{OK: false, User: user, Message: "SSH key copied but test connection failed — check your SSH config"}, nil
	}

	// 7. Ensure remote write directory exists.
	_ = exec.Command("ssh", alias, "mkdir", "-p", cfg.WriteDir()).Run()

	// 8. Save config.
	if err := SaveConfig(cfg); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return &SetupResult{OK: true, User: user, Message: "lab configured for " + user + "@" + Host}, nil
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
		ui.OK("SSH config alias " + alias + " already exists")
		return nil
	}

	block := fmt.Sprintf(`
Host %s
    Hostname %s
    User %s
    IdentityFile %s
    ControlMaster auto
    ControlPath %s/%%r@%%h-%%p
    ControlPersist 4h
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

	ui.OK("Added SSH config alias: " + alias)
	return nil
}
