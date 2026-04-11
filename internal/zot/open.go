package zot

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/sciminds/cli/internal/zot/local"
)

// PickAttachment returns the first imported-file attachment from an item
// (usually a PDF). Returns nil if none exist.
func PickAttachment(it *local.Item) *local.Attachment {
	for i := range it.Attachments {
		a := &it.Attachments[i]
		if a.Filename != "" {
			return a
		}
	}
	return nil
}

// AttachmentPath resolves an attachment's filesystem path inside the Zotero
// storage directory. Zotero stores files under `<dataDir>/storage/<itemKey>/<filename>`.
func AttachmentPath(dataDir string, a *local.Attachment) string {
	return filepath.Join(dataDir, "storage", a.Key, a.Filename)
}

// LaunchFile opens the given file path in the platform's default viewer.
// macOS: `open`, Linux: `xdg-open`, Windows: `rundll32`.
func LaunchFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
