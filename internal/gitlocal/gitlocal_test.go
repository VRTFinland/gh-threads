package gitlocal

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFileLinesPreservesBlankLines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp repo: %v", err)
	}

	runGit := func(args ...string) {
		cmd := exec.CommandContext(ctx, "git", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, stderr.String())
		}
	}

	runGit("init", "-q")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.com")

	content := "\nfirst\nsecond\n"
	path := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}

	runGit("add", "sample.txt")
	runGit("commit", "-qm", "add sample")

	repo := &Repo{available: true}
	lines, err := repo.FileLines(ctx, "HEAD", "sample.txt")
	if err != nil {
		t.Fatalf("FileLines returned error: %v", err)
	}

	if len(lines) != 4 {
		t.Fatalf("expected 4 lines including leading/trailing blanks, got %d: %#v", len(lines), lines)
	}
	if lines[0] != "" || lines[3] != "" {
		t.Fatalf("expected blank lines to be preserved, got %#v", lines)
	}
}
