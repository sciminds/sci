package cli

import (
	"context"
	"slices"
	"testing"

	"github.com/urfave/cli/v3"
)

// runPatchBuilder parses argv against the same flag set `item update`
// declares and returns what the patch builder made of it.
func runPatchBuilder(t *testing.T, argv ...string) (patch map[string]*string, any bool) {
	t.Helper()
	var title, doi, url, date, abstract, publication, extra string
	cmd := &cli.Command{
		Name: "update",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Destination: &title, Local: true},
			&cli.StringFlag{Name: "doi", Destination: &doi, Local: true},
			&cli.StringFlag{Name: "url", Destination: &url, Local: true},
			&cli.StringFlag{Name: "date", Destination: &date, Local: true},
			&cli.StringFlag{Name: "abstract", Destination: &abstract, Local: true},
			&cli.StringFlag{Name: "publication", Destination: &publication, Local: true},
			&cli.StringFlag{Name: "extra", Destination: &extra, Local: true},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			p, a := buildItemPatch(c)
			patch = map[string]*string{
				"title": p.Title, "doi": p.DOI, "url": p.Url, "date": p.Date,
				"abstract": p.AbstractNote, "publication": p.PublicationTitle, "extra": p.Extra,
			}
			any = a
			return nil
		},
	}
	if err := cmd.Run(context.Background(), slices.Concat([]string{"update"}, argv)); err != nil {
		t.Fatal(err)
	}
	return patch, any
}

// TestBuildItemPatch_ExplicitEmptyClearsTheField.
//
// "leave this alone" and "empty this" are different instructions, and a
// bare string cannot tell them apart — so presence comes from IsSet. The
// bug this pins was subtler than a refusal: strPtr returns nil for an empty
// string, so `--extra ""` produced a patch carrying only key/version/
// itemType. Zotero answered 204, the CLI printed "✓ updated item", and
// nothing changed. Same shape as the 709 empty patches that once reported
// "applied 709 of 709", and the reason a write path gets a wire-level test.
func TestBuildItemPatch_ExplicitEmptyClearsTheField(t *testing.T) {
	t.Parallel()
	patch, any := runPatchBuilder(t, "--extra", "")
	if !any {
		t.Fatal("an explicitly emptied field did not count as a field")
	}
	if patch["extra"] == nil {
		t.Fatal(`--extra "" produced a nil pointer: the PATCH would carry no extra at all`)
	}
	if *patch["extra"] != "" {
		t.Errorf(`extra = %q, want ""`, *patch["extra"])
	}
	// Everything the caller did not name stays absent, or a one-field edit
	// silently blanks the rest of the item.
	for _, f := range []string{"title", "doi", "url", "date", "abstract", "publication"} {
		if patch[f] != nil {
			t.Errorf("%s was included though it was never passed", f)
		}
	}
}

func TestBuildItemPatch_UnsetFieldsStayAbsent(t *testing.T) {
	t.Parallel()
	patch, any := runPatchBuilder(t, "--title", "New Title")
	if !any {
		t.Fatal("a set field did not count")
	}
	if patch["title"] == nil || *patch["title"] != "New Title" {
		t.Errorf("title = %v", patch["title"])
	}
	if patch["extra"] != nil {
		t.Error("extra was included though it was never passed")
	}
	if _, none := runPatchBuilder(t); none {
		t.Error("a command with no field flags reported a field")
	}
}
