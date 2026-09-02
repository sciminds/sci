// Package csl reads CSL-JSON (the citation record doi.org, Crossref and
// DataCite serve under content negotiation) and maps it onto the item data
// Zotero's Web API accepts. It is pure — no I/O — so the network client
// that fetches the record (internal/zot/doiorg) can stay the only package
// on this side of the tree that reaches net/http.
//
// Two things about CSL-JSON drive the shape of [Parse]: registrants
// disagree on scalar-vs-list for title, container-title, ISSN and ISBN
// (DataCite lists, Crossref does not), and some send volume or issue as a
// JSON number. Every string field therefore tolerates a string, a list of
// strings (first entry wins) or a number.
//
// [ToItem] never invents a field: it takes the item type's own field list
// (from api.ItemTypeFields) and its creator types, so a value the type has
// no slot for is either rerouted the way Zotero itself does it (a DOI on a
// book goes into `extra` as "DOI: …") or dropped.
package csl

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

// Name is one contributor. Personal names carry Family and Given;
// institutional ones carry Literal alone.
type Name struct {
	Family  string
	Given   string
	Literal string
}

// Date is a CSL date-parts triple. Zero components are unknown, so a
// year-only date has Month == Day == 0.
type Date struct {
	Year  int
	Month int
	Day   int
}

// ISO renders the date in the form Zotero's date field takes: "YYYY",
// "YYYY-MM" or "YYYY-MM-DD", as far as the components are known. A day
// with no month is dropped; an unknown year is "".
func (d Date) ISO() string {
	switch {
	case d.Year == 0:
		return ""
	case d.Month == 0:
		return fmt.Sprintf("%04d", d.Year)
	case d.Day == 0:
		return fmt.Sprintf("%04d-%02d", d.Year, d.Month)
	default:
		return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
	}
}

// Record is a CSL-JSON citation record, reduced to the fields a Zotero
// item can hold.
type Record struct {
	DOI            string
	Type           string
	Title          string
	ContainerTitle string
	Publisher      string
	Abstract       string
	URL            string
	Volume         string
	Issue          string
	Page           string
	ISSN           string
	ISBN           string
	Authors        []Name
	Editors        []Name
	Issued         Date
}

// flexString decodes a CSL field that registrants serialize as a string,
// a list of strings or a number. A list yields its first entry; anything
// else (null, an object) yields "".
type flexString string

// UnmarshalJSON implements json.Unmarshaler, accepting the three shapes
// registrants use for one scalar field.
func (f *flexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		if len(list) > 0 {
			*f = flexString(list[0])
		}
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexString(n.String())
		return nil
	}
	*f = ""
	return nil
}

// wireName is a CSL name object.
type wireName struct {
	Family  string `json:"family"`
	Given   string `json:"given"`
	Literal string `json:"literal"`
}

// wireRecord is the CSL-JSON wire form of the fields Record keeps.
type wireRecord struct {
	DOI            string     `json:"DOI"`
	Type           string     `json:"type"`
	Title          flexString `json:"title"`
	ContainerTitle flexString `json:"container-title"`
	Publisher      flexString `json:"publisher"`
	Abstract       flexString `json:"abstract"`
	URL            flexString `json:"URL"`
	Volume         flexString `json:"volume"`
	Issue          flexString `json:"issue"`
	Page           flexString `json:"page"`
	ISSN           flexString `json:"ISSN"`
	ISBN           flexString `json:"ISBN"`
	Author         []wireName `json:"author"`
	Editor         []wireName `json:"editor"`
	Issued         struct {
		DateParts [][]any `json:"date-parts"`
	} `json:"issued"`
}

// Parse decodes one CSL-JSON record. The DOI comes back lower-cased and
// trimmed: DOIs are case-insensitive by spec and DataCite serves them in
// upper case, so one canonical form keeps every later comparison honest.
func Parse(data []byte) (*Record, error) {
	var raw wireRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("csl: decode record: %w", err)
	}
	return &Record{
		DOI:            strings.ToLower(strings.TrimSpace(raw.DOI)),
		Type:           raw.Type,
		Title:          string(raw.Title),
		ContainerTitle: string(raw.ContainerTitle),
		Publisher:      string(raw.Publisher),
		Abstract:       string(raw.Abstract),
		URL:            string(raw.URL),
		Volume:         string(raw.Volume),
		Issue:          string(raw.Issue),
		Page:           string(raw.Page),
		ISSN:           string(raw.ISSN),
		ISBN:           string(raw.ISBN),
		Authors:        names(raw.Author),
		Editors:        names(raw.Editor),
		Issued:         dateFromParts(raw.Issued.DateParts),
	}, nil
}

func names(wire []wireName) []Name {
	if len(wire) == 0 {
		return nil
	}
	return lo.Map(wire, func(n wireName, _ int) Name { return Name(n) })
}

// dateFromParts reads issued.date-parts[0], which is [year], [year,
// month] or [year, month, day]. A part that is not a whole number — a
// season string like "Spring", which Crossref emits for some journals —
// reads as unknown without failing the record.
func dateFromParts(parts [][]any) Date {
	if len(parts) == 0 {
		return Date{}
	}
	first := parts[0]
	at := func(i int) int {
		if i >= len(first) {
			return 0
		}
		return datePart(first[i])
	}
	return Date{Year: at(0), Month: at(1), Day: at(2)}
}

// datePart coerces one date-parts element: a JSON number (decoded as
// float64) or a numeric string. Anything else is 0, unknown.
func datePart(v any) int {
	switch p := v.(type) {
	case float64:
		if p == float64(int(p)) {
			return int(p)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			return n
		}
	}
	return 0
}

// itemTypes maps CSL types onto Zotero item types. Anything not listed is
// a document.
var itemTypes = map[string]string{
	"article":           "journalArticle",
	"article-journal":   "journalArticle",
	"article-magazine":  "magazineArticle",
	"article-newspaper": "newspaperArticle",
	"book":              "book",
	"monograph":         "book",
	"chapter":           "bookSection",
	"paper-conference":  "conferencePaper",
	"thesis":            "thesis",
	"report":            "report",
	"posted-content":    "preprint",
	"dataset":           "dataset",
	"webpage":           "webpage",
}

// ItemType maps a CSL type onto the Zotero item type it becomes. Unknown
// and empty types are "document". This is the plain table; a record can
// override it from its other fields, so callers holding a Record use
// [Record.ItemType] — the type [ToItem] emits — to look the fields up.
func ItemType(cslType string) string {
	if t, ok := itemTypes[cslType]; ok {
		return t
	}
	return "document"
}

// ItemType is the Zotero item type [ToItem] emits for this record: the
// [ItemType] table, except that an arXiv record is always a preprint.
// DataCite types arXiv records as a bare "article" (verified live on
// 10.48550/arXiv.1706.03762), which the table would file as a journal
// article.
func (r *Record) ItemType() string {
	if r.isArxiv() {
		return "preprint"
	}
	return ItemType(r.Type)
}

// arxivDOIPrefix opens every DOI arXiv registers, compared lower-cased.
const arxivDOIPrefix = "10.48550/arxiv."

// isArxiv reports whether the record is an arXiv preprint: an arXiv DOI,
// or arXiv named as publisher or container.
func (r *Record) isArxiv() bool {
	return r.arxivID() != "" ||
		strings.EqualFold(strings.TrimSpace(r.Publisher), "arxiv") ||
		strings.EqualFold(strings.TrimSpace(r.ContainerTitle), "arxiv")
}

// arxivID is the arXiv identifier the DOI encodes — the suffix after the
// arXiv prefix, case preserved — or "" when the DOI is not an arXiv one.
func (r *Record) arxivID() string {
	if !strings.HasPrefix(strings.ToLower(r.DOI), arxivDOIPrefix) {
		return ""
	}
	return r.DOI[len(arxivDOIPrefix):]
}

// venueFields are the Zotero fields a CSL container-title can land in, in
// preference order; the first one the item type declares wins.
var venueFields = []string{"publicationTitle", "bookTitle", "proceedingsTitle"}

// ToItem maps rec onto the item data POST /items accepts. fields is the
// item type's own field list and creatorTypes its creator types (both from
// the Zotero schema for [Record.ItemType]); a field or creator role the
// type does not declare is never sent. The DOI of a type without a DOI
// field goes into `extra` as "DOI: …", Zotero's own convention; the
// container title goes to the first venue field the type declares
// (publicationTitle, bookTitle, proceedingsTitle) and is dropped when it
// declares none. An arXiv preprint also gets `repository: arXiv` and,
// when its DOI encodes the id, `archiveID: arXiv:<id>` — the two fields
// Zotero's own arXiv translator fills.
func ToItem(rec *Record, fields []string, creatorTypes []string) map[string]any {
	has := lo.Keyify(fields)
	item := map[string]any{"itemType": rec.ItemType()}
	set := func(field, value string) {
		if _, ok := has[field]; ok && value != "" {
			item[field] = value
		}
	}
	if rec.isArxiv() {
		set("repository", "arXiv")
		if id := rec.arxivID(); id != "" {
			set("archiveID", "arXiv:"+id)
		}
	}
	set("title", rec.Title)
	set("abstractNote", rec.Abstract)
	set("url", rec.URL)
	set("volume", rec.Volume)
	set("issue", rec.Issue)
	set("pages", rec.Page)
	set("ISSN", rec.ISSN)
	set("ISBN", rec.ISBN)
	set("publisher", rec.Publisher)
	set("date", rec.Issued.ISO())

	if _, ok := has["DOI"]; ok {
		set("DOI", rec.DOI)
	} else if rec.DOI != "" {
		set("extra", "DOI: "+rec.DOI)
	}
	if venue, ok := lo.Find(venueFields, func(f string) bool { _, ok := has[f]; return ok }); ok {
		set(venue, rec.ContainerTitle)
	}

	roles := lo.Keyify(creatorTypes)
	var creators []map[string]any
	if _, ok := roles["author"]; ok {
		creators = append(creators, creatorEntries("author", rec.Authors)...)
	}
	if _, ok := roles["editor"]; ok {
		creators = append(creators, creatorEntries("editor", rec.Editors)...)
	}
	if len(creators) > 0 {
		item["creators"] = creators
	}
	return item
}

// creatorEntries renders names as Zotero creator objects: a two-field name
// for personal names, a single `name` for institutional (literal) ones.
// A name with nothing in it is skipped.
func creatorEntries(role string, list []Name) []map[string]any {
	return lo.FilterMap(list, func(n Name, _ int) (map[string]any, bool) {
		switch {
		case n.Family != "" || n.Given != "":
			return map[string]any{"creatorType": role, "firstName": n.Given, "lastName": n.Family}, true
		case strings.TrimSpace(n.Literal) != "":
			return map[string]any{"creatorType": role, "name": n.Literal}, true
		default:
			return nil, false
		}
	})
}
