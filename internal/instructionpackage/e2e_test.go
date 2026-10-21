package instructionpackage

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

// Exercise the capture boundary through its public API without launching an agent.
func TestOwnerCaptureRetainsInstructionsAfterSourcesChange(t *testing.T) {
	baseline := fstest.MapFS{
		"metadata.yaml":          {Data: []byte("schema: 1\nversion: v1\n")},
		"catalogue.yaml":         {Data: []byte("- fixture-role\n")},
		"agents/fixture-role.md": {Data: []byte("Shipped instructions.\n")},
	}
	ownerDir := t.TempDir()
	ownerCatalogue := []byte("# Owner catalogue\n- fixture-role\n")
	ownerInstructions := []byte("Owner instructions with exact whitespace.\n\n")
	write := func(relative string, content []byte) {
		t.Helper()
		name := filepath.Join(ownerDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("catalogue.yaml", ownerCatalogue)
	write("agents/fixture-role.md", ownerInstructions)

	pkg, err := Load(t.Context(), baseline, ownerDir)
	if err != nil {
		t.Fatal(err)
	}
	captured, diagnostics, err := pkg.Capture(t.Context(), "fixture-role")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("valid owner capture has diagnostics: %+v", diagnostics)
	}
	if !bytes.Equal(captured.Instructions(), ownerInstructions) {
		t.Fatal("capture did not select exact owner instructions")
	}
	before := captured.Snapshot()
	if before.Role != "fixture-role" || before.ShippedVersion != "v1" || before.ShippedDigest == "" || before.EffectiveDigest == "" {
		t.Fatalf("capture lacks role or package identity: %+v", before)
	}
	if len(before.Files) != 2 {
		t.Fatalf("capture has %d files, want catalogue and role", len(before.Files))
	}
	wantFiles := map[string][]byte{
		"catalogue.yaml":         ownerCatalogue,
		"agents/fixture-role.md": ownerInstructions,
	}
	for _, file := range before.Files {
		want, ok := wantFiles[file.Path]
		if !ok || !bytes.Equal(file.Content, want) || file.Source != SourceOwner || file.Hash == "" {
			t.Fatalf("capture did not retain exact owner file and provenance: %s", file.Path)
		}
		delete(wantFiles, file.Path)
	}
	if len(wantFiles) != 0 {
		t.Fatal("capture omitted selected files")
	}
	retained, err := captured.Encode()
	if err != nil {
		t.Fatal(err)
	}

	// Both live sources now differ from the launch's retained instructions.
	baseline["metadata.yaml"].Data = []byte("schema: 1\nversion: v2\n")
	baseline["catalogue.yaml"].Data = []byte("[]\n")
	baseline["agents/fixture-role.md"].Data = []byte("Changed shipped instructions.\n")
	write("catalogue.yaml", []byte("[]\n"))
	write("agents/fixture-role.md", []byte("Changed owner instructions.\n"))
	if err := os.RemoveAll(ownerDir); err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(retained)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Instructions(), ownerInstructions) {
		t.Fatal("restore changed retained owner instructions")
	}
	if !reflect.DeepEqual(restored.Snapshot(), before) {
		t.Fatal("restore changed retained catalogue, provenance, or digests")
	}
	if !reflect.DeepEqual(captured.Snapshot(), before) {
		t.Fatal("live source changes changed an existing capture")
	}
}
