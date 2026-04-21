package cli

import (
	"context"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// doctor flag destinations.
var (
	doctorDeep bool
)

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Run every hygiene check and print a library-health dashboard",
		Description: `$ zot doctor                 # fast aggregate across every check
$ zot doctor --deep          # enables fuzzy duplicate matching + uncollected-item orphan scan
$ zot doctor --check missing --check invalid
$ zot doctor --json > health.json

$ zot doctor invalid         # drill into a single check
$ zot doctor missing --field title,creators
$ zot doctor orphans --kind uncollected-item
$ zot doctor duplicates --fuzzy
$ zot doctor citekeys

Bare 'zot doctor' runs every hygiene check in order — invalid, missing,
orphans, duplicates, citekeys — and prints a one-line summary per check
plus an aggregate totals footer. Doctor is strictly read-only; use the
sub-commands ('zot doctor invalid', etc.) for per-finding detail.

Deep mode flips the slow/accurate paths: duplicate detection adds the
fuzzy title pass (~30s on a 5k-item library) and orphans additionally
reports items that live in zero collections. It does NOT stat attachment
files on disk — use 'zot doctor orphans --kind missing-file --check-files'
for that.`,
		Commands: []*cli.Command{
			invalidCommand(),
			missingCommand(),
			orphansCommand(),
			duplicatesCommand(),
			citekeysCommand(),
		},
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "check",
				Usage: "limit run to specific checks (repeatable): invalid, missing, orphans, duplicates, citekeys",
				Local: true,
			},
			&cli.BoolFlag{
				Name:        "deep",
				Usage:       "enable slow/accurate paths (fuzzy duplicates + uncollected-item)",
				Destination: &doctorDeep,
				Local:       true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// StringSliceFlag + per-check validation. We parse ourselves
			// so unknown names fail before touching the DB.
			raw := cmd.StringSlice("check")
			var checks []string
			for _, r := range raw {
				for _, p := range strings.Split(r, ",") {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					name, err := zot.ParseDoctorCheck(p)
					if err != nil {
						return cmdutil.UsageErrorf(cmd, "%s", err.Error())
					}
					checks = append(checks, name)
				}
			}
			cfg, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			res, err := zot.Doctor(db, cfg, zot.DoctorOptions{
				Checks: checks,
				Deep:   doctorDeep,
			})
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, res)
			return nil
		},
	}
}
