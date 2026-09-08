package team

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRepoFileContainment(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.md"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "outside.md"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, filepath.Join(outside, "outside.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{rel, filepath.Join(outside, "outside.md"), "link/outside.md"} {
		if _, err := readRepoFile(root, name); err == nil {
			t.Errorf("readRepoFile accepted escaping path %q", name)
		}
	}
	data, err := readRepoFile(root, "inside.md")
	if err != nil || string(data) != "inside" {
		t.Fatalf("read valid repository file = %q, %v", data, err)
	}
}
