package instructionpackage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeOwnerFixture(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerFallbacks(t *testing.T) {
	cases := []struct {
		name                          string
		catalogue                     []byte
		role                          []byte
		missingCatalogue, missingRole bool
		file, category                string
	}{
		{name: "missing catalogue", missingCatalogue: true, role: []byte("Owner instructions"), file: CataloguePath, category: "missing"},
		{name: "malformed catalogue", catalogue: []byte("["), role: []byte("Owner instructions"), file: CataloguePath, category: "invalid"},
		{name: "added role", catalogue: []byte("[fixture-role, extra-role]"), role: []byte("Owner instructions"), file: CataloguePath, category: "catalogue-mismatch"},
		{name: "omitted role", catalogue: []byte("[]"), role: []byte("Owner instructions"), file: CataloguePath, category: "catalogue-mismatch"},
		{name: "missing role", catalogue: []byte("[fixture-role]"), missingRole: true, file: "agents/fixture-role.md", category: "missing"},
		{name: "blank role", catalogue: []byte("[fixture-role]"), role: []byte(" \n"), file: "agents/fixture-role.md", category: "invalid"},
		{name: "non UTF8 role", catalogue: []byte("[fixture-role]"), role: []byte{0xff}, file: "agents/fixture-role.md", category: "invalid"},
		{name: "oversize role", catalogue: []byte("[fixture-role]"), role: bytes.Repeat([]byte("x"), MaxFileBytes+1), file: "agents/fixture-role.md", category: "bounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tc.missingCatalogue {
				writeOwnerFixture(t, dir, CataloguePath, tc.catalogue)
			}
			if !tc.missingRole {
				writeOwnerFixture(t, dir, "agents/fixture-role.md", tc.role)
			}
			baseline := baselineFixture()
			pkg, err := Load(t.Context(), baseline, dir)
			if err != nil {
				t.Fatal(err)
			}
			capture, diags, err := pkg.Capture(t.Context(), "fixture-role")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(capture.Instructions(), baseline["agents/fixture-role.md"].Data) {
				t.Fatal("fallback selected incomplete shipped role")
			}
			if len(diags) != 1 || diags[0].Role != "fixture-role" || diags[0].File != tc.file || diags[0].Category != tc.category || diags[0].SelectedSource != SourceShipped {
				t.Fatalf("unexpected diagnostics: %+v", diags)
			}
			for _, f := range capture.Snapshot().Files {
				if f.Path == tc.file && f.Source != SourceShipped {
					t.Fatal("failed file was marked as owner")
				}
				if tc.file == CataloguePath && f.Source != SourceShipped {
					t.Fatal("invalid catalogue partially selected owner content")
				}
			}
			if !tc.missingRole {
				raw, err := os.ReadFile(filepath.Join(dir, "agents", "fixture-role.md"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(raw, tc.role) {
					t.Fatal("fallback modified owner role")
				}
			}
		})
	}
}

func TestMissingOwnerDirectoryIsCleanShippedSelection(t *testing.T) {
	pkg, err := Load(t.Context(), baselineFixture(), filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	capture, diags, err := pkg.Capture(t.Context(), "fixture-role")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("absent owner directory reported fallback: %+v", diags)
	}
	for _, f := range capture.Snapshot().Files {
		if f.Source != SourceShipped {
			t.Fatal("absent owner selected owner bytes")
		}
	}
}

func TestOwnerUpdatesAndRemovedFilesRemainUntouched(t *testing.T) {
	dir := t.TempDir()
	catalogue := []byte("# owner\n[fixture-role]\n")
	role := []byte("Owner instructions\n")
	removed := []byte("Retained inactive role\n")
	writeOwnerFixture(t, dir, CataloguePath, catalogue)
	writeOwnerFixture(t, dir, "agents/fixture-role.md", role)
	writeOwnerFixture(t, dir, "agents/removed-role.md", removed)
	baseline := baselineFixture()
	for _, version := range []string{"v1", "v2"} {
		baseline[MetadataPath].Data = []byte("schema: 1\nversion: " + version + "\n")
		baseline["agents/fixture-role.md"].Data = []byte("Shipped " + version)
		pkg, err := Load(t.Context(), baseline, dir)
		if err != nil {
			t.Fatal(err)
		}
		capture, diags, err := pkg.Capture(t.Context(), "fixture-role")
		if err != nil {
			t.Fatal(err)
		}
		if len(diags) != 0 || !bytes.Equal(capture.Instructions(), role) {
			t.Fatal("update displaced owner instructions")
		}
		if _, _, err := pkg.Capture(t.Context(), "removed-role"); err == nil {
			t.Fatal("inactive owner role became callable")
		}
	}
	for name, want := range map[string][]byte{CataloguePath: catalogue, "agents/fixture-role.md": role, "agents/removed-role.md": removed} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("update changed owner %s", name)
		}
	}
}

func TestOwnerRejectsSymlinksAndNonregularFiles(t *testing.T) {
	for _, kind := range []string{"root", "intermediate", "final", "directory-file"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			writeOwnerFixture(t, dir, CataloguePath, []byte("[fixture-role]"))
			writeOwnerFixture(t, dir, "agents/fixture-role.md", []byte("Owner role"))
			outside := t.TempDir()
			writeOwnerFixture(t, outside, CataloguePath, []byte("[fixture-role]"))
			writeOwnerFixture(t, outside, "agents/fixture-role.md", []byte("Outside role"))
			switch kind {
			case "root":
				link := filepath.Join(t.TempDir(), "owner")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link
			case "intermediate":
				if err := os.RemoveAll(filepath.Join(dir, "agents")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "agents"), filepath.Join(dir, "agents")); err != nil {
					t.Fatal(err)
				}
			case "final":
				if err := os.Remove(filepath.Join(dir, "agents", "fixture-role.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "agents", "fixture-role.md"), filepath.Join(dir, "agents", "fixture-role.md")); err != nil {
					t.Fatal(err)
				}
			case "directory-file":
				if err := os.Remove(filepath.Join(dir, "agents", "fixture-role.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, "agents", "fixture-role.md"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			baseline := baselineFixture()
			pkg, err := Load(t.Context(), baseline, dir)
			if err != nil {
				t.Fatal(err)
			}
			capture, diags, err := pkg.Capture(t.Context(), "fixture-role")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(capture.Instructions(), baseline["agents/fixture-role.md"].Data) || len(diags) != 1 || diags[0].Category != "unsafe" {
				t.Fatalf("unsafe owner did not safely fall back: %+v", diags)
			}
		})
	}
}

func TestCaptureCancellationAndUnknownRole(t *testing.T) {
	pkg, err := Load(t.Context(), baselineFixture(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := pkg.Capture(ctx, "fixture-role"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture = %v", err)
	}
	if _, _, err := pkg.Capture(t.Context(), "../fixture-role"); err == nil {
		t.Fatal("unknown unsafe role accepted")
	}
}

func TestOwnerReaderDetectsRootReplacement(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "owner")
	writeOwnerFixture(t, dir, CataloguePath, []byte("[fixture-role]"))
	reader, err := openOwner(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.root.Close()
	if err := os.Rename(dir, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.read(t.Context(), CataloguePath); failureCategory(err) != "changed" {
		t.Fatalf("replaced root read = %v", err)
	}
}

// The read hook runs at the context check immediately before the descriptor read.
// It replaces the pathname after the original descriptor has already been opened.
type ownerReadMutationContext struct {
	context.Context
	checks int
	mutate func()
}

func (c *ownerReadMutationContext) Err() error {
	c.checks++
	if c.checks == 2 {
		c.mutate()
	}
	return c.Context.Err()
}

func TestOwnerReaderDetectsReplacementDuringRead(t *testing.T) {
	dir := t.TempDir()
	writeOwnerFixture(t, dir, "agents/fixture-role.md", []byte("Original instructions"))
	reader, err := openOwner(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.root.Close()
	ctx := &ownerReadMutationContext{Context: t.Context(), mutate: func() {
		target := filepath.Join(dir, "agents", "fixture-role.md")
		if err := os.Rename(target, target+".old"); err != nil {
			t.Fatal(err)
		}
		writeOwnerFixture(t, dir, "agents/fixture-role.md", []byte("Replacement instructions"))
	}}
	if raw, err := reader.read(ctx, "agents/fixture-role.md"); failureCategory(err) != "changed" || raw != nil {
		t.Fatalf("concurrently replaced owner file returned content or wrong error: %v", err)
	}
}
