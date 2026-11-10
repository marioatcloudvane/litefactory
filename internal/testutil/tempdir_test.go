package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortTempRootIsShortAndWritable(t *testing.T) {
	root := ShortTempRoot()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) > shortTempRootLimit {
		t.Fatalf("root %q is too long for a Unix socket fixture", resolved)
	}
	dir, err := os.MkdirTemp(root, "lf-short-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
}

func TestShortTempRootSkipsLongAndUnwritableRoots(t *testing.T) {
	writable, err := os.MkdirTemp(ShortTempRoot(), "lf-root-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(writable) })
	readOnly := filepath.Join(writable, "ro")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	long := filepath.Join(writable, strings.Repeat("x", 128))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(writable)
	if err != nil {
		t.Fatal(err)
	}
	limit := len(resolved) + len("/ro")
	if got := shortTempRoot(limit, long, readOnly, writable, "/last"); got != writable {
		t.Fatalf("root = %q, want %q", got, writable)
	}
	if got := shortTempRoot(limit, long, readOnly); got != readOnly {
		t.Fatalf("fallback root = %q, want the last root %q", got, readOnly)
	}
}
