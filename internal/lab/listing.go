package lab

import (
	"strconv"
	"strings"

	"github.com/samber/lo"
)

// Entry is one item in a remote directory listing.
type Entry struct {
	Name   string
	IsDir  bool
	IsLink bool
}

// BuildBrowseLsArgs constructs the argv for listing a remote directory with
// type markers (ls -F) and directories first. `-Q` (C-style quoting) wraps
// each name in double quotes and escapes whitespace/specials, so a filename
// containing a newline doesn't split into multiple entries.
func BuildBrowseLsArgs(cfg *Config, remotePath string) []string {
	return []string{"ssh", cfg.SSHAlias(), "ls", "-1FQ", "--group-directories-first", ShellQuote(remotePath)}
}

// ParseLsOutput parses `ls -1FAQ` output. Each line is `"name"` followed by
// an optional type marker (/ dir, @ symlink, * exec, etc). Names containing
// whitespace or shell metacharacters are C-escaped inside the quotes; we
// unescape them back to the literal name. Symlinks-to-directories appear as
// "name"@ and are treated as links (not dirs) so the browser won't descend
// into them in v1.
func ParseLsOutput(out string) []Entry {
	lines := strings.Split(out, "\n")
	return lo.FilterMap(lines, func(line string, _ int) (Entry, bool) {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			return Entry{}, false
		}

		// Peel off optional trailing type marker (only valid when it follows
		// the closing quote — otherwise it's part of the quoted name).
		var marker byte
		if n := len(line); n >= 2 && line[n-1] != '"' {
			switch line[n-1] {
			case '/', '@', '*', '=', '|', '>':
				if line[n-2] == '"' {
					marker = line[n-1]
					line = line[:n-1]
				}
			}
		}

		name, ok := unquoteC(line)
		if !ok {
			return Entry{}, false
		}
		switch marker {
		case '/':
			return Entry{Name: name, IsDir: true}, true
		case '@':
			return Entry{Name: name, IsLink: true}, true
		default:
			// plain file (or executable / socket / pipe / door)
			return Entry{Name: name}, true
		}
	})
}

// unquoteC unwraps a `"…"` C-escaped string as produced by `ls -Q`. Falls
// back to strconv.Unquote (which understands the same escape catalog).
// Returns (_, false) if the input isn't a valid quoted string so callers can
// silently skip malformed lines.
func unquoteC(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	out, err := strconv.Unquote(s)
	if err != nil {
		return "", false
	}
	return out, true
}
