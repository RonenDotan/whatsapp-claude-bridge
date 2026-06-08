package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OutputTracker detects files created or modified during a single LLM turn.
// Call Snapshot() once before the LLM runs (records timestamp, returns nil).
// Call Snapshot() again after the LLM finishes (returns paths of new/changed output files).
type OutputTracker interface {
	Snapshot() ([]string, error)
}

// outputFileExts is the whitelist of extensions considered deliverable output.
// Internal bridge files (.md, .json, .ogg input audio, .db, .log) are excluded.
var outputFileExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
	".gif":  true,
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

// SnapshotTracker implements OutputTracker using a timestamp comparison.
// First Snapshot() call records time.Now(). Second call returns all files
// in dir whose mtime is after the recorded time and whose extension is
// in outputFileExts.
type SnapshotTracker struct {
	dir          string
	snapshotTime time.Time
	taken        bool
}

// NewSnapshotTracker creates a tracker for the given directory.
func NewSnapshotTracker(dir string) *SnapshotTracker {
	return &SnapshotTracker{dir: dir}
}

// Snapshot records the current time on the first call and returns nil.
// On the second call it reads the directory and returns absolute paths
// of files that were created or modified since the first call.
func (t *SnapshotTracker) Snapshot() ([]string, error) {
	if !t.taken {
		t.snapshotTime = time.Now()
		t.taken = true
		return nil, nil
	}

	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !outputFileExts[ext] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(t.snapshotTime) {
			result = append(result, filepath.Join(t.dir, e.Name()))
		}
	}
	return result, nil
}
