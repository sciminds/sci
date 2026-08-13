package cli

import (
	"context"
	"strings"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

// doctor flag destinations.
var (
	doctorDeep bool
)

// pdfAcquisitionMoved is the one-sentence explanation the retired `doctor
// pdfs` stub carries. It says what changed and why, so the error teaches
// the boundary rather than just redirecting: finding a missing PDF means
// a metered OpenAlex lookup, an HTTP download, and an upload into Zotero —
// three writes wearing a check's clothes.
const pdfAcquisitionMoved = "PDF acquisition is a credentialed, metered, " +
	"network-writing job, not a hygiene check — it moved to the zot binary, " +
	"which owns the Zotero credential and runs it unattended"

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Run every hygiene check and print a library-health dashboard",
		Description: `$ sci zot doctor                 # fast aggregate across every check
$ sci zot doctor --deep          # enables fuzzy duplicate matching + uncollected-item orphan scan
$ sci zot doctor --check missing --check invalid
$ sci zot doctor --json > health.json

$ sci zot doctor invalid         # drill into a single check
$ sci zot doctor missing --field title,creators
$ sci zot doctor orphans --kind uncollected-item
$ sci zot doctor duplicates --fuzzy
$ sci zot doctor citekeys

Bare 'sci zot doctor' runs every hygiene check in order — invalid, missing,
orphans, duplicates, citekeys — and prints a one-line summary per check
plus an aggregate totals footer. Use the sub-commands ('sci zot doctor
invalid', etc.) for per-finding detail.

Doctor is reporting only. Every check reads the local Zotero database and
nothing else: no writes, no network, no metered lookups. The repairs the
reports point at (DOI rewrites, cite-key rewrites, metadata enrichment,
PDF acquisition) belong to the zot binary, which owns the credential and
runs them unattended.

Deep mode flips the slow/accurate paths: duplicate detection adds the
fuzzy title pass (~30s on a 5k-item library) and orphans additionally
reports items that live in zero collections. It does NOT stat attachment
files on disk — use 'sci zot doctor orphans --kind missing-file --check-files'
for that.`,
		Commands: []*cli.Command{
			invalidCommand(),
			missingCommand(),
			orphansCommand(),
			duplicatesCommand(),
			citekeysCommand(),
			doisCommand(),
			movedToZotCommand("pdfs", "moved to `zot doctor pdfs`",
				[]string{"doctor", "pdfs"}, "zot doctor pdfs",
				pdfAcquisitionMoved),
		},
		Flags: []cli.Flag{
			// lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringSliceFlag{
				Name:  "check",
				Usage: "limit run to specific checks (repeatable): invalid, missing, orphans, duplicates, citekeys",
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
				for p := range strings.SplitSeq(r, ",") {
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
			outputScoped(ctx, cmd, res)
			return nil
		},
	}
}
