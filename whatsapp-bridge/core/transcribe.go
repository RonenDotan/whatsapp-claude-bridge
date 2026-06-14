package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TranscribeAudio runs whisper on the given audio file and returns the transcript.
// Cleans up both the input file and the generated .txt after reading.
func TranscribeAudio(filePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tmpDir := os.TempDir()
	cmd := exec.CommandContext(ctx, "whisper", filePath,
		"--model", "base",
		"--output_format", "txt",
		"--output_dir", tmpDir,
	)
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1")

	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("whisper failed: %w\noutput: %s", err, string(out))
	}

	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	txtPath := filepath.Join(tmpDir, base+".txt")
	defer os.Remove(filePath)
	defer os.Remove(txtPath)

	data, err := os.ReadFile(txtPath)
	if err != nil {
		return "", fmt.Errorf("failed to read transcript: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}
