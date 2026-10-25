package rights_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/rights"
	"github.com/cloudvane/litefactory/internal/store"
)

const (
	ownerID = 7
	userID  = 42
	task1   = "github:acme/api#1"
)

var clock = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

const untaggedDOT = `digraph {team=builtin;mode=plan;profile_version=1;start=plain;
plain [lf_action=lf_ask,lf_kind=question,lf_outcomes=published];
done [lf_terminal=true];
plain -> done [lf_on=published];}`

const fixtureDOT = `digraph {team=builtin;mode=plan;profile_version=1;start=review;
review [lf_action=lf_ask,lf_kind=question,lf_rights="approve,request_changes",lf_outcomes=published];
plain [lf_action=lf_ask,lf_kind=question,lf_outcomes=published];
done [lf_terminal=true];
review -> plain [lf_on=published];plain -> done [lf_on=published];}`

type fixedSource struct {
	entries []rights.Entry
	err     error
}

func (f *fixedSource) Catalogue(context.Context, store.Repo) ([]rights.Entry, error) {
	return f.entries, f.err
}

func fixtureEntries(t *testing.T) []rights.Entry {
	t.Helper()
	p, err := graphexec.ParseProfile([]byte(fixtureDOT))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := rights.EntriesFromProfiles(p)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

type env struct {
	db   *store.DB
	repo store.Repo
	src  *fixedSource
	svc  *rights.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "lf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "https://github.com/acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	src := &fixedSource{entries: fixtureEntries(t)}
	return &env{db: db, repo: repo, src: src, svc: &rights.Service{Store: db, Catalogue: src, OwnerID: ownerID, Now: func() time.Time { return clock }}}
}

func (e *env) grant(t *testing.T, mutate func(*rights.GrantInput)) store.RightsGrant {
	t.Helper()
	in := rights.GrantInput{RepoID: e.repo.ID, SubjectID: userID, SubjectLogin: "ann", Workflow: "plan", Stage: "review", Action: "approve"}
	if mutate != nil {
		mutate(&in)
	}
	g, err := e.svc.Grant(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (e *env) check(t *testing.T, mutate func(*rights.Request)) (rights.Decision, error) {
	t.Helper()
	req := rights.Request{RepoID: e.repo.ID, UserID: userID, Workflow: "plan", Stage: "review", Action: "approve", TaskRef: task1, Now: clock}
	if mutate != nil {
		mutate(&req)
	}
	return e.svc.Check(t.Context(), req)
}

func TestCatalogueListsOnlyTaggedActionsAndIgnoresRepository(t *testing.T) {
	entries := fixtureEntries(t)
	if len(entries) != 2 || entries[0].String() != "plan | review | approve" || entries[1].String() != "plan | review | request_changes" {
		t.Fatalf("entries: %v", entries)
	}
	src := rights.NewRepositorySource(func(_ context.Context, _ store.Repo, w agent.Workflow, _ graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		dot := untaggedDOT
		if w == agent.WorkflowPlan {
			dot = fixtureDOT
		}
		p, err := graphexec.ParseProfile([]byte(dot))
		return &teamsource.WorkflowDefinition{Profile: p}, err
	})
	a, err := src.Catalogue(t.Context(), store.Repo{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := src.Catalogue(t.Context(), store.Repo{ID: 2})
	if len(a) != 2 || len(b) != 2 || a[0] != b[0] || a[1] != b[1] {
		t.Fatalf("catalogues differ: %v %v", a, b)
	}
}

func TestRepositorySourceIsEmptyWithoutSelectedTeam(t *testing.T) {
	src := rights.NewRepositorySource(func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		return nil, fmt.Errorf("no linked installed team: %w", store.ErrNotFound)
	})
	entries, err := src.Catalogue(t.Context(), store.Repo{ID: 1})
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %v, err %v", entries, err)
	}
}

func TestRepositorySourceReportsOtherFailures(t *testing.T) {
	src := rights.NewRepositorySource(func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		return nil, errors.New("boom")
	})
	if _, err := src.Catalogue(t.Context(), store.Repo{ID: 1}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestGrantRules(t *testing.T) {
	e := newEnv(t)
	g := e.grant(t, func(in *rights.GrantInput) { in.TaskRef = task1; in.ExpiresAt = clock.Add(time.Hour) })
	if g.GrantedBy != ownerID || g.SubjectID != userID || g.TaskRef != task1 || !g.ExpiresAt.Equal(clock.Add(time.Hour)) {
		t.Fatalf("grant: %+v", g)
	}
	base := rights.GrantInput{RepoID: e.repo.ID, SubjectID: userID, SubjectLogin: "ann", Workflow: "plan", Stage: "review", Action: "approve"}
	for name, tc := range map[string]struct {
		edit func(*rights.GrantInput)
		want error
	}{
		"unknown repository": {func(in *rights.GrantInput) { in.RepoID = 999 }, rights.ErrUnknownRepository},
		"not in catalogue":   {func(in *rights.GrantInput) { in.Stage = "plain" }, rights.ErrNotInCatalogue},
		"past expiry":        {func(in *rights.GrantInput) { in.ExpiresAt = clock.Add(-time.Second) }, rights.ErrExpiryPast},
		"expiry now":         {func(in *rights.GrantInput) { in.ExpiresAt = clock }, rights.ErrExpiryPast},
		"duplicate":          {func(in *rights.GrantInput) { in.TaskRef = task1 }, rights.ErrDuplicate},
		"no person":          {func(in *rights.GrantInput) { in.SubjectID = 0 }, rights.ErrInvalid},
	} {
		in := base
		tc.edit(&in)
		if _, err := e.svc.Grant(t.Context(), in); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", name, err, tc.want)
		}
	}
	hist, _ := e.svc.History(t.Context(), e.repo.ID)
	if len(hist) != 1 {
		t.Fatalf("refusals left history: %d", len(hist))
	}
	if _, err := e.svc.Revoke(t.Context(), e.repo.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Revoke(t.Context(), e.repo.ID, g.ID); !errors.Is(err, rights.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	all, _ := e.svc.ListGrants(t.Context(), e.repo.ID, false)
	if len(all) != 1 || all[0].RevokedAt.IsZero() {
		t.Fatalf("revoked row not kept: %+v", all)
	}
}

func TestCheckDecisions(t *testing.T) {
	e := newEnv(t)
	open := e.grant(t, nil)
	scoped := e.grant(t, func(in *rights.GrantInput) { in.Action = "request_changes"; in.TaskRef = task1 })
	expiring := e.grant(t, func(in *rights.GrantInput) {
		in.SubjectID = 43
		in.SubjectLogin = "bo"
		in.ExpiresAt = clock.Add(time.Hour)
	})
	revoked := e.grant(t, func(in *rights.GrantInput) { in.SubjectID = 44; in.SubjectLogin = "cy" })
	if _, err := e.svc.Revoke(t.Context(), e.repo.ID, revoked.ID); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		edit    func(*rights.Request)
		allowed bool
		grant   int64
		owner   bool
		reason  string
	}{
		"owner":                   {func(r *rights.Request) { r.UserID = ownerID }, true, 0, true, ""},
		"exact grant":             {nil, true, open.ID, false, ""},
		"unrestricted any task":   {func(r *rights.Request) { r.TaskRef = "github:acme/api#9" }, true, open.ID, false, ""},
		"task matches":            {func(r *rights.Request) { r.Action = "request_changes" }, true, scoped.ID, false, ""},
		"task differs":            {func(r *rights.Request) { r.Action = "request_changes"; r.TaskRef = "github:acme/api#2" }, false, 0, false, rights.ReasonOtherTask},
		"task missing":            {func(r *rights.Request) { r.Action = "request_changes"; r.TaskRef = "" }, false, 0, false, rights.ReasonOtherTask},
		"before expiry":           {func(r *rights.Request) { r.UserID = 43; r.Now = clock.Add(time.Hour - time.Second) }, true, expiring.ID, false, ""},
		"at expiry":               {func(r *rights.Request) { r.UserID = 43; r.Now = clock.Add(time.Hour) }, false, 0, false, rights.ReasonExpired},
		"revoked":                 {func(r *rights.Request) { r.UserID = 44 }, false, 0, false, rights.ReasonNoGrant},
		"other person":            {func(r *rights.Request) { r.UserID = 99 }, false, 0, false, rights.ReasonNoGrant},
		"other action":            {func(r *rights.Request) { r.UserID = 43; r.Action = "request_changes" }, false, 0, false, rights.ReasonNoGrant},
		"grant earlier in repo 2": {func(r *rights.Request) { r.Stage = "other" }, false, 0, false, rights.ReasonNotInCatalog},
	} {
		d, err := e.check(t, tc.edit)
		wantErr := tc.reason == rights.ReasonNotInCatalog
		if (err != nil) != wantErr {
			t.Errorf("%s: error %v", name, err)
		}
		if d.Allowed != tc.allowed || d.GrantID != tc.grant || d.Owner != tc.owner || d.Reason != tc.reason {
			t.Errorf("%s: decision %+v", name, d)
		}
	}
}

func TestRevocationAndExpiryApplyForwardOnly(t *testing.T) {
	e := newEnv(t)
	g := e.grant(t, nil)
	before, err := e.check(t, nil)
	if err != nil || !before.Allowed {
		t.Fatalf("before: %+v %v", before, err)
	}
	if _, err := e.svc.Revoke(t.Context(), e.repo.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if !before.Allowed || before.GrantID != g.ID {
		t.Fatal("an earlier decision changed")
	}
	after, _ := e.check(t, nil)
	if after.Allowed {
		t.Fatal("revoked grant still allows")
	}
}

func TestCheckFailsClosed(t *testing.T) {
	e := newEnv(t)
	e.grant(t, nil)
	if d, err := e.check(t, func(r *rights.Request) { r.RepoID = 999 }); d.Allowed || !errors.Is(err, rights.ErrUnknownRepository) {
		t.Fatalf("unknown repository: %+v %v", d, err)
	}
	if d, err := e.check(t, func(r *rights.Request) { r.Action = "answer" }); d.Allowed || !errors.Is(err, rights.ErrNotInCatalogue) {
		t.Fatalf("missing entry: %+v %v", d, err)
	}
	boom := errors.New("boom")
	e.src.err = boom
	if d, err := e.check(t, nil); d.Allowed || !errors.Is(err, boom) {
		t.Fatalf("catalogue error: %+v %v", d, err)
	}
	e.src.err = nil
	failing := *e.svc
	failing.Store = failingStore{Store: e.db, err: boom}
	d, err := failing.Check(t.Context(), rights.Request{RepoID: e.repo.ID, UserID: userID, Workflow: "plan", Stage: "review", Action: "approve", Now: clock})
	if d.Allowed || !errors.Is(err, boom) {
		t.Fatalf("store error: %+v %v", d, err)
	}
}

type failingStore struct {
	rights.Store
	err error
}

func (f failingStore) MatchingRightsGrants(context.Context, int64, int64, string, string, string) ([]store.RightsGrant, error) {
	return nil, f.err
}

func TestRemovedTagMakesGrantInactiveButKeepsRow(t *testing.T) {
	e := newEnv(t)
	e.grant(t, nil)
	e.src.entries = nil
	d, err := e.check(t, nil)
	if d.Allowed || err == nil {
		t.Fatalf("grant for an untagged action allowed: %+v %v", d, err)
	}
	rows, _ := e.svc.ListGrants(t.Context(), e.repo.ID, true)
	if len(rows) != 1 {
		t.Fatalf("row lost: %v", rows)
	}
}

func TestReviewerAssignmentGrantsNothing(t *testing.T) {
	e := newEnv(t)
	in := rights.AssignInput{RepoID: e.repo.ID, TaskRef: task1, Workflow: "plan", Stage: "review", ReviewerID: 55, ReviewerLogin: "rae"}
	first, err := e.svc.Assign(t.Context(), in)
	if err != nil || first.AssignedBy != ownerID {
		t.Fatalf("assign: %+v %v", first, err)
	}
	if _, err := e.svc.Assign(t.Context(), in); !errors.Is(err, rights.ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	in.Stage = "plain"
	if _, err := e.svc.Assign(t.Context(), in); !errors.Is(err, rights.ErrNotInCatalogue) {
		t.Fatalf("stage outside catalogue: %v", err)
	}
	in.Stage, in.ReviewerID, in.ReviewerLogin = "review", 56, "sam"
	second, err := e.svc.Replace(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	open, _ := e.svc.ListReviewers(t.Context(), e.repo.ID, task1, true)
	if len(open) != 1 || open[0].ID != second.ID {
		t.Fatalf("open: %+v", open)
	}
	if _, err := e.svc.Remove(t.Context(), e.repo.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Remove(t.Context(), e.repo.ID, second.ID); !errors.Is(err, rights.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
	for _, id := range []int64{55, 56} {
		d, _ := e.check(t, func(r *rights.Request) { r.UserID = id })
		if d.Allowed {
			t.Fatalf("assignment of %d granted a right", id)
		}
	}
	hist, _ := e.svc.History(t.Context(), e.repo.ID)
	var kinds []string
	for _, h := range hist {
		kinds = append(kinds, h.Kind)
	}
	if strings.Join(kinds, ",") != "assign,remove,assign,remove" {
		t.Fatalf("history: %v", kinds)
	}
}
