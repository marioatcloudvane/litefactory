package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/rights"
	"github.com/cloudvane/litefactory/internal/store"
)

type cliCatalogue struct{}

func (cliCatalogue) Catalogue(context.Context, store.Repo) ([]rights.Entry, error) {
	return []rights.Entry{{Workflow: "plan", Stage: "review", Action: "approve", Description: "Plan review"}}, nil
}

type rightsCLI struct {
	t       *testing.T
	lookups int
	dbPath  string
	repoID  int64
}

func newRightsCLI(t *testing.T) *rightsCLI {
	t.Helper()
	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []store.Identity{{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 7, Source: store.SourceGH}, {Forge: store.ForgeGitHub, Role: store.RoleBot, Handle: "bot", UserID: 9, Source: store.SourceForgeAPI}} {
		if _, err := db.UpsertIdentity(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(paths.Home(), "wt")})
	if err != nil {
		t.Fatal(err)
	}
	return &rightsCLI{t: t, dbPath: paths.DBPath(), repoID: repo.ID}
}

func (r *rightsCLI) run(args ...string) (int, string, string) {
	r.t.Helper()
	var stdout, stderr bytes.Buffer
	env := &Env{Stdout: &stdout, Stderr: &stderr, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler), Version: "test",
		RightsCatalogue: cliCatalogue{},
		LookupUser: func(_ context.Context, login string) (github.User, error) {
			r.lookups++
			if login == "ann" {
				return github.User{ID: 42, Login: "Ann"}, nil
			}
			return github.User{}, fmt.Errorf("no such user %q", login)
		}}
	code := NewRouter("litefactory", env, Commands()).Run(r.t.Context(), args)
	return code, stdout.String(), stderr.String()
}

func TestRightsCommandsEndToEnd(t *testing.T) {
	r := newRightsCLI(t)
	code, out, errOut := r.run("rights", "catalogue", "--repo", "acme/api")
	if code != ExitOK || !strings.Contains(out, "plan | review | approve") {
		t.Fatalf("catalogue %d %q %q", code, out, errOut)
	}
	code, out, _ = r.run("rights", "catalogue", "--repo", "acme/api", "--json")
	var entries []rights.Entry
	if code != ExitOK || json.Unmarshal([]byte(out), &entries) != nil || len(entries) != 1 {
		t.Fatalf("catalogue json %d %q", code, out)
	}

	code, out, errOut = r.run("rights", "grant", "ann", "--repo", "acme/api", "--workflow", "plan", "--stage", "review", "--action", "approve", "--task", "5", "--json")
	if code != ExitOK {
		t.Fatalf("grant %d %q", code, errOut)
	}
	var g store.RightsGrant
	if err := json.Unmarshal([]byte(out), &g); err != nil || g.SubjectID != 42 || g.SubjectLogin != "Ann" || g.GrantedBy != 7 || g.TaskRef != "github:acme/api#5" {
		t.Fatalf("grant json %q %+v %v", out, g, err)
	}
	if r.lookups != 1 {
		t.Fatalf("lookups = %d", r.lookups)
	}
	if code, _, _ := r.run("rights", "grant", "ann", "--repo", "acme/api", "--workflow", "plan", "--stage", "review", "--action", "approve", "--task", "5"); code != ExitPrecondition {
		t.Fatalf("duplicate grant exit %d", code)
	}
	if code, _, _ := r.run("rights", "grant", "ann", "--repo", "acme/api", "--workflow", "plan", "--stage", "nope", "--action", "approve"); code != ExitPrecondition {
		t.Fatalf("uncatalogued grant exit %d", code)
	}
	if code, _, _ := r.run("rights", "grant", "ghost", "--repo", "acme/api", "--workflow", "plan", "--stage", "review", "--action", "approve"); code == ExitOK {
		t.Fatal("unknown login granted")
	}
	code, out, _ = r.run("rights", "list", "--repo", "acme/api")
	if code != ExitOK || !strings.Contains(out, "Ann (id 42)") || !strings.Contains(out, "active") {
		t.Fatalf("list %d %q", code, out)
	}

	code, out, errOut = r.run("reviewer", "assign", "ann", "--repo", "acme/api", "--task", "5", "--workflow", "plan", "--stage", "review", "--json")
	var a store.ReviewerAssignment
	if code != ExitOK || json.Unmarshal([]byte(out), &a) != nil || a.ReviewerID != 42 || a.AssignedBy != 7 {
		t.Fatalf("assign %d %q %q", code, out, errOut)
	}
	code, out, _ = r.run("reviewer", "list", "--repo", "acme/api", "--task", "5")
	if code != ExitOK || !strings.Contains(out, "Ann (id 42)") {
		t.Fatalf("reviewer list %d %q", code, out)
	}
	if code, _, _ := r.run("reviewer", "remove", fmt.Sprint(a.ID), "--repo", "acme/api"); code != ExitOK {
		t.Fatalf("remove exit %d", code)
	}
	if code, out, _ = r.run("reviewer", "list", "--repo", "acme/api", "--json"); code != ExitOK || strings.TrimSpace(out) != "[]" {
		t.Fatalf("open list after remove %d %q", code, out)
	}
	if code, _, _ := r.run("rights", "revoke", fmt.Sprint(g.ID), "--repo", "acme/api"); code != ExitOK {
		t.Fatalf("revoke exit %d", code)
	}
	if code, out, _ = r.run("rights", "list", "--repo", "acme/api"); code != ExitOK || strings.TrimSpace(out) != "" {
		t.Fatalf("active list after revoke %d %q", code, out)
	}
	code, out, _ = r.run("rights", "history", "--repo", "acme/api", "--json")
	var hist []historyView
	if code != ExitOK || json.Unmarshal([]byte(out), &hist) != nil || len(hist) != 4 {
		t.Fatalf("history %d %q", code, out)
	}
	for i, kind := range []string{"grant", "assign", "remove", "revoke"} {
		if hist[i].Kind != kind || hist[i].ActorID != 7 {
			t.Fatalf("history[%d] = %+v", i, hist[i])
		}
	}
	if code, out, _ = r.run("rights", "history", "--repo", "acme/api"); code != ExitOK || !strings.Contains(out, "actor 7") {
		t.Fatalf("history text %d %q", code, out)
	}
}

func TestRightsCommandsRefuseBadRepository(t *testing.T) {
	r := newRightsCLI(t)
	if code, _, _ := r.run("rights", "list"); code != ExitUsage {
		t.Fatalf("missing --repo exit %d", code)
	}
	if code, _, _ := r.run("rights", "list", "--repo", "acme/nope"); code != ExitPrecondition {
		t.Fatalf("unregistered repository exit %d", code)
	}
}
