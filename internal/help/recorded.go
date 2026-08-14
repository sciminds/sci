package help

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
)

// ansiRe matches the escape sequences an asciicast records alongside the
// text, so a command line can be recovered as the user typed it.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// RecordedCommand is one shell command line typed in a cast, with the
// cast it came from so a failure can name the file to re-record.
type RecordedCommand struct {
	Cast string // cast file name, e.g. "zot-search.cast"
	Line string // the command as typed, e.g. `sci zot search "llm"`
}

// RecordedCommands returns every command line typed at a prompt across
// the embedded casts.
//
// These are demos the help TUI plays, so a command line that no longer
// parses teaches a user something false — and unlike help text, a cast
// does not follow the CLI when a verb moves. The casts are hand-authored
// asciicast v2: a header line, then [time, "o", text] events. A prompt
// event is the bare "$ ", and the event straight after it is what was
// typed, so the two together are the only structure this needs.
//
// Trailing backslash continuations are joined, because a wrapped command
// is still one command line.
func RecordedCommands() ([]RecordedCommand, error) {
	entries, err := fs.ReadDir(castFS, "casts")
	if err != nil {
		return nil, err
	}
	var out []RecordedCommand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cast") {
			continue
		}
		body, err := fs.ReadFile(castFS, "casts/"+e.Name())
		if err != nil {
			return nil, err
		}
		for _, line := range recordedLines(string(body)) {
			out = append(out, RecordedCommand{Cast: e.Name(), Line: line})
		}
	}
	return out, nil
}

// recordedLines pulls the typed command lines out of one cast body.
func recordedLines(body string) []string {
	var lines []string
	var pending string // accumulates a backslash-continued command
	atPrompt := false

	for _, raw := range strings.Split(body, "\n") {
		raw = strings.TrimSpace(raw)
		if !strings.HasPrefix(raw, "[") {
			continue // the header, or a blank line
		}
		var ev []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &ev); err != nil || len(ev) < 3 {
			continue
		}
		var kind, text string
		if json.Unmarshal(ev[1], &kind) != nil || kind != "o" {
			continue
		}
		if json.Unmarshal(ev[2], &text) != nil {
			continue
		}
		text = ansiRe.ReplaceAllString(text, "")

		if pending != "" {
			pending += " " + strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), `\`))
			if !strings.HasSuffix(strings.TrimSpace(text), `\`) {
				lines = append(lines, strings.TrimSpace(pending))
				pending = ""
			}
			continue
		}
		if strings.TrimSpace(text) == "$" || text == "$ " {
			atPrompt = true
			continue
		}
		if !atPrompt {
			continue
		}
		atPrompt = false
		cmd := strings.TrimSpace(text)
		if cmd == "" {
			continue
		}
		if strings.HasSuffix(cmd, `\`) {
			pending = strings.TrimSpace(strings.TrimSuffix(cmd, `\`))
			continue
		}
		lines = append(lines, cmd)
	}
	return lines
}
