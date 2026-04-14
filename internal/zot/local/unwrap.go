package local

import "strings"

// zoteroNotePrefix is the div wrapper Zotero adds to all note bodies.
const zoteroNotePrefix = `<div class="zotero-note znv1">`

// UnwrapZoteroDiv strips the outer <div class="zotero-note znv1">…</div>
// wrapper that Zotero adds to every note body. Returns the inner content
// unchanged. If the wrapper is absent the body is returned as-is.
func UnwrapZoteroDiv(body string) string {
	s := strings.TrimSpace(body)
	if !strings.HasPrefix(s, zoteroNotePrefix) {
		return body
	}
	s = s[len(zoteroNotePrefix):]
	return strings.TrimSuffix(s, "</div>")
}
