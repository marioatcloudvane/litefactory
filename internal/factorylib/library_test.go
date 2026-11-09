package factorylib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

const repoSource = "../../teams/source/factory"

func TestRepoSourceMatchesRegistry(t *testing.T) {
	lib, err := ReadSource(context.Background(), repoSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range Roles() {
		c, err := lib.Capture(context.Background(), role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if len(c.Instructions()) == 0 {
			t.Fatalf("%s: empty role", role)
		}
	}
	conv, err := lib.CaptureConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if conv.InboxOrganizer() == "" || conv.Factory() == conv.Native() {
		t.Fatal("conversation capture lacks a role")
	}
}

func TestInstallLoadAndReinstall(t *testing.T) {
	ctx := context.Background()
	paths := config.NewPaths(t.TempDir())
	if _, err := NewInstalled(paths).Library(ctx); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("uninstalled library: %v", err)
	}
	lib, already, err := Install(ctx, paths, repoSource)
	if err != nil || already {
		t.Fatalf("install: %v %v", already, err)
	}
	installed, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	got, err := installed.Library(ctx)
	if err != nil || got.Digest() != lib.Digest() {
		t.Fatalf("installed digest: %v", err)
	}
	if _, already, err = Install(ctx, paths, repoSource); err != nil || !already {
		t.Fatalf("reinstall: %v %v", already, err)
	}
}

func TestInstallRefusesUnregisteredAndMissingFiles(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.CopyFS(source, os.DirFS(repoSource)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "roles", "extra.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, Judge.Path())); err != nil {
		t.Fatal(err)
	}
	_, _, err := Install(ctx, config.NewPaths(t.TempDir()), source)
	if err == nil || !strings.Contains(err.Error(), "roles/extra.md") || !strings.Contains(err.Error(), Judge.Path()) {
		t.Fatalf("install error must name both paths: %v", err)
	}
}

func TestLoadRefusesChangedInstalledFile(t *testing.T) {
	ctx := context.Background()
	paths := config.NewPaths(t.TempDir())
	lib, _, err := Install(ctx, paths, repoSource)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(paths.FactoryLibraryDir(), lib.Digest(), filepath.FromSlash(Judge.Path()))
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewInstalled(paths).Library(ctx); err == nil {
		t.Fatal("changed installed file was accepted")
	}
}

func TestRestoreLegacyCapture(t *testing.T) {
	legacy := `{"schema":1,"role":"challenger","shipped_version":"1.15.0","shipped_digest":"x","files":[` +
		`{"path":"agents/challenger.md","content":"cm9sZQ==","source":"shipped","hash":"` + hashBytes([]byte("role")) + `"},` +
		`{"path":"catalogue.yaml","content":"eA==","source":"shipped","hash":"y"}],` +
		`"effective_digest":"abc","diagnostics":[],"integrity":"z"}`
	c, err := Restore([]byte(legacy))
	if err != nil {
		t.Fatal(err)
	}
	if c.Role() != Judge || string(c.Instructions()) != "role" || c.Digest() != "abc" || c.Library() != "1.15.0" {
		t.Fatalf("legacy capture: %+v", c.data)
	}
	raw, err := c.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if again, err := Restore(raw); err != nil || again.Digest() != "abc" {
		t.Fatalf("re-encoded legacy capture: %v", err)
	}
}
