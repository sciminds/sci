package lab

import (
	"fmt"
	"strconv"
	"strings"
)

// BuildSizeArgs constructs the argv for measuring total size of multiple
// remote paths in one ssh call. `du -sbc` prints per-path bytes plus a
// final "total" line we parse via ParseDuTotal.
func BuildSizeArgs(cfg *Config, remotePaths []string) []string {
	args := []string{"ssh", cfg.SSHAlias(), "du", "-sbc"}
	for _, p := range remotePaths {
		args = append(args, ShellQuote(p))
	}
	return args
}

// ParseDuTotal extracts the aggregate byte count from `du -c` output,
// which ends with a "<bytes>\ttotal" line.
func ParseDuTotal(out string) (int64, error) {
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) == 2 && strings.TrimSpace(fields[1]) == "total" {
			return strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
		}
	}
	return 0, fmt.Errorf("du output missing total line")
}
