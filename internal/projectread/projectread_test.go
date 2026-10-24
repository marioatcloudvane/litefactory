package projectread

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestSessionOffersOnlyReadToolsAtThePinnedCommit(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	repo.WriteFile("feature.go", "package feature\n\nfunc Planned() { panic(\"not implemented\") }\n")
	pinnedHead := repo.Commit("Add a stub")
	repo.WriteFile("feature.go", "package feature\n\nfunc Planned() {}\n")
	repo.Commit("Replace the stub after the pin")
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := db.UpsertRepo(ctx, store.Repo{Path: repo.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "app", RemoteURL: "git@github.com:acme/app.git", BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	reader := Reader{DB: db, Git: binary, Redactor: redact.New(redact.Options{})}
	pinned, err := reader.Pin(ctx, registered.ID)
	if err != nil || pinned.Name != "acme/app" || pinned.Commit == "" || pinned.Gap != "" {
		t.Fatalf("pin=%+v %v", pinned, err)
	}
	// The pin is the remote base head; this fixture's commits are local only.
	pinned.Commit = pinnedHead

	session := Session{Reader: reader, RunsRoot: t.TempDir(), Executable: "/usr/bin/true"}
	prepared := int64(0)
	session.Prepare = func(_ context.Context, id int64) error { prepared = id; return nil }
	launch, stop, err := session.Start(ctx, pinned, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := launch.PermittedTools(); !slices.Equal(got, ToolNames()) || prepared != registered.ID {
		t.Fatalf("catalog=%v prepared=%d", got, prepared)
	}
	if err = stop(); err != nil {
		t.Fatal(err)
	}

	budget, err := logevidence.NewAnalystBudget(logevidence.AnalystLimits())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	incidents, err := logevidence.New(ctx, logevidence.Options{Store: db, RunsRoot: session.RunsRoot, Redactor: reader.Redactor, Budget: budget, Outcomes: true, Scope: logevidence.Scope{RepositoryIDs: []int64{registered.ID}, StartedAfter: now.Add(-IncidentWindow), StartedBefore: now}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = incidents.Close() })
	h := &handler{session: session, project: pinned, incidents: incidents}
	n := 0
	call := func(method, name string, args any) (map[string]any, error) {
		n++
		payload, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
		b, err := h.handle(ctx, submission.Request{Method: method, RequestID: fmt.Sprintf("r%d", n), Payload: payload})
		if err != nil {
			return nil, err
		}
		var out map[string]any
		return out, json.Unmarshal(b, &out)
	}
	list, err := call("tools/list", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tools, ok := list["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list=%v", list)
	}
	for _, tool := range tools {
		entry, ok := tool.(map[string]any)
		if !ok {
			t.Fatalf("tool entry=%v", tool)
		}
		name, ok := entry["name"].(string)
		if !ok {
			t.Fatalf("tool name=%v", entry)
		}
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, ToolNames()) {
		t.Fatalf("offered %v", names)
	}
	out, err := call("tools/call", "repo_read", map[string]any{"path": "feature.go"})
	if err != nil || !strings.Contains(fmt.Sprint(out["text"]), "not implemented") || !strings.Contains(fmt.Sprint(out["text"]), pinnedHead) {
		t.Fatalf("repo_read did not read the pinned commit: %v %v", out, err)
	}
	out, err = call("tools/call", "repo_log", map[string]any{})
	if err != nil || strings.Contains(fmt.Sprint(out["text"]), "after the pin") || !strings.Contains(fmt.Sprint(out["text"]), "Add a stub") {
		t.Fatalf("repo_log went past the pinned commit: %v %v", out, err)
	}
	out, err = call("tools/call", "repo_grep", map[string]any{"pattern": "not implemented"})
	if err != nil || !strings.Contains(fmt.Sprint(out["text"]), "feature.go:3:") {
		t.Fatalf("repo_grep=%v %v", out, err)
	}
	out, err = call("tools/call", "repo_read", map[string]any{"path": "feature.go", "repository": "other/repo"})
	if err != nil || out["status"] != "unavailable" {
		t.Fatalf("another repository was readable: %v %v", out, err)
	}
	out, err = call("tools/call", "incidents_list", map[string]any{})
	if err != nil || out["status"] == nil {
		t.Fatalf("incidents_list=%v %v", out, err)
	}
	for _, name := range []string{"runs_list", "run_logs_read", "communication_submit"} {
		if _, err = call("tools/call", name, map[string]any{}); err == nil {
			t.Fatalf("%s was served", name)
		}
	}
}
