package cliui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestRenderHelp_RootBanner(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cmd := &cli.Command{
		Name:   "sci",
		Usage:  "Your scientific computing toolkit",
		Writer: &buf,
		Commands: []*cli.Command{
			{Name: "guide", Category: "Getting Started", Usage: "Learn basics"},
			{Name: "db", Category: "Commands", Usage: "Database manager"},
			{Name: "doctor", Category: "Maintenance", Usage: "Check setup"},
		},
	}
	SetupHelp(cmd)
	_ = cmd.Run(context.Background(), []string{"sci", "--help"})
	out := buf.String()

	if !strings.Contains(out, "sci") {
		t.Errorf("root help should contain 'sci', got:\n%s", out)
	}
	if !strings.Contains(out, "Getting Started") {
		t.Errorf("root help should show 'Getting Started' category, got:\n%s", out)
	}
	if !strings.Contains(out, "Commands") {
		t.Errorf("root help should show 'Commands' category, got:\n%s", out)
	}
	if !strings.Contains(out, "Maintenance") {
		t.Errorf("root help should show 'Maintenance' category, got:\n%s", out)
	}
}

func TestRenderHelp_Subcommand(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cmd := &cli.Command{
		Name:   "sci",
		Writer: &buf,
		Commands: []*cli.Command{
			{
				Name:  "db",
				Usage: "Database manager",
				Commands: []*cli.Command{
					{Name: "view", Usage: "Browse data"},
				},
			},
		},
	}
	SetupHelp(cmd)
	_ = cmd.Run(context.Background(), []string{"sci", "db", "--help"})
	out := buf.String()

	if !strings.Contains(out, "Database manager") {
		t.Errorf("subcommand help should show description, got:\n%s", out)
	}
	if !strings.Contains(out, "view") {
		t.Errorf("subcommand help should list child commands, got:\n%s", out)
	}
}

func TestRenderHelp_Examples(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cmd := &cli.Command{
		Name:   "sci",
		Writer: &buf,
		Commands: []*cli.Command{
			{
				Name:        "doctor",
				Usage:       "Check setup",
				Description: "$ sci doctor",
			},
		},
	}
	SetupHelp(cmd)
	_ = cmd.Run(context.Background(), []string{"sci", "doctor", "--help"})
	out := buf.String()

	if !strings.Contains(out, "Examples") {
		t.Errorf("help should show Examples section, got:\n%s", out)
	}
	if !strings.Contains(out, "$ sci doctor") {
		t.Errorf("help should show example lines, got:\n%s", out)
	}
}

func TestRenderHelp_UsageLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cmd := &cli.Command{
		Name:   "sci",
		Writer: &buf,
		Commands: []*cli.Command{
			{
				Name:      "add",
				Usage:     "Add files",
				ArgsUsage: "<file>...",
				Action:    func(_ context.Context, _ *cli.Command) error { return nil },
			},
		},
	}
	SetupHelp(cmd)
	_ = cmd.Run(context.Background(), []string{"sci", "add", "--help"})
	out := buf.String()

	if !strings.Contains(out, "sci add <file>...") {
		t.Errorf("usage line should show 'sci add <file>...', got:\n%s", out)
	}
}
