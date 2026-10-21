package instructionpackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/config"
)

func baselineFixture() fstest.MapFS {
	return fstest.MapFS{
		MetadataPath:             {Data: []byte("schema: 1\nversion: v1\n")},
		CataloguePath:            {Data: []byte("- fixture-role\n")},
		"agents/fixture-role.md": {Data: []byte("Shipped role instructions.\n")},
	}
}

func TestBaselineRejectsInvalidInventory(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(fstest.MapFS)
	}{
		{"missing metadata", func(f fstest.MapFS) { delete(f, MetadataPath) }},
		{"unsupported schema", func(f fstest.MapFS) { f[MetadataPath].Data = []byte("schema: 2\nversion: v1\n") }},
		{"unknown metadata field", func(f fstest.MapFS) { f[MetadataPath].Data = []byte("schema: 1\nversion: v1\nextra: true\n") }},
		{"duplicate metadata field", func(f fstest.MapFS) { f[MetadataPath].Data = []byte("schema: 1\nschema: 1\nversion: v1\n") }},
		{"blank version", func(f fstest.MapFS) { f[MetadataPath].Data = []byte("schema: 1\nversion: ' '\n") }},
		{"wrong version type", func(f fstest.MapFS) { f[MetadataPath].Data = []byte("schema: 1\nversion: 1\n") }},
		{"missing catalogue", func(f fstest.MapFS) { delete(f, CataloguePath) }},
		{"mapping catalogue", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("agents: [fixture-role]\n") }},
		{"null catalogue", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("null\n") }},
		{"duplicate role", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("[fixture-role, fixture-role]\n") }},
		{"extension", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("[fixture-role.md]\n") }},
		{"traversal", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("[../fixture-role]\n") }},
		{"uppercase", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("[Fixture-role]\n") }},
		{"alias", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("- &role fixture-role\n- *role\n") }},
		{"multiple documents", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("[fixture-role]\n---\n[]\n") }},
		{"malformed YAML", func(f fstest.MapFS) { f[CataloguePath].Data = []byte("[fixture-role\n") }},
		{"missing role", func(f fstest.MapFS) { delete(f, "agents/fixture-role.md") }},
		{"blank role", func(f fstest.MapFS) { f["agents/fixture-role.md"].Data = []byte(" \n\t") }},
		{"non UTF8 role", func(f fstest.MapFS) { f["agents/fixture-role.md"].Data = []byte{0xff} }},
		{"oversize role", func(f fstest.MapFS) { f["agents/fixture-role.md"].Data = bytes.Repeat([]byte("x"), MaxFileBytes+1) }},
		{"oversize catalogue", func(f fstest.MapFS) { f[CataloguePath].Data = bytes.Repeat([]byte(" "), MaxFileBytes+1) }},
		{"unlisted file", func(f fstest.MapFS) { f["agents/unlisted.md"] = &fstest.MapFile{Data: []byte("Other role")} }},
		{"symlink", func(f fstest.MapFS) { f["agents/fixture-role.md"].Mode = fs.ModeSymlink }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := baselineFixture()
			tc.mutate(f)
			if _, err := Load(t.Context(), f, ""); err == nil {
				t.Fatal("invalid baseline accepted")
			}
		})
	}
}

func TestBaselineRejectsInventoryAndTotalBounds(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		f := baselineFixture()
		for i := 0; i < MaxFiles; i++ {
			f[fmt.Sprintf("extra/%d", i)] = &fstest.MapFile{Data: []byte("x")}
		}
		if _, err := Load(t.Context(), f, ""); err == nil {
			t.Fatal("oversize inventory accepted")
		}
	})
	t.Run("total bytes", func(t *testing.T) {
		f := baselineFixture()
		for i := 0; i < 16; i++ {
			f[fmt.Sprintf("extra/%d", i)] = &fstest.MapFile{Data: bytes.Repeat([]byte("x"), MaxFileBytes)}
		}
		if _, err := Load(t.Context(), f, ""); err == nil {
			t.Fatal("oversize aggregate accepted")
		}
	})
}

func TestBaselineOwnsBytesAndDeterministicIdentity(t *testing.T) {
	f := baselineFixture()
	first, err := Load(t.Context(), f, "")
	if err != nil {
		t.Fatal(err)
	}
	reordered := fstest.MapFS{}
	for _, name := range []string{"agents/fixture-role.md", CataloguePath, MetadataPath} {
		reordered[name] = f[name]
	}
	second, err := Load(t.Context(), reordered, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Version() != "v1" || first.Digest() == "" || first.Digest() != second.Digest() {
		t.Fatal("baseline identity is absent or order dependent")
	}
	roles := first.Roles()
	roles[0] = "modified"
	if !slices.Equal(first.Roles(), []string{"fixture-role"}) {
		t.Fatal("roles accessor exposed mutable storage")
	}
	f["agents/fixture-role.md"].Data[0] = 'X'
	if bytes.Equal(first.files["agents/fixture-role.md"], f["agents/fixture-role.md"].Data) {
		t.Fatal("baseline retained caller-owned bytes")
	}
	changed, err := Load(t.Context(), f, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest() == first.Digest() {
		t.Fatal("changed content retained old digest")
	}
}

func TestEmbeddedCatalogueRegistersMigratedRoles(t *testing.T) {
	pkg, err := New(t.Context(), config.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	want := append(ConversationRoles(), JudgeRole, "merge-conflict-advisor", "schedule-proposal-advisor", "mode-classifier", "doctor-diagnosis", "doctor-repair")
	if pkg.Version() != "1.8.0" || pkg.Digest() == "" {
		t.Fatal("embedded package must register the migrated roles and advance its version")
	}
	if len(pkg.files) != 17 {
		t.Fatalf("embedded inventory has %d files, want eleven roles, metadata, catalogue, two judge resources and two doctor skills", len(pkg.files))
	}
	for _, role := range want {
		if !slices.Contains(pkg.Roles(), role) {
			t.Fatalf("embedded catalogue missing role %s", role)
		}
		capture, _, err := pkg.Capture(t.Context(), role)
		if err != nil || capture == nil || len(capture.Instructions()) == 0 {
			t.Fatalf("capture %s: %v", role, err)
		}
		if role == JudgeRole {
			for _, name := range []string{JudgeRolePath, JudgePinPrefixPath, JudgeEvidenceSuffixPath} {
				if len(capture.Resource(name)) == 0 {
					t.Fatalf("missing judge resource %s", name)
				}
			}
		}
	}
}

func TestEmbeddedCatalogueIncludesChatRoles(t *testing.T) {
	pkg, err := New(t.Context(), config.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"chat-draft"}
	if pkg.Version() == "" || pkg.Digest() == "" {
		t.Fatal("embedded chat catalogue identity mismatch")
	}
	for _, role := range want {
		if !slices.Contains(pkg.Roles(), role) {
			t.Fatalf("embedded catalogue missing chat role %s", role)
		}
		capture, err := pkg.CaptureShipped(t.Context(), role)
		if err != nil || capture == nil || len(capture.Instructions()) == 0 {
			t.Fatalf("capture %s: %v", role, err)
		}
	}
}

func TestBaselineHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, baselineFixture(), ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load = %v", err)
	}
}
