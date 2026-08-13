// Package extract fingerprints the PDFs a docling extraction was made from.
//
// Running the extraction is not sci's job. That is a credentialed write —
// docling produces the text and the result is posted back as a child note
// through the Zotero Web API — so it lives in the sibling zot binary
// (`zot extract-lib`) along with the rest of the operate plane. What sci
// keeps is [HashPDF] / [ContentKey], which `doctor duplicates` uses to
// notice two library items holding the same PDF bytes.
//
// The two tags an extraction leaves in the library used to be named here
// too. They left with `link suggest`, the last verb that asked whether a
// note was one: the `docling` tag now appears only where it is queried,
// inside pkg/local's SQL, and the parent-side tag is pkg/local's own
// [local.HasMarkdownTag]. One name per fact.
package extract
