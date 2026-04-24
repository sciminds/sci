package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

func llmCommand() *cli.Command {
	return &cli.Command{
		Name:  "llm",
		Usage: experimental + " LLM-agent tools for querying docling notes",
		Description: "$ sci zot llm catalog                        # compact paper index\n" +
			"$ sci zot llm read ABC123                    # full note content\n" +
			"$ sci zot llm query -s transformers -- .h2   # filter + mq pipeline",
		Commands: []*cli.Command{
			llmCatalogCommand(),
			llmReadCommand(),
			llmQueryCommand(),
		},
	}
}

// isHTMLNote returns true when the note body (after stripping Zotero's
// wrapper div) contains rendered HTML rather than raw markdown.
// Raw-mode docling notes start with YAML frontmatter ("---");
// HTML-mode notes start with an HTML tag ("<h1>", "<p>", etc.).
func isHTMLNote(body string) bool {
	inner := strings.TrimSpace(local.UnwrapZoteroDiv(body))
	return inner != "" && strings.HasPrefix(inner, "<")
}

// noteBodyForMQ extracts the mq-processable content from a Zotero note.
// Strips the wrapper div so mq receives clean markdown (or HTML for
// HTML-mode notes, which should be passed to mq with -I html).
func noteBodyForMQ(body string) string {
	return local.UnwrapZoteroDiv(body)
}

// resolveMQ looks up the mq binary on PATH. Returns an error with
// install guidance when not found.
func resolveMQ() (string, error) {
	bin, err := exec.LookPath("mq")
	if err != nil {
		return "", fmt.Errorf("mq binary not found on PATH — install from https://github.com/harehare/mq")
	}
	return bin, nil
}

// runMQ invokes mq as a subprocess with the given args and input file.
// Returns captured stdout. On non-zero exit, the error includes stderr.
func runMQ(ctx context.Context, binary string, args []string, inputFile string) (string, error) {
	fullArgs := make([]string, 0, len(args)+1)
	fullArgs = append(fullArgs, args...)
	fullArgs = append(fullArgs, inputFile)

	cmd := exec.CommandContext(ctx, binary, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mq: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
