package extract

// DoclingTag is applied to every child note created by the extraction
// pipeline. Searching for this tag finds every extraction in the library.
const DoclingTag = "docling"

// MarkdownTag is applied to the parent item whenever it has at least
// one DoclingTag-tagged child note. Lets users build a Zotero saved
// search like `attachmentFileType:is:PDF + tag:doesNotInclude:has-markdown`
// to surface items still missing an extraction.
const MarkdownTag = "has-markdown"
