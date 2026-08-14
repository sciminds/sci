// Package extract fingerprints the PDFs a docling extraction was made from.
//
// Producing an extraction is out of scope here: docling produces the text and
// the result is posted back into the library as a child note, which is a
// write. What this package does is [HashPDF] / [ContentKey], which
// `sci zot doctor duplicates` uses to notice two library items holding the
// same PDF bytes.
//
// The tags an extraction leaves in the library are deliberately NOT named
// here: the `docling` tag appears only where it is queried, inside
// pkg/local's SQL, and the parent-side tag is pkg/local's own
// [local.HasMarkdownTag]. One name per fact.
package extract
