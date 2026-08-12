package cli

// --field and --creator: the escape hatches that let `item add` reach the
// rest of Zotero's schema.
//
// Zotero's item types accept ~30 fields and ~6 creator types each; sci
// modelled six fields and one creator type. That was enough for a journal
// article and nothing else, so filing a book chapter — edition, publisher,
// place, pages, and an editor — meant POSTing to the Web API by hand.
//
// Rather than grow eight more flags for the tail (and another eight for the
// next item type), both flags take any name the ITEM TYPE'S OWN schema
// declares, validated against /itemTypeFields and /itemTypeCreatorTypes
// before anything is written. Nothing here restates Zotero's schema.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/client"
)

// itemSchema is the slice of *api.Client that --field / --creator / --type
// need.
type itemSchema interface {
	ItemTypeFields(ctx context.Context, itemType string) ([]string, error)
	ItemTypeCreatorTypes(ctx context.Context, itemType string) ([]string, error)
	ItemTypes(ctx context.Context) ([]string, error)
}

// dedicatedFieldFlags maps a Zotero field to the flag that already owns it.
//
// Letting --field name one of these would create two ways to say the same
// thing with silent precedence between them — the failure mode where the
// command looks right and writes the other value. Refusing is one line for
// the user to change and zero ambiguity.
var dedicatedFieldFlags = map[string]string{
	"title":            "--title",
	"DOI":              "--doi",
	"url":              "--url",
	"date":             "--date",
	"abstractNote":     "--abstract",
	"extra":            "--extra",
	"publicationTitle": "--publication",
	"bookTitle":        "--publication",
	"proceedingsTitle": "--publication",
}

// parseFieldAssignment splits a `--field name=value` argument.
//
// Only the FIRST `=` separates, because values legitimately contain one
// (a url with a query string). An empty value is kept: it is the same
// "blank this field" instruction --extra "" carries.
func parseFieldAssignment(arg string) (name, value string, err error) {
	name, value, found := strings.Cut(arg, "=")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return "", "", fmt.Errorf("--field %q is not name=value", arg)
	}
	return name, value, nil
}

// parseCreatorAssignment splits a `--creator type:Last, First` argument.
//
// Only the first `:` separates. Both halves are required — a bare name has
// no type and would have to be guessed, and guessing "author" is how a
// mistyped editor silently becomes an author.
func parseCreatorAssignment(arg string) (kind, name string, err error) {
	kind, name, found := strings.Cut(arg, ":")
	kind, name = strings.TrimSpace(kind), strings.TrimSpace(name)
	if !found || kind == "" || name == "" {
		return "", "", fmt.Errorf("--creator %q is not type:name (e.g. \"editor:Gazzaniga, Michael\")", arg)
	}
	return kind, name, nil
}

// applyFields validates every --field against the item type's own schema
// and writes them.
//
// Validation is complete before the first write: a create that fails on its
// fourth field has already been rejected whole by Zotero, but an update
// would have applied three.
func applyFields(ctx context.Context, s itemSchema, data *client.ItemData, itemType string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	valid, err := s.ItemTypeFields(ctx, itemType)
	if err != nil {
		return err
	}
	type assignment struct{ name, value string }
	pending := make([]assignment, 0, len(args))

	for _, arg := range args {
		name, value, err := parseFieldAssignment(arg)
		if err != nil {
			return cmdutil.Coded(cmdutil.CodeUsage, "%v", err).
				WithTry(`pass each as --field name=value, e.g. --field pages=45-70`)
		}
		if flag, owned := dedicatedFieldFlags[name]; owned {
			return cmdutil.Coded(cmdutil.CodeUsage,
				"--field %s is owned by a dedicated flag", name).
				WithTry("use " + flag + " instead")
		}
		if !slices.Contains(valid, name) {
			return cmdutil.Coded(cmdutil.CodeUsage,
				"%q is not a field on item type %q", name, itemType).
				WithTry("fields for " + itemType + ": " + strings.Join(valid, ", "))
		}
		pending = append(pending, assignment{name, value})
	}
	for _, a := range pending {
		api.SetField(data, a.name, a.value)
	}
	return nil
}

// applyCreators builds the creators array from --author and --creator.
//
// --author is shorthand for --creator author:NAME and comes first, because
// author order is a claim about contribution and the common shape is
// "authors, then editors". Naming any creator REPLACES whatever the
// --openalex mapping supplied — a caller listing creators is stating the
// list, not adding to one.
func applyCreators(ctx context.Context, s itemSchema, data *client.ItemData, itemType string, authors, creators []string) error {
	if len(authors) == 0 && len(creators) == 0 {
		return nil
	}
	out := lo.Map(authors, func(a string, _ int) client.Creator { return parseCreator(a) })

	if len(creators) > 0 {
		valid, err := s.ItemTypeCreatorTypes(ctx, itemType)
		if err != nil {
			return err
		}
		for _, arg := range creators {
			kind, name, err := parseCreatorAssignment(arg)
			if err != nil {
				return cmdutil.Coded(cmdutil.CodeUsage, "%v", err).
					WithTry("creator types for " + itemType + ": " + strings.Join(valid, ", "))
			}
			if !slices.Contains(valid, kind) {
				return cmdutil.Coded(cmdutil.CodeUsage,
					"%q is not a creator type on item type %q", kind, itemType).
					WithTry("creator types for " + itemType + ": " + strings.Join(valid, ", "))
			}
			c := parseCreator(name)
			c.CreatorType = kind
			out = append(out, c)
		}
	}
	data.Creators = &out
	return nil
}
