// Package extract names what a docling extraction leaves behind in a
// Zotero library, and fingerprints the PDFs it was made from.
//
// Running the extraction is not sci's job. That is a credentialed write —
// docling produces the text and the result is posted back as a child note
// through the Zotero Web API — so it lives in the sibling zot binary
// (`zot extract-lib`) along with the rest of the operate plane. What sci
// keeps is the read side: the two tags that mark an extraction
// ([DoclingTag], [MarkdownTag]), which `link suggest` uses to refuse a
// paper's own bibliography, and [HashPDF] / [ContentKey], which
// `doctor duplicates` uses to notice two library items holding the same
// PDF bytes.
package extract
