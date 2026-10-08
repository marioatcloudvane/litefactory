package store

import (
	"encoding/json"
	"fmt"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
	"strings"
	"testing"
	"time"
)

type creationDBFixture struct {
	db       *DB
	c        *CreationStore
	now      time.Time
	instance string
}

func newCreationDBFixture(t *testing.T) *creationDBFixture {
	t.Helper()
	f := &creationDBFixture{db: newTestDB(t), now: time.Now().UTC().Add(-time.Minute), instance: strings.Repeat("a", 64)}
	var err error
	f.c, err = NewCreationStore(f.db, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.StartMCPInstance(t.Context(), f.instance); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *creationDBFixture) proposal(t *testing.T, repoName string, issue, count int) (Session, CreationProposal) {
	t.Helper()
	repo := testRepo(t, f.db, "owner", repoName)
	s, err := f.db.GetOrCreateSession(t.Context(), repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := make([]map[string]string, count)
	for i := range artifacts {
		artifacts[i] = map[string]string{"title": fmt.Sprintf("Issue %d", i), "body": "Exact publishable content"}
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "repository": "owner/" + repoName, "source_task": s.TaskRef, "relationship": "linked", "artifacts": artifacts})
	if err != nil {
		t.Fatal(err)
	}
	p := CreationProposal{Hash: submissionDigest(raw), Repository: "owner/" + repoName, SourceTask: s.TaskRef, Relationship: "linked", Artifacts: count, Envelope: raw}
	return s, p
}
func (f *creationDBFixture) lease(t *testing.T, s Session, attempt, mode string, expiry time.Time) MCPLease {
	t.Helper()
	b, err := f.db.BindResultAttempt(t.Context(), resultRun(t, f.db, s, mode, attempt, int64(len(attempt)+1)))
	if err != nil {
		t.Fatal(err)
	}
	l := MCPLease{Binding: b, Instance: f.instance, PermissionDigest: strings.Repeat("b", 64), ExpiresAt: expiry}
	if err = f.db.BindMCPLease(t.Context(), l); err != nil {
		t.Fatal(err)
	}
	// Lease bookkeeping has a production clock. Inject only its persisted launch
	// timestamp to align it with the deterministic creation clock for boundary tests.
	if _, err = f.db.sql.ExecContext(t.Context(), `UPDATE mcp_attempt_leases SET bound_ns=? WHERE run_id=? AND attempt=?`, f.now.UnixNano(), b.RunID, b.Attempt); err != nil {
		t.Fatal(err)
	}
	return l
}
func (f *creationDBFixture) approved(t *testing.T, repo string, issue, count int) (MCPLease, CreationProposal, int64) {
	t.Helper()
	s, p := f.proposal(t, repo, issue, count)
	if err := f.c.RetainProposal(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	a := CreationApproval{Repository: p.Repository, SourceTask: p.SourceTask, ProposalHash: p.Hash, CommentID: int64(issue), OwnerID: 10, ActorID: 10, CreatedAt: f.now}
	id, err := f.c.AdmitOwnerIntent(t.Context(), a, "linked")
	if err != nil {
		t.Fatal(err)
	}
	l := f.lease(t, s, "cold", "research", f.now.Add(time.Hour))
	return l, p, id
}
func TestMCPCreationDB_ApprovalProvenanceAndImmutableReplay(t *testing.T) {
	f := newCreationDBFixture(t)
	_, p := f.proposal(t, "api", 278, 1)
	if err := f.c.RetainProposal(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	good := CreationApproval{Repository: p.Repository, SourceTask: p.SourceTask, ProposalHash: p.Hash, CommentID: 1, OwnerID: 10, ActorID: 10, CreatedAt: f.now}
	for name, change := range map[string]func(*CreationApproval){"nonowner": func(a *CreationApproval) { a.ActorID = 11 }, "bot": func(a *CreationApproval) { a.Bot = true }, "missing comment": func(a *CreationApproval) { a.CommentID = 0 }, "wrong repo": func(a *CreationApproval) { a.Repository = "other/api" }, "wrong source": func(a *CreationApproval) { a.SourceTask = "github:owner/api#279" }, "absent proposal": func(a *CreationApproval) { a.ProposalHash = strings.Repeat("f", 64) }, "before proposal": func(a *CreationApproval) { a.CreatedAt = a.CreatedAt.Add(-time.Nanosecond) }, "future": func(a *CreationApproval) { a.CreatedAt = a.CreatedAt.Add(time.Nanosecond) }, "edited before intake": func(a *CreationApproval) { a.UpdatedAt = a.CreatedAt.Add(time.Second) }} {
		t.Run(name, func(t *testing.T) {
			a := good
			change(&a)
			if _, err := f.c.AdmitOwnerIntent(t.Context(), a, "linked"); err == nil {
				t.Fatal("accepted unauthorized approval")
			}
		})
	}
	id, err := f.c.AdmitOwnerIntent(t.Context(), good, "linked")
	if err != nil {
		t.Fatal(err)
	}
	edited := good
	edited.UpdatedAt = f.now.Add(time.Second)
	if again, err := f.c.AdmitOwnerIntent(t.Context(), edited, "linked"); err != nil || again != id {
		t.Fatalf("edit replay changed immutable intent: %d %v", again, err)
	}
	changed := p
	changed.Envelope = append([]byte(nil), p.Envelope...)
	changed.Envelope = append(changed.Envelope, ' ')
	changed.Hash = submissionDigest(changed.Envelope)
	if err = f.c.RetainProposal(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	if _, err = f.c.AdmitOwnerIntent(t.Context(), good, "linked"); err == nil {
		t.Fatal("changed proposal accepted")
	}
	if err = f.c.RetainProposal(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err = f.c.AdmitOwnerIntent(t.Context(), good, "linked"); err == nil {
		t.Fatal("returning to old proposal revived approval")
	}
}
func TestMCPCreationDB_ReopenRetainsOperationWithoutAuthority(t *testing.T) {
	f := newCreationDBFixture(t)
	l, _, intent := f.approved(t, "api", 278, 5)
	id := creationfixture.CreationHistory(t, DSN(f.db.Path()), intent, l.Binding.RunID, l.Binding.Attempt, "", 0)
	var err error
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), f.db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewCreationStore(reopened, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.StartMCPInstance(t.Context(), strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	op, err := recovered.GetCreationOperation(t.Context(), id)
	if err != nil || op.State != "uncertain" || !op.Dispatched {
		t.Fatalf("recovery lost outcome %+v %v", op, err)
	}
	if _, err = recovered.ReconcileOperation(t.Context(), op.ID, "never_dispatched"); err == nil {
		t.Fatal("released uncertain dispatched quota")
	}
	if _, err = recovered.ReconcileOperation(t.Context(), op.ID, "confirmed"); err != nil {
		t.Fatal(err)
	}
}
