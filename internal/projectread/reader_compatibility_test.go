package projectread

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// Both the analyst session and standards-author delegate to Reader.Call.
func TestReaderCallerCompatibility(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "compat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	repo.WriteFile("src/file.go", "package sample\n// retained marker\n")
	pin := repo.Commit("Retained source")
	repo.WriteFile("src/file.go", "package changed\n")
	repo.Commit("Later source")
	repo.WriteFile("src/file.go", "uncommitted content")
	registered, err := db.UpsertRepo(ctx, store.Repo{Path: repo.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "app", RemoteURL: "git@github.com:acme/app.git", BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader := Reader{DB: db, Git: binary, Redactor: redact.New(redact.Options{})}
	project := Project{ID: registered.ID, Name: "acme/app", BaseBranch: gittest.DefaultBranch, Commit: pin}
	for _, tc := range []struct {
		name    string
		request Request
		want    string
	}{
		{"repo_read", Request{Path: "src/file.go"}, "retained marker"},
		{"repo_grep", Request{Path: "src", Pattern: "retained"}, "src/file.go:2:"},
		{"repo_log", Request{Path: "src/file.go", Query: "Retained"}, "Retained source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, ok, err := reader.Call(ctx, project, tc.name, tc.request)
			if err != nil || !ok {
				t.Fatalf("result %s %v", raw, err)
			}
			var out map[string]any
			if err = json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tc.want) || strings.Contains(string(raw), "uncommitted content") || strings.Contains(string(raw), "Later source") {
				t.Fatalf("wrong committed evidence: %s", raw)
			}
		})
	}
	raw, ok, err := reader.Call(ctx, Project{ID: registered.ID, Name: project.Name}, "repo_grep", Request{Pattern: "retained"})
	if err != nil || ok || !strings.Contains(string(raw), "unavailable") {
		t.Fatalf("missing pin: %s %v", raw, err)
	}
}
