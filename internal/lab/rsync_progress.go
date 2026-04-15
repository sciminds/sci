package lab

import (
	"regexp"
	"strconv"
	"strings"
)

// Progress is one snapshot of an in-flight rsync transfer, parsed from
// the `--info=progress2` aggregate progress line.
type Progress struct {
	Bytes   int64  // bytes transferred so far across all files
	Percent int    // 0..100
	Rate    string // human string from rsync, e.g. "12.34MB/s"
	ETA     string // human string from rsync, e.g. "0:00:05"
}

// progress2 lines look like:
//
//	"      1,234,567  42%   12.34MB/s    0:00:05"
//
// or, on the final tick:
//
//	"  9,999,999 100%  100.00MB/s    0:00:00 (xfr#3, to-chk=0/3)"
var progressRE = regexp.MustCompile(`^\s*([\d,]+)\s+(\d+)%\s+(\S+)\s+(\d+:\d+:\d+)`)

// ParseProgressLine parses one rsync progress2 line. Returns (_, false)
// for lines that aren't progress (filenames, status messages, blank lines).
func ParseProgressLine(line string) (Progress, bool) {
	m := progressRE.FindStringSubmatch(line)
	if m == nil {
		return Progress{}, false
	}
	bytes, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
	if err != nil {
		return Progress{}, false
	}
	pct, err := strconv.Atoi(m[2])
	if err != nil {
		return Progress{}, false
	}
	return Progress{Bytes: bytes, Percent: pct, Rate: m[3], ETA: m[4]}, true
}

// BuildResumableGetArgs constructs the argv for a resumable rsync download.
// --partial keeps an interrupted transfer's bytes at the destination path;
// --append-verify resumes by appending new bytes and checksumming the existing
// prefix. Note: rsync 3.4+ rejects --append* combined with --partial-dir, so we
// keep partials in place rather than in a sidecar directory.
func BuildResumableGetArgs(cfg *Config, remotePath, localPath string) []string {
	return []string{
		"rsync", "-az",
		"--partial",
		"--append-verify",
		"--info=progress2",
		cfg.SSHAlias() + ":" + remotePath,
		localPath,
	}
}
