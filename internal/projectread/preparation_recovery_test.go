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
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestBudgetPausedPreparationMovesToNewAuthorizedPass(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "reads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	fixture := gittest.New(t)
	fixture.WriteFile("large.txt", strings.Repeat("z", 3*preparationBytes+1))
	commit := fixture.Commit("large source")
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: store.ForgeGitHub, Owner: "fixture", Name: "recovery", BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old := NewViews()
	reader := Reader{DB: db, Git: binary, Redactor: redact.New(redact.Options{Environ: []string{}}), Views: old}
	project := Project{ID: repo.ID, Name: repo.Slug(), BaseBranch: gittest.DefaultBranch, Commit: commit}
	call := func(request Request) map[string]any {
		t.Helper()
		data, _, err := reader.Call(ctx, project, "repo_read", request)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	handler := handler{session: Session{Reader: reader}, project: project, repoCalls: maxRepoCalls - 1}
	args, _ := json.Marshal(Request{Path: "large.txt"})
	payload, _ := json.Marshal(submission.ToolCall{Name: "repo_read", Arguments: args})
	firstRaw, err := handler.handle(ctx, submission.Request{Method: "tools/call", RequestID: "last-authorized-call", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	var first map[string]any
	if err = json.Unmarshal(firstRaw, &first); err != nil {
		t.Fatal(err)
	}
	if first["status"] != "budget_limited" || first["continuation"] != nil {
		t.Fatalf("first page: %v", first)
	}
	locator, ok := first["locator"].(string)
	if !ok || locator == "" {
		t.Fatal("budget exhaustion lost durable preparation position")
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := NewViews()
	t.Cleanup(func() { _ = fresh.Close() })
	reader.Views = fresh
	resumed := call(Request{Locator: locator})
	if resumed["status"] != "preparing" || resumed["source_bytes_prepared"] != float64(2*preparationBytes) {
		t.Fatalf("new pass restarted or skipped unfinished source: %v", resumed)
	}
	replay := call(Request{Locator: locator})
	if replay["status"] != "unavailable" {
		t.Fatalf("private state moved twice: %v", replay)
	}
	if err = fresh.Close(); err != nil {
		t.Fatal(err)
	}

}
