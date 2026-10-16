package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceSymlinkReplacementDuringObservation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target"), []byte("same bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := root.Lstat("link")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the original inode alive so the replacement cannot reuse it.
	if err := os.Rename(filepath.Join(dir, "link"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := observeSourceSymlink(root, "link", before); err == nil || !strings.Contains(err.Error(), "changed during observation") {
		t.Fatalf("replacement certified: %v", err)
	}
}
