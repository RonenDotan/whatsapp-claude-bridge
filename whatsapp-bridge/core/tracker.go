package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

var outputFileExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
	".gif":  true,
	".mp3":  true,
	".wav":  true,
	".mp4":  true,
	".pdf":  true,
	".docx": true,
	".xlsx": true,
	".pptx": true,
	".zip":  true,
	".html": true,
	".svg":  true,
	".csv":  true,
}

// SnapshotTracker detects files created in a chat directory during a single LLM turn.
// Usage: call Before() just before the LLM call, then After() to get new files.
type SnapshotTracker struct {
	dir  string
	mark time.Time
}

func NewSnapshotTracker(dir string) *SnapshotTracker {
	return &SnapshotTracker{dir: dir}
}

// Before records the current time as the baseline for the next After() call.
func (t *SnapshotTracker) Before() {
	t.mark = time.Now()
}

// After returns paths of files in the tracked directory that were created or
// modified after the most recent Before() call. Returns nil if Before() was
// never called or the directory cannot be read.
func (t *SnapshotTracker) After() []string {
	if t.mark.IsZero() {
		return nil
	}
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !outputFileExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(t.mark) {
			result = append(result, filepath.Join(t.dir, e.Name()))
		}
	}
	return result
}
