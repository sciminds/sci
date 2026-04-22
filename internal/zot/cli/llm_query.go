package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// llm query flag destinations.
var (
	llmQuerySearch     string
	llmQueryTag        string
	llmQueryCollection string
	llmQueryKey        []string
	llmQueryLimit      int
)

func llmQueryCommand() *cli.Command {
	return &cli.Command{
		Name:  "query",
		Usage: "Filter notes by metadata, then query content via mq",
		Description: "$ zot llm query -s transformers -- .h2\n" +
			"$ zot llm query -t ml -n 10\n" +
			"$ zot llm query -k ABC123 -k DEF456 -- .p",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "search", Aliases: []string{"s"}, Usage: "Cross-field text search", Destination: &llmQuerySearch, Local: true},
			&cli.StringFlag{Name: "tag", Aliases: []string{"t"}, Usage: "Filter by tag", Destination: &llmQueryTag, Local: true},
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "Filter by collection key", Destination: &llmQueryCollection, Local: true},
			&cli.StringSliceFlag{Name: "key", Aliases: []string{"k"}, Usage: "Specific parent key(s) (repeatable)", Destination: &llmQueryKey}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Usage: "Max notes to process (0 = all)", Destination: &llmQueryLimit, Local: true},
		},
		Action: llmQueryAction,
	}
}

func llmQueryAction(ctx context.Context, cmd *cli.Command) error {
	keys := llmQueryKey
	if llmQuerySearch == "" && llmQueryTag == "" && llmQueryCollection == "" && len(keys) == 0 {
		return fmt.Errorf("at least one filter is required (-s, -t, -c, or -k); use 'llm catalog' to browse all notes")
	}

	// Args after "--" are mq arguments.
	mqArgs := lo.Filter(cmd.Args().Slice(), func(s string, _ int) bool { return s != "--" })

	mqBin, err := resolveMQ()
	if err != nil {
		return err
	}

	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Stage 1: collect candidate notes.
	type noteCandidate struct {
		ParentKey   string
		ParentTitle string
		NoteKey     string
		Body        string
	}

	var candidates []noteCandidate

	if len(keys) > 0 {
		// Explicit keys — look up each parent's docling notes.
		for _, pk := range keys {
			notes, err := db.ListDoclingNotes(pk)
			if err != nil {
				return fmt.Errorf("list notes for %s: %w", pk, err)
			}
			parent, _ := db.Read(pk) // best-effort metadata
			for _, ch := range notes {
				title := ""
				if parent != nil {
					title = parent.Title
				}
				candidates = append(candidates, noteCandidate{
					ParentKey:   pk,
					ParentTitle: title,
					NoteKey:     ch.Key,
					Body:        ch.Note,
				})
			}
		}
	} else if llmQuerySearch != "" || llmQueryTag != "" || llmQueryCollection != "" {
		// Metadata filter → intersect with docling notes.
		hasNotes, err := db.ParentsWithDoclingNotes()
		if err != nil {
			return err
		}

		if llmQuerySearch != "" {
			limit := llmQueryLimit
			if limit == 0 {
				limit = 500
			}
			items, err := db.Search(llmQuerySearch, limit)
			if err != nil {
				return err
			}
			for _, item := range items {
				if !hasNotes[item.Key] {
					continue
				}
				notes, err := db.ListDoclingNotes(item.Key)
				if err != nil {
					continue
				}
				for _, ch := range notes {
					candidates = append(candidates, noteCandidate{
						ParentKey:   item.Key,
						ParentTitle: item.Title,
						NoteKey:     ch.Key,
						Body:        ch.Note,
					})
				}
			}
		} else {
			filter := local.ListFilter{
				Tag:           llmQueryTag,
				CollectionKey: llmQueryCollection,
			}
			if llmQueryLimit > 0 {
				filter.Limit = llmQueryLimit
			}
			items, err := db.ListAll(filter)
			if err != nil {
				return err
			}
			for _, item := range items {
				if !hasNotes[item.Key] {
					continue
				}
				notes, err := db.ListDoclingNotes(item.Key)
				if err != nil {
					continue
				}
				for _, ch := range notes {
					candidates = append(candidates, noteCandidate{
						ParentKey:   item.Key,
						ParentTitle: item.Title,
						NoteKey:     ch.Key,
						Body:        ch.Note,
					})
				}
			}
		}
	}

	if llmQueryLimit > 0 && len(candidates) > llmQueryLimit {
		candidates = candidates[:llmQueryLimit]
	}

	if len(candidates) == 0 {
		cmdutil.Output(cmd, zot.LLMQueryResult{MqQuery: strings.Join(mqArgs, " ")})
		return nil
	}

	// If no mq args, dump raw content (same as read, but filtered).
	if len(mqArgs) == 0 {
		entries := lo.Map(candidates, func(nc noteCandidate, _ int) zot.LLMQueryMatch {
			return zot.LLMQueryMatch{
				Key:    nc.ParentKey,
				Title:  nc.ParentTitle,
				Output: noteBodyForMQ(nc.Body),
			}
		})
		cmdutil.Output(cmd, zot.LLMQueryResult{
			Matched: len(entries),
			Results: entries,
		})
		return nil
	}

	// Stage 2: pipe through mq.
	tmpDir, err := os.MkdirTemp("", "sci-llm-query-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	var results []zot.LLMQueryMatch
	var skipped int

	for _, nc := range candidates {
		body := noteBodyForMQ(nc.Body)
		html := isHTMLNote(nc.Body)

		tmpFile := filepath.Join(tmpDir, nc.NoteKey+".md")
		if err := os.WriteFile(tmpFile, []byte(body), 0o600); err != nil {
			return fmt.Errorf("write temp file for %s: %w", nc.NoteKey, err)
		}

		// For HTML-mode notes, prepend -I html so mq parses them correctly.
		args := mqArgs
		if html {
			args = slices.Concat([]string{"-I", "html"}, args)
		}

		output, err := runMQ(ctx, mqBin, args, tmpFile)
		if err != nil {
			// HTML notes may not work well with all mq queries — skip gracefully.
			if html {
				skipped++
				continue
			}
			return fmt.Errorf("mq query on %s (%s): %w", nc.ParentKey, nc.ParentTitle, err)
		}

		output = strings.TrimRight(output, "\n")
		if output == "" {
			continue
		}

		results = append(results, zot.LLMQueryMatch{
			Key:    nc.ParentKey,
			Title:  nc.ParentTitle,
			Output: output,
		})
	}

	cmdutil.Output(cmd, zot.LLMQueryResult{
		MqQuery: strings.Join(mqArgs, " "),
		Matched: len(results),
		Skipped: skipped,
		Results: results,
	})
	return nil
}
