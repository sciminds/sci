package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cass"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

var (
	cassForce bool
)

func cassCommand() *cli.Command {
	return &cli.Command{
		Name:  "cass",
		Usage: "Canvas LMS & GitHub Classroom management",
		Description: "Manage course data, grades, and assignments via Canvas LMS and\n" +
			"GitHub Classroom. Data is synced to a local SQLite database with\n" +
			"a git-like workflow: pull, diff, push.\n\n" +
			"  $ sci cass setup          # one-time Canvas API token\n" +
			"  $ sci cass init           # create cass.yaml for this course\n" +
			"  $ sci cass pull           # fetch students, assignments, submissions\n" +
			"  $ sci cass status         # see what changed",
		Category: "Experimental",
		Commands: []*cli.Command{
			cassSetupCommand(),
			cassInitCommand(),
			cassPullCommand(),
			cassStatusCommand(),
			cassDiffCommand(),
			cassPushCommand(),
			cassMatchCommand(),
			cassRevertCommand(),
			cassLogCommand(),
			cassCanvasCommand(),
		},
	}
}

// --- setup: one-time Canvas API token ---

func cassSetupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Configure Canvas API token (one-time setup)",
		Description: "Saves your Canvas API token to ~/.config/sci/credentials.json.\n\n" +
			"To generate a token:\n" +
			"  1. Log in to your Canvas instance\n" +
			"  2. Go to Account → Settings → Approved Integrations\n" +
			"  3. Click '+ New Access Token' and copy the token\n\n" +
			"  $ sci cass setup",
		Action: func(_ context.Context, cmd *cli.Command) error {
			credPath := cloud.ConfigPath()

			// Check for existing token.
			existing, _ := cass.LoadCanvasToken(credPath)
			if existing != "" {
				fmt.Fprintf(os.Stderr, "  %s Canvas API token already configured.\n", ui.SymOK)
				overwrite := false
				if err := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().
						Title("Replace existing token?").
						Value(&overwrite),
				)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
					return err
				}
				if !overwrite {
					ui.Hint("cancelled")
					return nil
				}
			}

			var token string
			if err := huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Canvas API token").
					Description("Paste the token from Canvas → Account → Settings → Approved Integrations").
					EchoMode(huh.EchoModePassword).
					Value(&token).
					Validate(func(s string) error {
						if len(s) < 10 {
							return fmt.Errorf("token too short")
						}
						return nil
					}),
			)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
				return err
			}

			if err := cass.SaveCanvasToken(credPath, token); err != nil {
				return err
			}

			result := &setupResult{Token: token}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

type setupResult struct {
	Token string `json:"token_prefix"`
}

func (r *setupResult) JSON() any {
	// Only expose a prefix for safety.
	prefix := r.Token
	if len(prefix) > 8 {
		prefix = prefix[:8] + "..."
	}
	return map[string]string{"token_prefix": prefix, "status": "saved"}
}

func (r *setupResult) Human() string {
	return fmt.Sprintf("  %s Canvas API token saved to %s\n", ui.SymOK, cloud.ConfigPath())
}

// --- init: per-project cass.yaml ---

func cassInitCommand() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Initialize a course directory (creates cass.yaml)",
		Description: "Creates a cass.yaml config file and empty database in the current directory.\n\n" +
			"  $ sci cass init\n" +
			"  $ sci cass init --force   # overwrite existing cass.yaml",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "overwrite existing config", Destination: &cassForce, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			dir, _ := os.Getwd()
			configPath := filepath.Join(dir, cass.ConfigFile)

			// Check for existing config.
			if !cassForce {
				if _, err := os.Stat(configPath); err == nil {
					return fmt.Errorf("cass.yaml already exists — use --force to overwrite")
				}
			}

			var canvasURL, classroomURL string
			if err := huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Canvas course URL").
					Description("Paste the full URL from your browser (e.g. https://canvas.ucsd.edu/courses/12345)").
					Value(&canvasURL).
					Validate(func(s string) error {
						_, _, err := cass.ParseCanvasURL(s)
						return err
					}),
				huh.NewInput().
					Title("GitHub Classroom URL (optional)").
					Description("Leave blank for Canvas-only courses").
					Value(&classroomURL).
					Validate(func(s string) error {
						if s == "" {
							return nil
						}
						_, err := cass.ParseClassroomURL(s)
						return err
					}),
			)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
				return err
			}

			cfg := &cass.Config{
				Canvas: cass.CanvasConfig{URL: canvasURL},
			}
			if classroomURL != "" {
				cfg.Classroom = &cass.ClassroomConfig{URL: classroomURL}
			}

			if err := cass.SaveConfig(configPath, cfg); err != nil {
				return err
			}

			// Create empty database.
			dbPath := filepath.Join(dir, "cass.db")
			db, err := cass.OpenDB(dbPath)
			if err != nil {
				return err
			}
			_ = db.Close()

			result := &initResult{ConfigPath: configPath, DBPath: dbPath, HasClassroom: classroomURL != ""}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

type initResult struct {
	ConfigPath   string `json:"config_path"`
	DBPath       string `json:"db_path"`
	HasClassroom bool   `json:"has_classroom"`
}

func (r *initResult) JSON() any { return r }
func (r *initResult) Human() string {
	out := fmt.Sprintf("  %s Created %s\n", ui.SymOK, filepath.Base(r.ConfigPath))
	out += fmt.Sprintf("  %s Created %s\n", ui.SymOK, filepath.Base(r.DBPath))
	if r.HasClassroom {
		out += fmt.Sprintf("  %s GitHub Classroom configured\n", ui.SymOK)
	}
	out += fmt.Sprintf("\n  Next: run %s to fetch course data\n", ui.TUI.Accent().Render("sci cass pull"))
	return out
}

// cassLoadContext finds the cass.yaml and opens the database.
func cassLoadContext() (*cass.Config, *cass.DB, error) {
	dir, _ := os.Getwd()
	configPath, err := cass.FindConfig(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("no cass.yaml found — run 'sci cass init' first")
	}
	cfg, err := cass.LoadConfig(configPath)
	if err != nil {
		return nil, nil, err
	}
	dbPath := filepath.Join(filepath.Dir(configPath), "cass.db")
	db, err := cass.OpenDB(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, db, nil
}

// --- stub commands (to be implemented in later phases) ---

func cassPullCommand() *cli.Command {
	return &cli.Command{
		Name:        "pull",
		Usage:       "Fetch students, assignments, and submissions from Canvas/GitHub",
		Description: "$ sci cass pull",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			credPath := cloud.ConfigPath()
			token, err := cass.RequireCanvasToken(credPath)
			if err != nil {
				return err
			}

			baseURL, courseID, err := cfg.CanvasParts()
			if err != nil {
				return err
			}

			var result cass.PullResult

			err = ui.RunWithSpinner("Fetching students", func() error {
				cl, err := cass.PullStudents(ctx, db, baseURL, token, courseID)
				if err != nil {
					return err
				}
				result.Changelogs = append(result.Changelogs, cl)
				return nil
			})
			if err != nil {
				return err
			}

			err = ui.RunWithSpinner("Fetching assignments", func() error {
				cl, err := cass.PullAssignments(ctx, db, baseURL, token, courseID)
				if err != nil {
					return err
				}
				result.Changelogs = append(result.Changelogs, cl)
				return nil
			})
			if err != nil {
				return err
			}

			err = ui.RunWithSpinner("Fetching submissions", func() error {
				cl, err := cass.PullSubmissions(ctx, db, baseURL, token, courseID)
				if err != nil {
					return err
				}
				result.Changelogs = append(result.Changelogs, cl)
				return nil
			})
			if err != nil {
				return err
			}

			// GitHub Classroom pull (if configured).
			if cfg.HasClassroom() {
				ghToken, err := cass.GetGHTokenFromCLI()
				if err != nil {
					fmt.Fprintf(os.Stderr, "  %s GitHub token not available — skipping Classroom pull (%v)\n", ui.SymWarn, err)
				} else {
					// Resolve classroom API ID if not cached.
					classroomID, resolveErr := cfg.ClassroomAPIID()
					if resolveErr != nil {
						var resolved int
						resolveErr = ui.RunWithSpinner("Resolving GitHub Classroom", func() error {
							var err error
							resolved, err = cass.ResolveClassroomID(ctx, ghToken, cfg.Classroom.URL)
							return err
						})
						if resolveErr != nil {
							return resolveErr
						}
						classroomID = resolved
						cfg.Classroom.APIID = classroomID
						// Cache to config file.
						configPath, _ := cass.FindConfig(filepath.Dir(db.Path))
						_ = cass.SaveConfig(configPath, cfg)
					}
					err = ui.RunWithSpinner("Fetching GitHub assignments", func() error {
						cl, err := cass.PullGHAssignments(ctx, db, ghToken, classroomID)
						if err != nil {
							return err
						}
						result.Changelogs = append(result.Changelogs, cl)
						return nil
					})
					if err != nil {
						return err
					}

					err = ui.RunWithSpinner("Fetching GitHub submissions", func() error {
						cl, err := cass.PullGHSubmissions(ctx, db, ghToken, classroomID)
						if err != nil {
							return err
						}
						result.Changelogs = append(result.Changelogs, cl)
						return nil
					})
					if err != nil {
						return err
					}
				}
			}

			// Record pull time and log.
			now := time.Now().Format(time.RFC3339)
			_ = db.SetMeta("last_pull", now)
			var parts []string
			for _, cl := range result.Changelogs {
				parts = append(parts, fmt.Sprintf("%s: %d new, %d updated", cl.Entity, cl.Added, cl.Updated))
			}
			_ = db.WriteLog("pull", strings.Join(parts, "; "), "")

			cmdutil.Output(cmd, &result)
			return nil
		},
	}
}

func cassStatusCommand() *cli.Command {
	return &cli.Command{
		Name:        "status",
		Usage:       "Show sync status, pending changes, and discrepancies",
		Description: "$ sci cass status",
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			result, err := cass.Status(db, cfg)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassDiffCommand() *cli.Command {
	var remote bool
	return &cli.Command{
		Name:  "diff",
		Usage: "Show pending grade changes",
		Description: "$ sci cass diff            # local baseline vs local edits\n" +
			"$ sci cass diff --remote   # 3-way: baseline vs Canvas vs local",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remote", Usage: "fetch live Canvas scores for 3-way diff", Destination: &remote, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if remote {
				credPath := cloud.ConfigPath()
				token, err := cass.RequireCanvasToken(credPath)
				if err != nil {
					return err
				}
				baseURL, courseID, err := cfg.CanvasParts()
				if err != nil {
					return err
				}

				var result *cass.RemoteDiffResult
				err = ui.RunWithSpinner("Fetching live Canvas scores", func() error {
					var fetchErr error
					result, fetchErr = cass.DiffRemote(ctx, db, baseURL, token, courseID)
					return fetchErr
				})
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}

			result, err := cass.DiffLocal(db)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassPushCommand() *cli.Command {
	var force bool
	return &cli.Command{
		Name:        "push",
		Usage:       "Push grade changes to Canvas",
		Description: "$ sci cass push\n$ sci cass push --force",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "skip conflict check", Destination: &force, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if err := cass.CheckPushGates(db); err != nil {
				return err
			}

			diff, err := cass.DiffLocal(db)
			if err != nil {
				return err
			}
			if len(diff.Changes) == 0 {
				fmt.Fprintf(os.Stderr, "  %s No pending grade changes to push.\n", ui.SymOK)
				return nil
			}

			// Show preview.
			fmt.Fprint(os.Stderr, diff.Human())

			if !force {
				if done, err := cmdutil.ConfirmOrSkip(false, fmt.Sprintf("Push %d grade(s) to Canvas?", len(diff.Changes))); done || err != nil {
					return err
				}
			}

			credPath := cloud.ConfigPath()
			token, err := cass.RequireCanvasToken(credPath)
			if err != nil {
				return err
			}

			baseURL, courseID, err := cfg.CanvasParts()
			if err != nil {
				return err
			}

			var pushed int
			err = ui.RunWithSpinner("Pushing grades to Canvas", func() error {
				var pushErr error
				pushed, pushErr = cass.PushGrades(ctx, db, baseURL, token, courseID, diff.Changes)
				return pushErr
			})
			if err != nil {
				return err
			}

			// Sync shadow table.
			if err := cass.SyncGrades(db); err != nil {
				return err
			}

			_ = db.WriteLog("push", fmt.Sprintf("%d grades pushed", pushed), "")

			fmt.Fprintf(os.Stderr, "  %s Pushed %d grade(s) to Canvas\n", ui.SymOK, pushed)
			return nil
		},
	}
}

func cassMatchCommand() *cli.Command {
	var auto bool
	return &cli.Command{
		Name:  "match",
		Usage: "Interactively match GitHub usernames to Canvas students",
		Description: "Matches GitHub Classroom users to Canvas students by name.\n" +
			"High-confidence matches (exact name) are applied automatically.\n" +
			"Remaining ambiguous matches are presented for manual selection.\n\n" +
			"  $ sci cass match\n" +
			"  $ sci cass match --auto   # skip manual matching, only auto-match",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "auto", Usage: "only apply exact matches, skip interactive", Destination: &auto, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if !cfg.HasClassroom() {
				fmt.Fprintf(os.Stderr, "  %s No GitHub Classroom configured — nothing to match.\n", ui.SymOK)
				return nil
			}

			result, err := cass.RunMatch(db, auto)
			if err != nil {
				return err
			}

			_ = db.WriteLog("match", fmt.Sprintf("%d matched, %d unmatched", result.Matched, result.Unmatched), "")
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassRevertCommand() *cli.Command {
	var yes bool
	return &cli.Command{
		Name:        "revert",
		Usage:       "Discard unpushed grade edits",
		Description: "$ sci cass revert",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &yes, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			diff, err := cass.DiffLocal(db)
			if err != nil {
				return err
			}
			if len(diff.Changes) == 0 {
				fmt.Fprintf(os.Stderr, "  %s No pending changes to revert.\n", ui.SymOK)
				return nil
			}

			if done, err := cmdutil.ConfirmOrSkip(yes, fmt.Sprintf("Revert %d pending grade change(s)?", len(diff.Changes))); done || err != nil {
				return err
			}

			count, err := cass.Revert(db)
			if err != nil {
				return err
			}

			_ = db.WriteLog("revert", fmt.Sprintf("%d grades reverted", count), "")
			fmt.Fprintf(os.Stderr, "  %s Reverted %d grade(s)\n", ui.SymOK, count)
			return nil
		},
	}
}

func cassLogCommand() *cli.Command {
	return &cli.Command{
		Name:        "log",
		Usage:       "Show operation history",
		Description: "$ sci cass log",
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, db, err := cassLoadContext()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			entries, err := db.ReadLog(50)
			if err != nil {
				return err
			}
			result := &cass.LogResult{Entries: entries}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassCanvasCommand() *cli.Command {
	return &cli.Command{
		Name:  "canvas",
		Usage: "Direct Canvas LMS management",
		Description: "Imperative commands that interact with Canvas API directly.\n\n" +
			"  $ sci cass canvas modules\n" +
			"  $ sci cass canvas assignments\n" +
			"  $ sci cass canvas announce",
		Commands: []*cli.Command{
			cassCanvasModulesCommand(),
			cassCanvasAssignmentsCommand(),
			cassCanvasAnnounceCommand(),
			cassCanvasFilesCommand(),
		},
	}
}

func cassCanvasModulesCommand() *cli.Command {
	var createName string
	var publishID, unpublishID int
	var deleteID int
	return &cli.Command{
		Name:  "modules",
		Usage: "List, create, publish, or delete course modules",
		Description: "$ sci cass canvas modules\n" +
			"$ sci cass canvas modules --create 'Week 3'\n" +
			"$ sci cass canvas modules --publish 123\n" +
			"$ sci cass canvas modules --delete 123",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "create", Usage: "create a new module with this name", Destination: &createName, Local: true},
			&cli.IntFlag{Name: "publish", Usage: "publish module by ID", Destination: &publishID, Local: true},
			&cli.IntFlag{Name: "unpublish", Usage: "unpublish module by ID", Destination: &unpublishID, Local: true},
			&cli.IntFlag{Name: "delete", Usage: "delete module by ID", Destination: &deleteID, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			baseURL, courseID, token, err := cassCanvasContext()
			if err != nil {
				return err
			}

			if createName != "" {
				result, err := cass.CreateModule(ctx, baseURL, token, courseID, createName)
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if publishID > 0 {
				result, err := cass.PublishModule(ctx, baseURL, token, courseID, publishID, true)
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if unpublishID > 0 {
				result, err := cass.PublishModule(ctx, baseURL, token, courseID, unpublishID, false)
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if deleteID > 0 {
				if done, err := cmdutil.ConfirmOrSkip(false, fmt.Sprintf("Delete module %d?", deleteID)); done || err != nil {
					return err
				}
				if err := cass.DeleteModule(ctx, baseURL, token, courseID, deleteID); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "  %s Deleted module %d\n", ui.SymOK, deleteID)
				return nil
			}

			result, err := cass.ListModules(ctx, baseURL, token, courseID)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassCanvasAssignmentsCommand() *cli.Command {
	var createName string
	var points float64
	var deleteID int
	var publishID, unpublishID int
	return &cli.Command{
		Name:  "assignments",
		Usage: "List, create, publish, or delete assignments",
		Description: "$ sci cass canvas assignments\n" +
			"$ sci cass canvas assignments --create 'Lab 3' --points 20\n" +
			"$ sci cass canvas assignments --publish 123\n" +
			"$ sci cass canvas assignments --delete 123",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "create", Usage: "create a new assignment with this name", Destination: &createName, Local: true},
			&cli.FloatFlag{Name: "points", Usage: "points possible (for create/update)", Destination: &points, Local: true},
			&cli.IntFlag{Name: "publish", Usage: "publish assignment by ID", Destination: &publishID, Local: true},
			&cli.IntFlag{Name: "unpublish", Usage: "unpublish assignment by ID", Destination: &unpublishID, Local: true},
			&cli.IntFlag{Name: "delete", Usage: "delete assignment by ID", Destination: &deleteID, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			baseURL, courseID, token, err := cassCanvasContext()
			if err != nil {
				return err
			}

			if createName != "" {
				result, err := cass.CreateCanvasAssignment(ctx, baseURL, token, courseID, cass.AssignmentSpec{
					Name:   createName,
					Points: points,
				})
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if publishID > 0 {
				result, err := cass.UpdateCanvasAssignment(ctx, baseURL, token, courseID, publishID, cass.AssignmentSpec{Published: true})
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if unpublishID > 0 {
				result, err := cass.UpdateCanvasAssignment(ctx, baseURL, token, courseID, unpublishID, cass.AssignmentSpec{})
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if deleteID > 0 {
				if done, err := cmdutil.ConfirmOrSkip(false, fmt.Sprintf("Delete assignment %d?", deleteID)); done || err != nil {
					return err
				}
				if err := cass.DeleteCanvasAssignment(ctx, baseURL, token, courseID, deleteID); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "  %s Deleted assignment %d\n", ui.SymOK, deleteID)
				return nil
			}

			result, err := cass.ListCanvasAssignments(ctx, baseURL, token, courseID)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassCanvasAnnounceCommand() *cli.Command {
	var title, message string
	var list bool
	var deleteID int
	return &cli.Command{
		Name:  "announce",
		Usage: "List, post, or delete course announcements",
		Description: "$ sci cass canvas announce --list\n" +
			"$ sci cass canvas announce --title 'Hello' --message 'Welcome!'\n" +
			"$ sci cass canvas announce --delete 123",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "list", Aliases: []string{"l"}, Usage: "list announcements", Destination: &list, Local: true},
			&cli.StringFlag{Name: "title", Aliases: []string{"t"}, Usage: "announcement title", Destination: &title, Local: true},
			&cli.StringFlag{Name: "message", Aliases: []string{"m"}, Usage: "announcement body (HTML)", Destination: &message, Local: true},
			&cli.IntFlag{Name: "delete", Usage: "delete announcement by ID", Destination: &deleteID, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			baseURL, courseID, token, err := cassCanvasContext()
			if err != nil {
				return err
			}

			if list {
				result, err := cass.ListAnnouncements(ctx, baseURL, token, courseID)
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, result)
				return nil
			}
			if deleteID > 0 {
				if done, err := cmdutil.ConfirmOrSkip(false, fmt.Sprintf("Delete announcement %d?", deleteID)); done || err != nil {
					return err
				}
				if err := cass.DeleteAnnouncement(ctx, baseURL, token, courseID, deleteID); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "  %s Deleted announcement %d\n", ui.SymOK, deleteID)
				return nil
			}
			if title == "" || message == "" {
				return cmdutil.UsageErrorf(cmd, "--title and --message are required (or use --list)")
			}

			result, err := cass.PostAnnouncement(ctx, baseURL, token, courseID, title, message)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

func cassCanvasFilesCommand() *cli.Command {
	return &cli.Command{
		Name:        "files",
		Usage:       "List course files",
		Description: "$ sci cass canvas files",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			baseURL, courseID, token, err := cassCanvasContext()
			if err != nil {
				return err
			}

			result, err := cass.ListFiles(ctx, baseURL, token, courseID)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, result)
			return nil
		},
	}
}

// cassCanvasContext loads config and credentials needed for canvas management commands.
func cassCanvasContext() (baseURL string, courseID int, token string, err error) {
	dir, _ := os.Getwd()
	configPath, err := cass.FindConfig(dir)
	if err != nil {
		return "", 0, "", fmt.Errorf("no cass.yaml found — run 'sci cass init' first")
	}
	cfg, err := cass.LoadConfig(configPath)
	if err != nil {
		return "", 0, "", err
	}

	credPath := cloud.ConfigPath()
	token, err = cass.RequireCanvasToken(credPath)
	if err != nil {
		return "", 0, "", err
	}

	baseURL, courseID, err = cfg.CanvasParts()
	return baseURL, courseID, token, err
}
