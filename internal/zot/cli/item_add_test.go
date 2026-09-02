package cli

// `sci zot item add`, driven through the production tree against the fake
// Zotero, a fake doi.org and a stubbed PDF sniff. Every test swaps package
// globals, so none may t.Parallel().

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot/bib"
	"github.com/sciminds/sci/internal/zot/doiorg"
	"github.com/sciminds/sci/internal/zot/pdfid"
)

// fakeDOIOrg serves CSL-JSON records by DOI, case-insensitively, and 404s
// the rest — the two answers doi.org gives.
type fakeDOIOrg struct {
	mu       sync.Mutex
	records  map[string]string
	requests []string
}

func installFakeDOIOrg(t *testing.T) *fakeDOIOrg {
	t.Helper()
	f := &fakeDOIOrg{records: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	prev := newDOIResolver
	newDOIResolver = func() *doiorg.Client {
		return &doiorg.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	}
	t.Cleanup(func() { newDOIResolver = prev })
	return f
}

// add registers a record under doi. The CSL type is the caller's; the
// record carries one author, a container title and a 2013 date.
func (f *fakeDOIOrg) add(doi, cslType, title, container string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[strings.ToLower(doi)] = fmt.Sprintf(`{
		"DOI": %q, "type": %q, "title": %q, "container-title": %q,
		"author": [{"family": "Smith", "given": "John"}, {"family": "Doe", "given": "Jane"}],
		"issued": {"date-parts": [[2013, 5, 1]]}, "volume": "498", "page": "1-2"
	}`, doi, cslType, title, container)
}

// addRaw registers a verbatim CSL-JSON body.
func (f *fakeDOIOrg) addRaw(doi, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[strings.ToLower(doi)] = body
}

func (f *fakeDOIOrg) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func (f *fakeDOIOrg) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doi := strings.TrimPrefix(r.URL.Path, "/")
	f.requests = append(f.requests, doi)
	body, ok := f.records[strings.ToLower(doi)]
	if !ok {
		http.Error(w, "DOI not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.citationstyles.csl+json")
	_, _ = w.Write([]byte(body))
}

// stubSniff replaces the PDF identifier sniff with a table keyed by base
// filename; a file not in the table reports no identifier.
func stubSniff(t *testing.T, table map[string][]bib.Ref) {
	t.Helper()
	prev := sniffPDF
	sniffPDF = func(_ context.Context, path string) ([]bib.Ref, error) {
		refs, ok := table[filepath.Base(path)]
		if !ok {
			return nil, fmt.Errorf("%s: %w", path, pdfid.ErrNoIdentifier)
		}
		return refs, nil
	}
	t.Cleanup(func() { sniffPDF = prev })
}

func stubSniffErr(t *testing.T, err error) {
	t.Helper()
	prev := sniffPDF
	sniffPDF = func(context.Context, string) ([]bib.Ref, error) { return nil, err }
	t.Cleanup(func() { sniffPDF = prev })
}

func doiRef(doi string) bib.Ref { return bib.Ref{Raw: doi, Kind: bib.KindDOI, Value: doi} }

func arxivRef(id string) bib.Ref { return bib.Ref{Raw: "arXiv:" + id, Kind: bib.KindArxiv, Value: id} }

// runWriteWithStderr is runWrite with stderr captured too, for the notes a
// verb prints beside its result.
func runWriteWithStderr(t *testing.T, args ...string) (stdout []byte, stderr string, err error) {
	t.Helper()
	readErr, restoreErr := captureStderr(t)
	stdout, err = runWrite(t, args...)
	restoreErr()
	return stdout, readErr(), err
}

// writePDF drops a fake PDF file into dir and returns its path.
func writePDF(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("%PDF-1.4 "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedLocalItemWithDOI mirrors an item with a title and DOI into the local
// database, so the dedup gate has something to hit.
func (fx *writeFixture) seedLocalItemWithDOI(t *testing.T, itemID int, key, title, doi string) {
	t.Helper()
	id := strconv.Itoa(itemID)
	fx.exec(t,
		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified)
			VALUES (`+id+`, 1, 1, '`+key+`', 1, '2024-01-01 10:00:00', '2024-01-01 10:00:00', '2024-01-01 10:00:00')`,
		`INSERT INTO itemDataValues (valueID, value) VALUES (`+id+`01, '`+title+`'), (`+id+`03, '`+doi+`')`,
		`INSERT INTO itemData (itemID, fieldID, valueID) VALUES (`+id+`, 1, `+id+`01), (`+id+`, 3, `+id+`03)`,
	)
}

// writeData reads the map a WriteResult's Data decoded into.
func writeData(t *testing.T, out []byte) map[string]any {
	t.Helper()
	data, ok := decodeWrite(t, out).Data.(map[string]any)
	if !ok {
		t.Fatalf("Data is not an object: %s", out)
	}
	return data
}

// --- identifiers ----------------------------------------------------------

func TestItemAdd_ByDOI_CreatesFromTheRegistryRecord(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")

	out, err := runWrite(t, "--json", "item", "add", "https://doi.org/10.1038/NATURE12373",
		"--collection", "reading list", "--tag", "to-read", "--tag", "sleep")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Action != "created" || res.Kind != "item" || res.Target != "NEW00001" {
		t.Errorf("got %+v", res)
	}
	created := fx.fake.item("NEW00001")
	if created == nil {
		t.Fatal("no item on the server")
	}
	want := map[string]any{
		"itemType": "journalArticle", "title": "Deep sleep", "publicationTitle": "Nature",
		"DOI": "10.1038/nature12373", "date": "2013-05-01", "volume": "498", "pages": "1-2",
	}
	for k, v := range want {
		if created.Data[k] != v {
			t.Errorf("server data[%s] = %v, want %v", k, created.Data[k], v)
		}
	}
	creators, _ := created.Data["creators"].([]any)
	if len(creators) != 2 {
		t.Fatalf("creators = %v, want the two authors", created.Data["creators"])
	}
	if first, _ := creators[0].(map[string]any); first["lastName"] != "Smith" || first["creatorType"] != "author" {
		t.Errorf("first creator = %v", creators[0])
	}
	if got := created.Data["collections"]; !slices.Equal(anyStrings(got), []string{"COLL0001"}) {
		t.Errorf("collections = %v, want [COLL0001] (resolved by name)", got)
	}
	tags, _ := created.Data["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags = %v, want to-read and sleep", created.Data["tags"])
	}
	data := writeData(t, out)
	item, _ := data["item"].(map[string]any)
	if item["key"] != "NEW00001" || data["attached"] != false {
		t.Errorf("data = %v, want the server item and attached:false", data)
	}
}

func anyStrings(v any) []string {
	raw, _ := v.([]any)
	return lo.Map(raw, func(s any, _ int) string { return fmt.Sprint(s) })
}

func TestItemAdd_ByDOI_UnregisteredIsNotFound(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)

	_, err := runWrite(t, "--json", "item", "add", "10.9999/nothing")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotFound || !strings.Contains(coded.Message, "doi.org has no record of 10.9999/nothing") {
		t.Errorf("got %+v", coded)
	}
	if len(fx.fake.requestsMatching("POST ")) != 0 {
		t.Error("nothing may be written for an unregistered DOI")
	}
}

func TestItemAdd_ByDOI_DuplicateNamesTheExistingItem(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalItemWithDOI(t, 7, "ITEM0007", "Deep sleep", "10.1038/Nature12373")
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")

	_, err := runWrite(t, "--json", "item", "add", "10.1038/nature12373")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeDuplicate {
		t.Errorf("Code = %q, want duplicate", coded.Code)
	}
	if !strings.Contains(coded.Message, "ITEM0007") || !strings.Contains(coded.Message, "Deep sleep") {
		t.Errorf("Message should name the existing key and title: %q", coded.Message)
	}
	if !strings.Contains(coded.Try, "--force") {
		t.Errorf("Try should name --force: %q", coded.Try)
	}
	if len(fx.fake.requestsMatching("POST ")) != 0 {
		t.Error("a duplicate must not be created")
	}
}

func TestItemAdd_ByDOI_ForceCreatesASecondCopy(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalItemWithDOI(t, 7, "ITEM0007", "Deep sleep", "10.1038/nature12373")
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")

	out, err := runWrite(t, "--json", "item", "add", "10.1038/nature12373", "--force")
	if err != nil {
		t.Fatalf("add --force: %v\n%s", err, out)
	}
	if fx.fake.item("NEW00001") == nil {
		t.Error("--force should create the item anyway")
	}
}

func TestItemAdd_ByArxivID_ResolvesTheDataCiteDOIAsAPreprint(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.addRaw("10.48550/arXiv.1706.03762", `{"DOI": "10.48550/ARXIV.1706.03762", "type": "article",
		"title": ["Attention Is All You Need"], "publisher": "arXiv",
		"author": [{"family": "Vaswani", "given": "Ashish"}], "issued": {"date-parts": [[2017]]}}`)

	for _, arg := range []string{"1706.03762v5", "arXiv:1706.03762"} {
		t.Run(arg, func(t *testing.T) {
			out, err := runWrite(t, "--json", "item", "add", arg)
			if err != nil {
				t.Fatalf("add %s: %v\n%s", arg, err, out)
			}
			res := decodeWrite(t, out)
			created := fx.fake.item(res.Target)
			if created.Data["itemType"] != "preprint" || created.Data["repository"] != "arXiv" || created.Data["archiveID"] != "arXiv:1706.03762" {
				t.Errorf("server data = %v, want a preprint with repository/archiveID", created.Data)
			}
			if created.Data["title"] != "Attention Is All You Need" {
				t.Errorf("title = %v", created.Data["title"])
			}
		})
	}
	if seen := reg.seen(); len(seen) != 2 || seen[0] != "10.48550/arXiv.1706.03762" {
		t.Errorf("registry saw %v, want the DataCite DOI without the version suffix", seen)
	}
}

func TestItemAdd_UnrecognisedInputIsUsageNamingIt(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)

	_, err := runWrite(t, "--json", "item", "add", "not-a-doi")
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeUsage || !strings.Contains(coded.Message, "not-a-doi") {
		t.Errorf("got %+v", coded)
	}
	if fx.fake.requestCount() != 0 {
		t.Error("a usage error must not reach the server")
	}
}

func TestItemAdd_NoInputIsUsage(t *testing.T) {
	newWriteFixture(t)
	_, err := runWrite(t, "--json", "item", "add")
	if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage: %v", coded.Code, err)
	}
}

// --- PDFs -----------------------------------------------------------------

func TestItemAdd_PDF_SniffsResolvesCreatesAndAttaches(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniff(t, map[string][]bib.Ref{
		"sleep.pdf": {doiRef("10.1038/nature12373"), doiRef("10.1000/cited-in-refs")},
	})

	out, err := runWrite(t, "--json", "item", "add", path)
	if err != nil {
		t.Fatalf("add pdf: %v\n%s", err, out)
	}
	res := decodeWrite(t, out)
	if res.Target != "NEW00001" {
		t.Fatalf("Target = %q", res.Target)
	}
	children := fx.fake.children("NEW00001")
	if !slices.Equal(children, []string{"NEW00002"}) {
		t.Fatalf("children = %v, want the one attachment", children)
	}
	att := fx.fake.item("NEW00002")
	if att.Data["filename"] != "sleep.pdf" || att.Data["contentType"] != "application/pdf" || att.Data["title"] != "sleep.pdf" || att.Data["linkMode"] != "imported_file" {
		t.Errorf("attachment data = %v", att.Data)
	}
	bodies := fx.fake.uploadedBodies()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "%PDF-1.4 sleep.pdf") {
		t.Errorf("S3 received %q, want the file bytes", bodies)
	}
	data := writeData(t, out)
	if data["attached"] != true || data["attachment_key"] != "NEW00002" {
		t.Errorf("data = %v, want attached:true with the attachment key", data)
	}
	candidates, _ := data["candidates"].([]any)
	if len(candidates) != 1 {
		t.Errorf("candidates = %v, want the second DOI reported, not used", data["candidates"])
	}
}

func TestItemAdd_PDF_NoAttachSkipsTheUpload(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniff(t, map[string][]bib.Ref{"sleep.pdf": {doiRef("10.1038/nature12373")}})

	out, err := runWrite(t, "--json", "item", "add", path, "--no-attach")
	if err != nil {
		t.Fatalf("add --no-attach: %v", err)
	}
	if len(fx.fake.children("NEW00001")) != 0 {
		t.Error("--no-attach must not create an attachment")
	}
	if data := writeData(t, out); data["attached"] != false {
		t.Errorf("attached = %v, want false", data["attached"])
	}
}

// TestItemAdd_PDF_AttachFailureIsReportedNotFatal: the item exists once the
// create succeeded, so a failed upload rides the result rather than failing
// the command — the caller can re-run `item attach`.
func TestItemAdd_PDF_AttachFailureIsReportedNotFatal(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniff(t, map[string][]bib.Ref{"sleep.pdf": {doiRef("10.1038/nature12373")}})
	fx.fake.failNextUpload = "storage quota exceeded"

	out, err := runWrite(t, "--json", "item", "add", path)
	if err != nil {
		t.Fatalf("a failed attach must not fail the command: %v", err)
	}
	data := writeData(t, out)
	if data["attached"] != false {
		t.Errorf("attached = %v, want false", data["attached"])
	}
	if msg, _ := data["attach_error"].(string); !strings.Contains(msg, "storage quota exceeded") {
		t.Errorf("attach_error = %q, want the upload failure", msg)
	}
	if fx.fake.item("NEW00001") == nil {
		t.Error("the item itself must exist")
	}
}

func TestItemAdd_PDF_NoToolIsNotConfiguredNamingPoppler(t *testing.T) {
	newWriteFixture(t)
	installFakeDOIOrg(t)
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniffErr(t, pdfid.ErrNoTool)

	_, err := runWrite(t, "--json", "item", "add", path)
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotConfigured || !strings.Contains(coded.Try, "poppler") {
		t.Errorf("got %+v", coded)
	}
}

func TestItemAdd_PDF_NoIdentifierPointsAtManualMode(t *testing.T) {
	newWriteFixture(t)
	installFakeDOIOrg(t)
	path := writePDF(t, t.TempDir(), "scan.pdf")
	stubSniff(t, map[string][]bib.Ref{})

	_, err := runWrite(t, "--json", "item", "add", path)
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotFound || !strings.Contains(coded.Message, "scan.pdf") {
		t.Errorf("got %+v", coded)
	}
	if !strings.Contains(coded.Try, "--title") {
		t.Errorf("Try should point at --title: %q", coded.Try)
	}
}

// TestItemAdd_PDF_DuplicatePointsAtAttach: when the PDF's paper is already
// in the library the useful action is attaching the file to it, which the
// Try names but the command never does on its own.
func TestItemAdd_PDF_DuplicatePointsAtAttach(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalItemWithDOI(t, 7, "ITEM0007", "Deep sleep", "10.1038/nature12373")
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniff(t, map[string][]bib.Ref{"sleep.pdf": {doiRef("10.1038/nature12373")}})

	_, err := runWrite(t, "--json", "item", "add", path)
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeDuplicate {
		t.Errorf("Code = %q, want duplicate", coded.Code)
	}
	if want := "sci zot --library personal item attach ITEM0007 " + shellQuote(path); !strings.Contains(coded.Try, want) {
		t.Errorf("Try = %q, want %q (scoped, path quoted)", coded.Try, want)
	}
	if len(fx.fake.requestsMatching("POST ")) != 0 {
		t.Error("nothing may be written")
	}
}

// TestItemAdd_PDF_FirstIdentifierWinsAndTheRestAreCandidates: pdfid returns
// identifiers in document order, so an arXiv stamp on page 1 precedes a
// DOI cited on page 3. item add takes the first and reports the others —
// deduplicated — as candidates, with a stderr note naming the alternative
// so a wrong pick is visible rather than silent.
func TestItemAdd_PDF_FirstIdentifierWinsAndTheRestAreCandidates(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.addRaw("10.48550/arXiv.1706.03762", `{"DOI": "10.48550/ARXIV.1706.03762", "type": "article",
		"title": "Attention Is All You Need", "publisher": "arXiv", "issued": {"date-parts": [[2017]]}}`)
	reg.add("10.1038/cited", "article-journal", "A cited paper", "Nature")
	path := writePDF(t, t.TempDir(), "attention.pdf")
	stubSniff(t, map[string][]bib.Ref{
		"attention.pdf": {arxivRef("1706.03762"), doiRef("10.1038/cited"), doiRef("10.1038/CITED")},
	})

	out, stderr, err := runWriteWithStderr(t, "--json", "item", "add", path)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if got := fx.fake.item("NEW00001").Data["itemType"]; got != "preprint" {
		t.Errorf("itemType = %v, want the arXiv stamp (first in document order) to win", got)
	}
	if seen := reg.seen(); len(seen) != 1 {
		t.Errorf("registry saw %v, want only the chosen identifier resolved", seen)
	}
	data := writeData(t, out)
	if cands := anyStrings(data["candidates"]); !slices.Equal(cands, []string{"10.1038/cited"}) {
		t.Errorf("candidates = %v, want the other identifier once (case-insensitive dedup)", data["candidates"])
	}
	if !strings.Contains(stderr, "10.1038/cited") || !strings.Contains(stderr, "--no-attach") || !strings.Contains(stderr, "item attach") {
		t.Errorf("stderr should note the alternative and the add-then-attach recipe:\n%s", stderr)
	}
}

// TestItemAdd_PDF_UploadFailureTrashesTheEmptyAttachment: a create that
// succeeded followed by an upload that failed must not leave an empty
// imported_file child on the server. The attachment is trashed, the result
// says so, and attachment_key is empty because nothing usable exists.
func TestItemAdd_PDF_UploadFailureTrashesTheEmptyAttachment(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniff(t, map[string][]bib.Ref{"sleep.pdf": {doiRef("10.1038/nature12373")}})
	fx.fake.failS3 = true

	out, err := runWrite(t, "--json", "item", "add", path)
	if err != nil {
		t.Fatalf("a failed attach must not fail the command: %v", err)
	}
	data := writeData(t, out)
	if data["attached"] != false || data["attachment_key"] != "" {
		t.Errorf("data = %v, want attached:false and an empty attachment_key", data)
	}
	if msg, _ := data["attach_error"].(string); !strings.Contains(msg, "trash") {
		t.Errorf("attach_error should say the empty attachment was trashed: %q", msg)
	}
	att := fx.fake.item("NEW00002")
	if att == nil || att.Data["deleted"] != float64(1) {
		t.Errorf("attachment = %+v, want it moved to the trash", att)
	}
	if fx.fake.item("NEW00001") == nil {
		t.Error("the item itself must exist")
	}
}

func TestItemAdd_DryRun_PDFWithSeveralIdentifiersShowsAll(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/a", "article-journal", "Paper A", "Nature")
	path := writePDF(t, t.TempDir(), "a.pdf")
	stubSniff(t, map[string][]bib.Ref{"a.pdf": {doiRef("10.1038/a"), arxivRef("1706.03762")}})

	out, err := runWrite(t, "--json", "item", "add", path, "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	var res struct {
		Items []struct {
			Identifier string   `json:"identifier"`
			Candidates []string `json:"candidates"`
		} `json:"items"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Identifier != "10.1038/a" || !slices.Equal(res.Items[0].Candidates, []string{"arXiv:1706.03762"}) {
		t.Errorf("got %+v, want the first identifier marked as the one to use and the rest listed", res.Items)
	}
	if n := len(fx.fake.requestsMatching("POST ")); n != 0 {
		t.Errorf("dry run made %d writes", n)
	}
}

// TestItemAdd_ManualOnlyFlagWithoutTitleIsUsage: in identifier mode the
// record comes from the registry, so a field flag would be silently dropped
// — refused instead, naming the mode it belongs to.
func TestItemAdd_ManualOnlyFlagWithoutTitleIsUsage(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)
	for name, args := range map[string][]string{
		"--author":      {"item", "add", "10.1038/x", "--author", "Smith, A"},
		"--field":       {"item", "add", "10.1038/x", "--field", "volume=1"},
		"--publication": {"item", "add", "10.1038/x", "--publication", "Nature"},
		"--type":        {"item", "add", "10.1038/x", "--type", "book"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runWrite(t, slices.Concat([]string{"--json"}, args)...)
			coded := codedErr(t, err)
			if coded.Code != cmdutil.CodeUsage || !strings.Contains(coded.Message, "--title") {
				t.Errorf("got %+v, want a usage error naming --title (manual mode)", coded)
			}
			if fx.fake.requestCount() != 0 {
				t.Error("a usage error must not reach the server")
			}
		})
	}
}

func TestItemAdd_Directory_PartialBatchInSortedOrder(t *testing.T) {
	fx := newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/a", "article-journal", "Paper A", "Nature")
	reg.add("10.1038/c", "article-journal", "Paper C", "Nature")
	dir := t.TempDir()
	writePDF(t, dir, "c.pdf")
	writePDF(t, dir, "a.pdf")
	writePDF(t, dir, "b.pdf")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubSniff(t, map[string][]bib.Ref{
		"a.pdf": {doiRef("10.1038/a")},
		"c.pdf": {doiRef("10.1038/c")},
		// b.pdf: no identifier
	})

	out, err := runWrite(t, "--json", "item", "add", dir)
	if err != nil {
		t.Fatalf("add dir: %v\n%s", err, out)
	}
	res := decodeBulk(t, out)
	if res.Action != "created" || res.Kind != "item" || res.Total != 3 {
		t.Errorf("got %+v", res)
	}
	if !slices.Equal(res.Success, []string{"NEW00001", "NEW00003"}) {
		t.Errorf("Success = %v", res.Success)
	}
	bPath := filepath.Join(dir, "b.pdf")
	if msg, ok := res.Failed[bPath]; !ok || !strings.Contains(msg, "no DOI or arXiv id") {
		t.Errorf("Failed = %v, want b.pdf reported", res.Failed)
	}
	data, _ := res.Data.(map[string]any)
	rows, _ := data["rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("rows = %v, want one per PDF", data["rows"])
	}
	first, _ := rows[0].(map[string]any)
	if first["input"] != filepath.Join(dir, "a.pdf") || first["key"] != "NEW00001" || first["title"] != "Paper A" || first["attached"] != true {
		t.Errorf("rows[0] = %v", first)
	}
	second, _ := rows[1].(map[string]any)
	if second["input"] != bPath || second["error"] == "" {
		t.Errorf("rows[1] = %v, want the failure in input order", second)
	}
	if len(fx.fake.uploadedBodies()) != 2 {
		t.Errorf("S3 saw %d uploads, want 2", len(fx.fake.uploadedBodies()))
	}
}

func TestItemAdd_Batch_HumanOutputIsOneLinePerInput(t *testing.T) {
	newWriteFixture(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/a", "article-journal", "Paper A", "Nature")

	out, err := runWrite(t, "item", "add", "10.1038/a", "10.9999/nothing")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	got := stripANSI(string(out))
	if !strings.Contains(got, "Paper A (NEW00001)") {
		t.Errorf("want a ✓ line naming title and key:\n%s", got)
	}
	if !strings.Contains(got, "10.9999/nothing: ") {
		t.Errorf("want a ✗ line naming the failed input:\n%s", got)
	}
}

// --- dry run --------------------------------------------------------------

func TestItemAdd_DryRun_ResolvesButWritesNothing(t *testing.T) {
	fx := newWriteFixture(t)
	fx.seedLocalCollections(t)
	reg := installFakeDOIOrg(t)
	reg.add("10.1038/nature12373", "article-journal", "Deep sleep", "Nature")
	path := writePDF(t, t.TempDir(), "sleep.pdf")
	stubSniff(t, map[string][]bib.Ref{"sleep.pdf": {doiRef("10.1038/nature12373")}})

	out, err := runWrite(t, "--json", "item", "add", path, "--collection", "Projects", "--dry-run")
	if err != nil {
		t.Fatalf("add --dry-run: %v\n%s", err, out)
	}
	if n := len(fx.fake.requestsMatching("POST ")); n != 0 {
		t.Errorf("dry run made %d writes: %v", n, fx.fake.requestsMatching("POST "))
	}
	if len(reg.seen()) != 1 {
		t.Errorf("dry run should still resolve the DOI, registry saw %v", reg.seen())
	}
	var res struct {
		DryRun bool `json:"dry_run"`
		Items  []struct {
			Input       string `json:"input"`
			ItemType    string `json:"item_type"`
			Title       string `json:"title"`
			FirstAuthor string `json:"first_author"`
			Year        int    `json:"year"`
			DOI         string `json:"doi"`
			Collection  string `json:"collection"`
			Attach      bool   `json:"attach"`
		} `json:"items"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if !res.DryRun || len(res.Items) != 1 {
		t.Fatalf("got %+v", res)
	}
	it := res.Items[0]
	if it.ItemType != "journalArticle" || it.Title != "Deep sleep" || it.FirstAuthor != "Smith" || it.Year != 2013 ||
		it.DOI != "10.1038/nature12373" || it.Collection != "COLL0002" || !it.Attach {
		t.Errorf("row = %+v", it)
	}
}

// --- manual mode ----------------------------------------------------------

func TestItemAdd_Manual_BuildsTheItemFromFlags(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)

	out, err := runWrite(t, "--json", "item", "add", "--title", "A hand-made record",
		"--author", "Smith, Alice", "--author", "NASA", "--creator", "editor:Doe, Jane",
		"--publication", "Nature", "--date", "2020", "--field", "volume=12", "--extra", "Line: one, two",
		"--tag", "manual")
	if err != nil {
		t.Fatalf("add --title: %v\n%s", err, out)
	}
	created := fx.fake.item("NEW00001")
	if created == nil {
		t.Fatal("no item on the server")
	}
	want := map[string]any{
		"itemType": "journalArticle", "title": "A hand-made record", "publicationTitle": "Nature",
		"date": "2020", "volume": "12", "extra": "Line: one, two",
	}
	for k, v := range want {
		if created.Data[k] != v {
			t.Errorf("server data[%s] = %v, want %v", k, created.Data[k], v)
		}
	}
	creators, _ := created.Data["creators"].([]any)
	if len(creators) != 3 {
		t.Fatalf("creators = %v, want author, institutional author, editor", created.Data["creators"])
	}
	alice, _ := creators[0].(map[string]any)
	if alice["creatorType"] != "author" || alice["lastName"] != "Smith" || alice["firstName"] != "Alice" {
		t.Errorf("creators[0] = %v, want Smith, Alice as ONE author", alice)
	}
	nasa, _ := creators[1].(map[string]any)
	if nasa["name"] != "NASA" || nasa["lastName"] != nil {
		t.Errorf("creators[1] = %v, want a single-field institutional name", nasa)
	}
	editor, _ := creators[2].(map[string]any)
	if editor["creatorType"] != "editor" || editor["lastName"] != "Doe" {
		t.Errorf("creators[2] = %v", editor)
	}
}

func TestItemAdd_Manual_UsageErrorsNeverReachTheServer(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)
	cases := map[string][]string{
		"unknown field for the type": {"--title", "T", "--field", "bogus=1"},
		"field owned by a flag":      {"--title", "T", "--field", "title=other"},
		"field without =":            {"--title", "T", "--field", "volume"},
		"unknown creator type":       {"--title", "T", "--creator", "dj:Smith, A"},
		"creator without a type":     {"--title", "T", "--creator", "Smith, A"},
		"unknown item type":          {"--title", "T", "--type", "mixtape"},
		"venue on a type without":    {"--title", "T", "--type", "book", "--publication", "Nature"},
		"identifier with --title":    {"--title", "T", "10.1038/x"},
		"two PDFs with --title":      {"--title", "T", "a.pdf", "b.pdf"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runWrite(t, slices.Concat([]string{"--json", "item", "add"}, args)...)
			if coded := codedErr(t, err); coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want usage: %v", coded.Code, err)
			}
			if n := len(fx.fake.requestsMatching("POST ")); n != 0 {
				t.Errorf("a usage error must not write, saw %d POSTs", n)
			}
		})
	}
}

func TestItemAdd_Manual_PublicationRoutesToTheTypesVenueField(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)

	out, err := runWrite(t, "--json", "item", "add", "--title", "Talk", "--type", "conferencePaper", "--publication", "NeurIPS")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if got := fx.fake.item("NEW00001").Data["proceedingsTitle"]; got != "NeurIPS" {
		t.Errorf("proceedingsTitle = %v, want NeurIPS", got)
	}
}

func TestItemAdd_Manual_NoAttachSkipsTheUpload(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)
	path := writePDF(t, t.TempDir(), "scan.pdf")

	out, err := runWrite(t, "--json", "item", "add", "--title", "A scan", path, "--no-attach")
	if err != nil {
		t.Fatalf("add --title --no-attach: %v", err)
	}
	if n := len(fx.fake.children("NEW00001")); n != 0 {
		t.Errorf("--no-attach must not create an attachment, got %d", n)
	}
	if data := writeData(t, out); data["attached"] != false {
		t.Errorf("attached = %v", data["attached"])
	}
}

// TestItemAdd_Manual_DryRunWritesNothing: manual mode honours --dry-run
// too, and an institutional first author renders by its name.
func TestItemAdd_Manual_DryRunWritesNothing(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)

	out, err := runWrite(t, "--json", "item", "add", "--title", "Report", "--author", "NASA", "--type", "document", "--dry-run")
	if err != nil {
		t.Fatalf("manual dry run: %v\n%s", err, out)
	}
	if n := len(fx.fake.requestsMatching("POST ")); n != 0 {
		t.Errorf("dry run made %d writes", n)
	}
	var res struct {
		DryRun bool `json:"dry_run"`
		Items  []struct {
			ItemType    string `json:"item_type"`
			FirstAuthor string `json:"first_author"`
		} `json:"items"`
	}
	if err := json.Unmarshal(unwrapData(t, out), &res); err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Items) != 1 || res.Items[0].ItemType != "document" || res.Items[0].FirstAuthor != "NASA" {
		t.Errorf("got %+v", res)
	}
}

func TestItemAdd_Manual_WithPDFAttaches(t *testing.T) {
	fx := newWriteFixture(t)
	installFakeDOIOrg(t)
	path := writePDF(t, t.TempDir(), "scan.pdf")
	stubSniffErr(t, errors.New("sniff must not run in manual mode"))

	out, err := runWrite(t, "--json", "item", "add", "--title", "A scan", path)
	if err != nil {
		t.Fatalf("add --title pdf: %v\n%s", err, out)
	}
	if children := fx.fake.children("NEW00001"); !slices.Equal(children, []string{"NEW00002"}) {
		t.Errorf("children = %v, want the attachment", children)
	}
	if data := writeData(t, out); data["attached"] != true {
		t.Errorf("attached = %v", data["attached"])
	}
}
