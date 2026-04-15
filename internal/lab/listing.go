package lab

import (
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
// type markers (ls -F) and directories first. Used by the browse TUI, which
// needs structured output; sci lab ls uses BuildLsArgs for human output.
func BuildBrowseLsArgs(cfg *Config, remotePath string) []string {
	return []string{"ssh", cfg.SSHAlias(), "ls", "-1FA", "--group-directories-first", remotePath}
}

// ParseLsOutput parses `ls -1F` output (one name per line, type-marked with
// / for dirs, @ for symlinks, * for executables) into structured entries.
// Symlinks-to-directories appear as name@ and are treated as links (not dirs)
// so the browser won't descend into them in v1.
func ParseLsOutput(out string) []Entry {
	lines := strings.Split(out, "\n")
	return lo.FilterMap(lines, func(line string, _ int) (Entry, bool) {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			return Entry{}, false
		}
		last := line[len(line)-1]
		switch last {
		case '/':
			return Entry{Name: line[:len(line)-1], IsDir: true}, true
		case '@':
			return Entry{Name: line[:len(line)-1], IsLink: true}, true
		case '*', '=', '|', '>':
			// executable, socket, pipe, door — treat as plain file
			return Entry{Name: line[:len(line)-1]}, true
		default:
			return Entry{Name: line}, true
		}
	})
}
