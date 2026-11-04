package owneractions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type fakeBrainstormDrafts struct{ db *store.DB }

func (f fakeBrainstormDrafts) IssueDrafts(ctx context.Context, _ owneraction.Principal, brainstormID, setID string, keys []string, generation int) (store.BrainstormDraftSet, []BrainstormIssueDraft, error) {
	set, err := f.db.ReadBrainstormDraftSet(ctx, setID)
	if err != nil {
		return set, nil, err
	}
	var out []BrainstormIssueDraft
	for _, k := range keys {
		for _, d := range set.Drafts {
			if d.Key == k {
				out = append(out, BrainstormIssueDraft{DraftID: d.ID, Key: d.Key, Title: d.Title, Body: "body of " + d.Title, Marker: fmt.Sprintf("m %s %s %d", brainstormID, d.ID, generation), Generation: generation, LedgerDigest: "digest"})
			}
		}
	}
	return set, out, nil
}

type brainstormFixture struct {
	db  *store.DB
	raw *sql.DB
	p   owneraction.Principal
	at  time.Time
	b   BrainstormTasks
}

func (f brainstormFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.raw.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func (f brainstormFixture) addSet(t *testing.T, seq int, id string) {
	t.Helper()
	ts := store.FormatTime(f.at)
	f.exec(t, `UPDATE brainstorm_draft_sets SET state='superseded' WHERE state='latest'`)
	f.exec(t, `INSERT INTO brainstorm_scans(id,brainstorm_id,scan_seq,owner_id,owner_generation,repository_ref,request_key,digest,question,team_version,team_files,grant_id,consent_revision,job_id,execution_id,created_at) VALUES(?,'b',?,42,?,'github:acme/api',?,'d','Why?','v','{}','grant','1',?,?,?)`, "scan-"+id, seq, f.p.OwnerGeneration, "k"+id, "job-"+id, "exec-"+id, ts)
	f.exec(t, `INSERT INTO brainstorm_draft_sets(id,brainstorm_id,scan_id,seq,state,summary,created_at) VALUES(?,'b',?,?,'latest','s',?)`, id, "scan-"+id, seq, ts)
	for j := 1; j <= 2; j++ {
		f.exec(t, `INSERT INTO brainstorm_drafts(id,draft_set_id,key,ordinal,title,size,impact,ledger,digest) VALUES(?,?,?,?,?,'S','i','{}','x')`, fmt.Sprintf("%s-d%d", id, j), id, fmt.Sprintf("k%d", j), j, fmt.Sprintf("%s title %d", id, j))
	}
}

func newBrainstormFixture(t *testing.T, scope string) brainstormFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "litefactory.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	now := time.Now().UTC()
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RegisterConnectorClient(t.Context(), store.ConnectorClient{ID: "client", Name: "Client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Forge: "github", Owner: "acme", Name: "api", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	f := brainstormFixture{db: db, raw: raw, at: now, p: owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client", GrantID: "grant"}}
	ts := store.FormatTime(now)
	f.exec(t, `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES('grant','client','https://example.test/litefactory/mcp',?,42,?,?,?)`, scope, owner.Generation, ts, store.FormatTime(now.Add(time.Hour)))
	f.exec(t, `INSERT INTO connector_grant_repositories(grant_id,repo_id,repository_ref) VALUES('grant',?,'github:acme/api')`, repo.ID)
	f.exec(t, `INSERT INTO brainstorms(id,owner_id,owner_generation,repository_ref,state,created_at,last_activity_at) VALUES('b',42,?,'github:acme/api','open',?,?)`, owner.Generation, ts, ts)
	f.addSet(t, 1, "set1")
	protect := func(_ context.Context, s string) (string, error) { return s, nil }
	f.b = BrainstormTasks{DB: db, Drafts: fakeBrainstormDrafts{db: db}, Protect: protect, Availability: Availability{Brainstorm: true}}
	return f
}

const bothScopes = "repository:actions repository:brainstorm"

func createInvocation(set, drafts string) owneraction.Invocation {
	return owneraction.Invocation{Command: BrainstormCreateTasks, Arguments: map[string]string{"brainstorm": "b", "draft_set": set, "drafts": drafts}}
}

func TestBrainstormCreateTasksFreezesExactIssuesAndAdmitsOnce(t *testing.T) {
	f := newBrainstormFixture(t, bothScopes)
	facts, err := f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k2"), f.at)
	if err != nil {
		t.Fatal(err)
	}
	if facts.RepositoryRef != "github:acme/api" || !strings.Contains(facts.Summary, "set1 title 2") || strings.Contains(facts.Summary, "set1 title 1") || !strings.Contains(facts.Payload, "body of set1 title 2") {
		t.Fatalf("facts = %+v", facts)
	}
	if err = f.b.Recheck(t.Context(), f.p, facts, f.at); err != nil {
		t.Fatal(err)
	}
	res, err := f.b.Execute(t.Context(), f.p, facts, "exec-1", f.at)
	if err != nil || res.State != "executing" || res.Reference == "" {
		t.Fatalf("execute = %+v %v", res, err)
	}
	again, err := f.b.Execute(t.Context(), f.p, facts, "exec-1", f.at)
	if err != nil || again.State != "executing" || again.Reference != res.Reference {
		t.Fatalf("replay = %+v %v", again, err)
	}
	items, _ := f.db.BrainstormTaskItems(t.Context(), res.Reference)
	if len(items) != 1 || items[0].DraftID != "set1-d2" || items[0].Body != "body of set1 title 2" {
		t.Fatalf("items = %+v", items)
	}
	rec, found, err := f.b.Recover(t.Context(), f.p, facts, "exec-1", f.at)
	if err != nil || !found || rec.State != "executing" || !strings.Contains(rec.Summary, "pending") {
		t.Fatalf("recover = %+v %v %v", rec, found, err)
	}
	if _, err = f.b.Execute(t.Context(), f.p, facts, "exec-2", f.at); err != nil {
		t.Fatal(err)
	}
	if res2, _ := f.b.Execute(t.Context(), f.p, facts, "exec-3", f.at); res2.State != "failed" {
		t.Fatalf("second admission of a used set = %+v", res2)
	}
	tampered := facts
	tampered.Payload = strings.Replace(facts.Payload, "body of", "changed", 1)
	if err = f.b.Recheck(t.Context(), f.p, tampered, f.at); !errors.Is(err, owneraction.ErrIntegrity) {
		t.Fatalf("tampered payload = %v", err)
	}
}

func TestBrainstormContinuationReachesBatchAfterNewerSet(t *testing.T) {
	f := newBrainstormFixture(t, bothScopes)
	facts, err := f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k1,k2"), f.at)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.b.Execute(t.Context(), f.p, facts, "exec-1", f.at)
	if err != nil || res.State != "executing" {
		t.Fatal(res, err)
	}
	items, _ := f.db.BrainstormTaskItems(t.Context(), res.Reference)
	done, failed := items[0], items[1]
	done.State = store.BrainstormItemPlanStarted
	failed.State = store.BrainstormItemNotCreated
	for _, it := range []store.BrainstormTaskItem{done, failed} {
		if ok, err := f.db.SaveBrainstormTaskItem(t.Context(), it, store.BrainstormItemPending, f.at); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	f.addSet(t, 2, "set2")
	// A newer set never blocks continuing the batch of the used set.
	cont, err := f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k1,k2"), f.at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cont.Summary, "continue batch "+res.Reference) || strings.Contains(cont.Summary, "set1 title 1") || !strings.Contains(cont.Summary, "set1 title 2") {
		t.Fatalf("continue summary = %q", cont.Summary)
	}
	// A frozen item that moves after the proposal refuses the confirmation.
	moved := failed
	moved.State = store.BrainstormItemPending
	if ok, err := f.db.SaveBrainstormTaskItem(t.Context(), moved, store.BrainstormItemNotCreated, f.at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err = f.b.Recheck(t.Context(), f.p, cont, f.at); err == nil || !strings.Contains(err.Error(), "request a fresh proposal") {
		t.Fatalf("moved item recheck = %v", err)
	}
	if r, _ := f.b.Execute(t.Context(), f.p, cont, "exec-c1", f.at); r.State != "failed" {
		t.Fatalf("moved item execute = %+v", r)
	}
	if ok, err := f.db.SaveBrainstormTaskItem(t.Context(), failed, store.BrainstormItemPending, f.at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	cont, err = f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k1,k2"), f.at)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.b.Execute(t.Context(), f.p, cont, "exec-c2", f.at)
	if err != nil || r.State != "executing" || r.Reference != res.Reference {
		t.Fatalf("continue execute = %+v %v", r, err)
	}
	c, err := f.db.BrainstormTaskContinuationByExecution(t.Context(), "exec-c2")
	if err != nil || len(c.Items) != 1 || c.Items[0].ItemID != failed.ID {
		t.Fatalf("frozen = %+v %v", c, err)
	}
	got, _ := f.db.ReadBrainstormTaskItem(t.Context(), failed.ID)
	if got.State != store.BrainstormItemPending {
		t.Fatalf("continued item = %+v", got)
	}
	if err = f.db.SettleBrainstormTaskContinuation(t.Context(), c.ID, f.at); err != nil {
		t.Fatal(err)
	}
	if rec, found, err := f.b.Recover(t.Context(), f.p, cont, "exec-c2", f.at); err != nil || !found || rec.State != "succeeded" {
		t.Fatalf("settled recover = %+v %v %v", rec, found, err)
	}
	// A new batch from the newer set still needs the latest unused set.
	if _, err = f.b.Resolve(t.Context(), f.p, createInvocation("set2", "k1"), f.at); err != nil {
		t.Fatal(err)
	}
}

func TestBrainstormNewBatchNeedsLatestUnusedSet(t *testing.T) {
	f := newBrainstormFixture(t, bothScopes)
	f.addSet(t, 2, "set2")
	if _, err := f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k1"), f.at); err == nil || !strings.Contains(err.Error(), "latest unused") {
		t.Fatalf("superseded set = %v", err)
	}
}

func TestBrainstormTasksRefuseGrantWithoutBrainstormScope(t *testing.T) {
	f := newBrainstormFixture(t, "repository:actions")
	if _, err := f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k1"), f.at); !errors.Is(err, store.ErrConnectorScope) {
		t.Fatalf("old grant = %v", err)
	}
}

func TestBrainstormRecreateOnlyAfterExhaustedChecks(t *testing.T) {
	f := newBrainstormFixture(t, bothScopes)
	facts, _ := f.b.Resolve(t.Context(), f.p, createInvocation("set1", "k1"), f.at)
	res, err := f.b.Execute(t.Context(), f.p, facts, "exec-1", f.at)
	if err != nil {
		t.Fatal(err)
	}
	in := owneraction.Invocation{Command: BrainstormTaskRecreate, Arguments: map[string]string{"batch": res.Reference, "draft": "k1"}}
	if _, err = f.b.Resolve(t.Context(), f.p, in, f.at); err == nil {
		t.Fatal("recreate of a pending item proposed")
	}
	items, _ := f.db.BrainstormTaskItems(t.Context(), res.Reference)
	it := items[0]
	it.State = store.BrainstormItemUncertainExhausted
	if ok, err := f.db.SaveBrainstormTaskItem(t.Context(), it, store.BrainstormItemPending, f.at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	rf, err := f.b.Resolve(t.Context(), f.p, in, f.at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rf.Summary, "first issue may still appear") || !strings.Contains(rf.Summary, "two issues") {
		t.Fatalf("recreate summary = %q", rf.Summary)
	}
	r, err := f.b.Execute(t.Context(), f.p, rf, "exec-r", f.at)
	if err != nil || r.State != "executing" {
		t.Fatalf("recreate = %+v %v", r, err)
	}
	got, _ := f.db.ReadBrainstormTaskItem(t.Context(), it.ID)
	if got.Generation != 2 || got.State != store.BrainstormItemPending || !strings.HasSuffix(got.Marker, " 2") {
		t.Fatalf("recreated = %+v", got)
	}
}

func TestBrainstormCommandsClassifiedAndCatalogedOnlyWhenAvailable(t *testing.T) {
	for _, command := range []string{BrainstormCreateTasks, BrainstormTaskRecreate} {
		tier, scope, err := owneraction.Classify(owneraction.Invocation{Command: command})
		if err != nil || tier != owneraction.Conversational || scope != "repository:actions" {
			t.Fatalf("%s = %q %q %v", command, tier, scope, err)
		}
	}
	has := func(a Availability, command string) bool {
		for _, entry := range a.ActionCatalog() {
			if entry.Command == command {
				return true
			}
		}
		return false
	}
	if has(Availability{Answer: true, Draft: true}, BrainstormCreateTasks) || has(Availability{}, BrainstormTaskRecreate) {
		t.Fatal("brainstorm commands listed while unavailable")
	}
	if !has(Availability{Brainstorm: true}, BrainstormCreateTasks) || !has(Availability{Brainstorm: true}, BrainstormTaskRecreate) {
		t.Fatal("brainstorm commands missing while available")
	}
	if _, err := (Availability{Answer: true}).ValidateChange(createInvocation("s", "k1")); !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("unavailable command validated: %v", err)
	}
}
