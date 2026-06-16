package signal

import (
	"os"
	"path/filepath"
)

// resolveSignalAttachmentPath returns the absolute path of a signal attachment on disk,
// or "" if the file cannot be located.
func resolveSignalAttachmentPath(a signalAttachment) string {
	switch {
	case filepath.IsAbs(a.Filename):
		if _, err := os.Stat(a.Filename); err == nil {
			return a.Filename
		}
	case a.Filename != "":
		p := filepath.Join(signalAttachmentsDir, a.Filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	case a.ID != "":
		base := filepath.Join(signalAttachmentsDir, a.ID)
		if _, err := os.Stat(base); err == nil {
			return base
		}
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".txt"} {
			candidate := base + ext
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}
