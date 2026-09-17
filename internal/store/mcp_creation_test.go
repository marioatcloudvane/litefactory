package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	if _, err = f.c.BindGrant(t.Context(), l, id); err != nil {
		t.Fatal(err)
	}
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
func TestMCPCreationDB_ExpiryScopeAndNextLaunch(t *testing.T) {
	for _, which := range []string{"approval", "clamp", "missing", "foreign", "mode", "content", "count", "restart", "cancelled", "resume", "fallback", "before approval"} {
		t.Run(which, func(t *testing.T) {
			f := newCreationDBFixture(t)
			l, p, id := f.approved(t, "api", 278, 1)
			switch which {
			case "approval":
				f.now = f.now.Add(MCPCreationApprovalTTL)
			case "clamp":
				f.now = l.ExpiresAt // grant remains bounded by the earlier approval as well
			case "missing":
				if _, err := f.db.sql.ExecContext(t.Context(), `DELETE FROM mcp_creation_grants`); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				l.PermissionDigest = strings.Repeat("c", 64)
			case "mode":
				l.Binding.Mode = "validate"
			case "content":
				p.Envelope = append(p.Envelope, ' ')
				p.Hash = submissionDigest(p.Envelope)
			case "count":
				p.Artifacts = 2
			case "restart":
				if err := f.db.StartMCPInstance(t.Context(), strings.Repeat("d", 64)); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if err := f.db.RevokeMCPLease(t.Context(), l, "cancelled"); err != nil {
					t.Fatal(err)
				}
			case "resume", "fallback":
				s, err := f.db.GetSessionByTaskRef(t.Context(), l.Binding.Task)
				if err != nil {
					t.Fatal(err)
				}
				next := f.lease(t, s, which, "research", l.ExpiresAt)
				if _, err = f.c.BindGrant(t.Context(), next, id); err == nil {
					t.Fatal("grant transferred")
				}
				l = next
			case "before approval":
				if _, err := f.db.sql.ExecContext(t.Context(), `UPDATE mcp_attempt_leases SET bound_ns=?`, f.now.Add(-time.Nanosecond).UnixNano()); err != nil {
					t.Fatal(err)
				}
				if _, err := f.c.BindGrant(t.Context(), l, id); err == nil {
					t.Fatal("older launch consumed approval")
				}
				return
			}

			if _, err := f.c.ReserveCreation(t.Context(), l, p); err == nil {
				t.Fatal("unauthorized reservation succeeded")
			}
		})
	}
}
func TestMCPCreationDB_ConcurrentSingleDispatchAndRecovery(t *testing.T) {
	f := newCreationDBFixture(t)
	l, p, _ := f.approved(t, "api", 278, 5)
	const n = 16
	results := make(chan CreationReservation, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.c.ReserveCreation(context.Background(), l, p)
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var id int64
	var permit *CreationPermit
	permits := 0
	for r := range results {
		if id != 0 && id != r.Operation.ID {
			t.Fatal("duplicate operation")
		}
		id = r.Operation.ID
		if r.Permit != nil {
			permits++
			permit = r.Permit
		}
	}
	if permits != 1 {
		t.Fatalf("dispatch owners %d", permits)
	}
	if _, err := f.c.BeginDispatch(t.Context(), permit); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.BeginDispatch(t.Context(), permit); err == nil {
		t.Fatal("second dispatch")
	}
	if _, err := f.c.RecordOperationOutcome(t.Context(), permit, "uncertain"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.StartMCPInstance(t.Context(), strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReserveCreation(t.Context(), l, p); err == nil {
		t.Fatal("restart restored grant")
	}
	op, err := f.c.GetCreationOperation(t.Context(), id)
	if err != nil || op.State != "uncertain" {
		t.Fatalf("history: %+v %v", op, err)
	}
	if _, err = f.c.ReconcileOperation(t.Context(), id, "never_dispatched"); err == nil {
		t.Fatal("released uncertain dispatched quota")
	}
	if _, err = f.c.ReconcileOperation(t.Context(), id, "confirmed"); err != nil {
		t.Fatal(err)
	}
}
func TestMCPCreationDB_QuotaRollingAndUnresolved(t *testing.T) {
	for _, state := range []string{"confirmed", "uncertain", "reserved"} {
		t.Run(state, func(t *testing.T) {
			f := newCreationDBFixture(t)
			start := f.now
			for i := 1; i <= 4; i++ {
				l, p, _ := f.approved(t, "api", i, 5)
				r, err := f.c.ReserveCreation(t.Context(), l, p)
				if err != nil {
					t.Fatal(err)
				}
				if state != "reserved" {
					if _, err = f.c.BeginDispatch(t.Context(), r.Permit); err != nil {
						t.Fatal(err)
					}
					if _, err = f.c.RecordOperationOutcome(t.Context(), r.Permit, state); err != nil {
						t.Fatal(err)
					}
				}
			}
			l, p, _ := f.approved(t, "api", 5, 1)
			if _, err := f.c.ReserveCreation(t.Context(), l, p); !errors.Is(err, ErrCreationQuota) {
				t.Fatalf("20 cap: %v", err)
			}
			other, q, _ := f.approved(t, "other", 5, 5)
			if _, err := f.c.ReserveCreation(t.Context(), other, q); err != nil {
				t.Fatalf("repo isolation: %v", err)
			}
			f.now = start.Add(MCPCreationWindow - time.Nanosecond)
			l, p, _ = f.approved(t, "api", 6, 1)
			if _, err := f.c.ReserveCreation(t.Context(), l, p); !errors.Is(err, ErrCreationQuota) {
				t.Fatalf("before rolling boundary: %v", err)
			}
			f.now = start.Add(MCPCreationWindow)
			l, p, _ = f.approved(t, "api", 7, 5)
			_, err := f.c.ReserveCreation(t.Context(), l, p)
			if state == "confirmed" && err != nil {
				t.Fatal(err)
			}
			if state != "confirmed" && !errors.Is(err, ErrCreationQuota) {
				t.Fatalf("old unresolved reservation uncharged: %v", err)
			}
		})
	}
}
func TestMCPCreationDB_ReleaseDoesNotReopenGrantAndDelayedDispatch(t *testing.T) {
	for _, state := range []string{"release", "expired", "revoked", "cancelled context"} {
		t.Run(state, func(t *testing.T) {
			f := newCreationDBFixture(t)
			l, p, _ := f.approved(t, "api", 278, 5)
			r, err := f.c.ReserveCreation(t.Context(), l, p)
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "release":
				if _, err = f.c.RecordOperationOutcome(t.Context(), r.Permit, "never_dispatched"); err != nil {
					t.Fatal(err)
				}
				again, e := f.c.ReserveCreation(t.Context(), l, p)
				if e != nil || again.Permit != nil || again.Operation.ID != r.Operation.ID || again.Operation.State != "released" {
					t.Fatalf("grant reopened: %+v %v", again, e)
				}
			case "expired":
				f.now = f.now.Add(MCPCreationApprovalTTL)
			case "revoked":
				if err = f.db.RevokeMCPLease(t.Context(), l, "closed"); err != nil {
					t.Fatal(err)
				}
			case "cancelled context":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if _, err = f.c.BeginDispatch(ctx, r.Permit); err == nil {
					t.Fatal("cancelled dispatch admitted")
				}
				return
			}
			if _, err = f.c.BeginDispatch(t.Context(), r.Permit); err == nil {
				t.Fatal("invalid delayed dispatch")
			}
		})
	}
}

func TestMCPCreationDB_ApprovalBoundaryAndAttemptClamp(t *testing.T) {
	for _, clamp := range []bool{false, true} {
		t.Run(fmt.Sprint(clamp), func(t *testing.T) {
			f := newCreationDBFixture(t)
			s, p := f.proposal(t, "api", 278, 1)
			if err := f.c.RetainProposal(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			a := CreationApproval{Repository: p.Repository, SourceTask: p.SourceTask, ProposalHash: p.Hash, CommentID: 1, OwnerID: 10, ActorID: 10, CreatedAt: f.now}
			id, err := f.c.AdmitOwnerIntent(t.Context(), a, "linked")
			if err != nil {
				t.Fatal(err)
			}
			deadline := f.now.Add(MCPCreationApprovalTTL)
			expiry := f.now.Add(time.Hour)
			if clamp {
				expiry = f.now.Add(5 * time.Minute)
				deadline = expiry
			}
			l := f.lease(t, s, "cold", "research", expiry)
			if _, err = f.c.BindGrant(t.Context(), l, id); err != nil {
				t.Fatal(err)
			}
			f.now = deadline.Add(-time.Nanosecond)
			r, err := f.c.ReserveCreation(t.Context(), l, p)
			if err != nil {
				t.Fatal(err)
			}
			f.now = deadline
			if _, err = f.c.ReserveCreation(t.Context(), l, p); err == nil {
				t.Fatal("exact expiry replay accepted")
			}
			if _, err = f.c.BeginDispatch(t.Context(), r.Permit); err == nil {
				t.Fatal("expired dispatch")
			}
			if _, err = f.c.BindGrant(t.Context(), l, id); err == nil {
				t.Fatal("expired grant rebound")
			}
		})
	}
}

func TestMCPCreationDB_ReopenRetainsOperationWithoutAuthority(t *testing.T) {
	f := newCreationDBFixture(t)
	l, p, _ := f.approved(t, "api", 278, 5)
	r, err := f.c.ReserveCreation(t.Context(), l, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.c.BeginDispatch(t.Context(), r.Permit); err != nil {
		t.Fatal(err)
	}
	if _, err = f.c.RecordOperationOutcome(t.Context(), r.Permit, "uncertain"); err != nil {
		t.Fatal(err)
	}
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
	if _, err = recovered.ReserveCreation(t.Context(), l, p); err == nil {
		t.Fatal("reopened stale grant")
	}
	if _, err = recovered.BeginDispatch(t.Context(), r.Permit); err == nil {
		t.Fatal("reopened second dispatch")
	}
	op, err := recovered.GetCreationOperation(t.Context(), r.Operation.ID)
	if err != nil || op.State != "uncertain" || !op.Dispatched {
		t.Fatalf("recovery lost outcome %+v %v", op, err)
	}
	if _, err = recovered.ReconcileOperation(t.Context(), op.ID, "confirmed"); err != nil {
		t.Fatal(err)
	}
}
