package team

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeOverrideSource writes content at root/relPath, creating any parent
// directories, so tests can point SetOverride at a real markdown file inside
// the target repo.
func writeOverrideSource(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, content, 0o666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestSetOverride_ThenActiveOverrideReportsIt(t *testing.T) {
	root := t.TempDir()
	writeOverrideSource(t, root, "local/reviewer.md", []byte("---\nname: reviewer\n---\nBody.\n"))

	if err := SetOverride(root, "reviewer", "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride err = %v, want nil", err)
	}

	path, ok, err := ActiveOverride(root, "reviewer")
	if err != nil {
		t.Fatalf("ActiveOverride err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("ActiveOverride ok = false, want true")
	}
	if path != "local/reviewer.md" {
		t.Errorf("ActiveOverride path = %q, want %q", path, "local/reviewer.md")
	}
}

func TestSetOverride_ReplacesExistingOverrideForSameKey(t *testing.T) {
	root := t.TempDir()
	writeOverrideSource(t, root, "local/first.md", []byte("first\n"))
	writeOverrideSource(t, root, "local/second.md", []byte("second\n"))

	if err := SetOverride(root, "reviewer", "local/first.md"); err != nil {
		t.Fatalf("SetOverride (first) err = %v, want nil", err)
	}
	if err := SetOverride(root, "reviewer", "local/second.md"); err != nil {
		t.Fatalf("SetOverride (second) err = %v, want nil", err)
	}

	path, ok, err := ActiveOverride(root, "reviewer")
	if err != nil {
		t.Fatalf("ActiveOverride err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("ActiveOverride ok = false, want true")
	}
	if path != "local/second.md" {
		t.Errorf("ActiveOverride path = %q, want %q (second call replaces the first)", path, "local/second.md")
	}
}

func TestUnsetOverride_RemovesIt(t *testing.T) {
	root := t.TempDir()
	writeOverrideSource(t, root, "local/reviewer.md", []byte("Body.\n"))
	if err := SetOverride(root, "reviewer", "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride err = %v, want nil", err)
	}

	if err := UnsetOverride(root, "reviewer"); err != nil {
		t.Fatalf("UnsetOverride err = %v, want nil", err)
	}

	_, ok, err := ActiveOverride(root, "reviewer")
	if err != nil {
		t.Fatalf("ActiveOverride err = %v, want nil", err)
	}
	if ok {
		t.Errorf("ActiveOverride ok = true after UnsetOverride, want false")
	}
}

func TestUnsetOverride_NoActiveOverride_IsIdempotent(t *testing.T) {
	root := t.TempDir()

	if err := UnsetOverride(root, "reviewer"); err != nil {
		t.Fatalf("UnsetOverride (no override present) err = %v, want nil", err)
	}
}

func TestActiveOverride_NoOverridesFileAtAll_ReportsNotActive(t *testing.T) {
	root := t.TempDir()

	_, ok, err := ActiveOverride(root, "reviewer")
	if err != nil {
		t.Fatalf("ActiveOverride err = %v, want nil", err)
	}
	if ok {
		t.Errorf("ActiveOverride ok = true with no overrides file, want false")
	}
}

func TestReadOverrideSource_ReturnsFileBytes(t *testing.T) {
	root := t.TempDir()
	content := []byte("---\nname: reviewer\n---\nOverridden body.\n")
	writeOverrideSource(t, root, "local/reviewer.md", content)
	if err := SetOverride(root, "reviewer", "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride err = %v, want nil", err)
	}

	raw, path, ok, err := ReadOverrideSource(root, "reviewer")
	if err != nil {
		t.Fatalf("ReadOverrideSource err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("ReadOverrideSource ok = false, want true")
	}
	if path != "local/reviewer.md" {
		t.Errorf("ReadOverrideSource path = %q, want %q", path, "local/reviewer.md")
	}
	if string(raw) != string(content) {
		t.Errorf("ReadOverrideSource raw = %q, want %q", raw, content)
	}
}

func TestReadOverrideSource_NoOverride_ReportsNotOK(t *testing.T) {
	root := t.TempDir()

	raw, path, ok, err := ReadOverrideSource(root, "reviewer")
	if err != nil {
		t.Fatalf("ReadOverrideSource err = %v, want nil", err)
	}
	if ok {
		t.Errorf("ReadOverrideSource ok = true, want false")
	}
	if raw != nil || path != "" {
		t.Errorf("ReadOverrideSource = (%v, %q), want (nil, \"\")", raw, path)
	}
}

func TestSetOverride_InvalidAgentKey_ReturnsKeyErrorAndRecordsNoOverride(t *testing.T) {
	root := t.TempDir()
	writeOverrideSource(t, root, "local/reviewer.md", []byte("Body.\n"))

	err := SetOverride(root, "Not_A_Valid_Key!", "local/reviewer.md")
	if err == nil {
		t.Fatalf("SetOverride err = nil, want *KeyError")
	}
	var keyErr *KeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("SetOverride err = %v, want *KeyError", err)
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("errors.Is(err, ErrInvalidKey) = false, want true")
	}

	if _, err := os.Stat(overridesPath(root)); !os.IsNotExist(err) {
		t.Errorf("overrides file exists after a rejected SetOverride, want no file written")
	}
}

// TestSetOverride_PathFailures_RecordsNoOverride covers AC-4: a set naming a
// local path that doesn't exist, isn't a markdown file, or otherwise fails
// validateOverrideSource's checks fails with an *OverridePathError and
// records no override at all.
func TestSetOverride_PathFailures_RecordsNoOverride(t *testing.T) {
	root := t.TempDir()
	writeOverrideSource(t, root, "local/not-markdown.txt", []byte("Body.\n"))

	tests := []struct {
		name     string
		path     string
		wantSkip bool // true when the file must not be pre-created
	}{
		{name: "does not exist", path: "local/missing.md"},
		{name: "not a markdown file", path: "local/not-markdown.txt"},
		{name: "empty path", path: ""},
		{name: "absolute path", path: "/etc/passwd.md"},
		{name: "escapes repo root", path: "../outside.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetOverride(root, "reviewer", tt.path)
			if err == nil {
				t.Fatalf("SetOverride(%q) err = nil, want *OverridePathError", tt.path)
			}
			var pathErr *OverridePathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("SetOverride(%q) err = %v, want *OverridePathError", tt.path, err)
			}
			if !errors.Is(err, ErrOverridePath) {
				t.Errorf("errors.Is(err, ErrOverridePath) = false, want true")
			}

			_, ok, activeErr := ActiveOverride(root, "reviewer")
			if activeErr != nil {
				t.Fatalf("ActiveOverride err = %v, want nil", activeErr)
			}
			if ok {
				t.Errorf("ActiveOverride ok = true after a rejected SetOverride(%q), want false (no override recorded)", tt.path)
			}
		})
	}
}

func TestSetOverride_RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writeOverrideSource(t, root, "real/reviewer.md", []byte("Body.\n"))
	linkPath := filepath.Join(root, "local", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "reviewer.md"), linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	err := SetOverride(root, "reviewer", "local/reviewer.md")
	if err == nil {
		t.Fatalf("SetOverride err = nil, want error refusing the symlink")
	}
	if !errors.Is(err, ErrRefusedSymlink) {
		t.Errorf("errors.Is(err, ErrRefusedSymlink) = false, want true; err = %v", err)
	}

	_, ok, activeErr := ActiveOverride(root, "reviewer")
	if activeErr != nil {
		t.Fatalf("ActiveOverride err = %v, want nil", activeErr)
	}
	if ok {
		t.Errorf("ActiveOverride ok = true after a symlink-refused SetOverride, want false")
	}
}

func TestUnsetOverride_InvalidAgentKey_ReturnsKeyError(t *testing.T) {
	root := t.TempDir()

	err := UnsetOverride(root, "Not_Valid!")
	if err == nil {
		t.Fatalf("UnsetOverride err = nil, want *KeyError")
	}
	var keyErr *KeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("UnsetOverride err = %v, want *KeyError", err)
	}
}

func TestDecodeOverrides_MalformedKey_AndBadValue_AggregatesBothErrors(t *testing.T) {
	// Two distinct faults in one document: a key that fails KeyPattern, and a
	// value that is not a non-empty JSON string. decodeOverrides must report
	// both rather than stopping at the first (joinSorted), matching this
	// package's aggregate-then-report policy used throughout (never fail
	// fast on the first error in a loop or in validation).
	data := []byte(`{"Not_Valid!": "local/a.md", "reviewer": ""}`)

	_, err := decodeOverrides(data)
	if err == nil {
		t.Fatalf("decodeOverrides err = nil, want aggregated error")
	}
	var keyErr *KeyError
	if !errors.As(err, &keyErr) {
		t.Errorf("decodeOverrides err = %v, want to contain *KeyError", err)
	}
	var emptyErr *EmptyFieldError
	if !errors.As(err, &emptyErr) {
		t.Errorf("decodeOverrides err = %v, want to contain *EmptyFieldError", err)
	}
}

func TestLoadOverrides_MissingFile_ReturnsEmptyMapNoError(t *testing.T) {
	root := t.TempDir()

	overrides, err := loadOverrides(root)
	if err != nil {
		t.Fatalf("loadOverrides err = %v, want nil", err)
	}
	if len(overrides) != 0 {
		t.Errorf("loadOverrides = %v, want empty map", overrides)
	}
}
