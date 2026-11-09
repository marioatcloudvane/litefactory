package teampackage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestLocalPolicyRejectsSpecialAndUnsafeRoots(t *testing.T) {
	t.Parallel()
	outside := canonicalTemp(t)
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := canonicalTemp(t)
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(link, "installed")); err == nil {
		t.Fatal("symlink directory accepted")
	}
	if _, err := readLocal(filepath.Join(link, "sentinel"), MaxCatalogueBytes); err == nil {
		t.Fatal("symlink component accepted")
	}
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocal(fifo, MaxCatalogueBytes); err == nil {
		t.Fatal("special file accepted")
	}
	_, server := newFixture(t, "1.0.0")
	p := config.NewPaths(canonicalTemp(t))
	if err := os.Mkdir(p.TeamsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, p.TeamsLockPath()); err != nil {
		t.Fatal(err)
	}
	m := fixtureManager(t, p, server)
	if _, err := m.List(t.Context()); err == nil {
		t.Fatal("symlink lock accepted")
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "preserve" {
		t.Fatal("outside sentinel changed")
	}
}
func TestReleaseFileCountAndTotalBounds(t *testing.T) {
	t.Parallel()
	r, _ := fixtureRelease("1.0.0")
	for i := 0; i < MaxFiles; i++ {
		r.Files = append(r.Files, File{Path: strings.Repeat("x", i+1), SHA256: strings.Repeat("0", 64)})
	}
	r.Checksum = digest(r.Files)
	if err := validateRelease(r); err == nil {
		t.Fatal("file count bound lost")
	}
	// Each listed file stays within its limit; their aggregate exceeds 64 MiB.
	payload := make([]byte, teamsource.MaxSourceBytes)
	r, _ = fixtureRelease("1.0.0")
	r.Files = nil
	files := map[string][]byte{"team.config": []byte("research: research/flow.dot\n")}
	r.Files = append(r.Files, File{Path: "team.config", SHA256: hashBytes(files["team.config"])})
	for i := 0; i < 65; i++ {
		name := "file-" + strings.Repeat("x", i+1)
		r.Files = append(r.Files, File{Path: name, SHA256: hashBytes(payload)})
		files[name] = payload
	}
	r.Checksum = digest(r.Files)
	p := config.NewPaths(canonicalTemp(t))
	m, err := New(Options{Paths: p, Validate: func(context.Context, fs.FS, map[string]string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.InstallBuilt(t.Context(), "file:///fixture/teams/source", files, r, BuildInfo{}); err == nil {
		t.Fatal("release bound lost")
	}
	if entries, err := m.List(t.Context()); err != nil || len(entries) != 0 {
		t.Fatalf("oversized release published: %d %v", len(entries), err)
	}
}

func TestExactContentAccessRefusesTraversalAndNewSymlinks(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	entry := installFixture(t, m, server.URL+"/teams/teams.json", "1.0.0")
	outside := filepath.Join(canonicalTemp(t), "sentinel")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.WithExact(t.Context(), entry.Identity, func(installed Installed, content fs.FS) error {
		if _, err := content.Open("../receipt.json"); err == nil {
			t.Error("traversal accepted")
		}
		target := filepath.Join(installed.Path, "research", "flow.dot")
		if err := os.Remove(target); err != nil {
			return err
		}
		if err := os.Symlink(outside, target); err != nil {
			return err
		}
		if _, err := content.Open("research/flow.dot"); err == nil {
			t.Error("new content symlink accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "preserve" {
		t.Fatal("outside changed")
	}
}
