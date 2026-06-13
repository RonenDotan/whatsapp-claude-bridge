package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OutputTracker detects files created or modified during a single LLM turn.
type OutputTracker interface {
	Snapshot() ([]string, error)
}

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

type SnapshotTracker struct {
	dir          string
	snapshotTime time.Time
	taken        bool
}

func NewSnapshotTracker(dir string) *SnapshotTracker {
	return &SnapshotTracker{dir: dir}
}

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
