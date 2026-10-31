package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func workflowStoreFixture(t *testing.T) (*DB, OwnerAction, WorkflowRequest) {
	t.Helper()
	db, p := ownerActionStoreFixture(t)
	r := WorkflowRequest{OwnerID: p.Principal.OwnerID, OwnerGeneration: p.Principal.OwnerGeneration, Key: "workflow-key", Digest: "digest", ProposalID: p.ID, JobID: "job", Snapshot: `{"definition":{"ref":"test","version":"1"}}`, Inputs: `{"input":"value"}`, Kind: "start", CreatedAt: p.CreatedAt}
	if _, err := db.ReserveWorkflowRequest(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	return db, p, r
}
func admitStoreWorkflow(t *testing.T, db *DB, p OwnerAction) WorkflowAttempt {
	t.Helper()
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil || !ok {
		t.Fatalf("claim action: %v %v", ok, err)
	}
	a, j, err := db.ClaimWorkflowAttempt(t.Context(), p.ConfirmNotBefore, time.Minute)
	if err != nil || j.State != "running" {
		t.Fatalf("claim workflow: %+v %v", j, err)
	}
	return a
}
func TestDurableWorkflowRequestIdentityAndAtomicAdmission(t *testing.T) {
	db, p, r := workflowStoreFixture(t)
	retry := r
	retry.ProposalID = "another-client-proposal"
	retry.JobID = "different-random-id"
	got, err := db.ReserveWorkflowRequest(t.Context(), retry)
	if err != nil || got.ProposalID != p.ID || got.JobID != r.JobID {
		t.Fatalf("retry changed operation: %+v %v", got, err)
	}
	retry.Digest = "changed"
	if _, err = db.ReserveWorkflowRequest(t.Context(), retry); !errors.Is(err, ErrWorkflowConflict) {
		t.Fatalf("changed request: %v", err)
	}
	if _, err = db.ReadWorkflowJob(t.Context(), r.JobID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proposal admitted work: %v", err)
	}
	if _, err = db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	j, err := db.ReadWorkflowJob(t.Context(), r.JobID)
	if err != nil || j.State != "queued" {
		t.Fatalf("no atomic queued job: %+v %v", j, err)
	}
	attempts, err := db.WorkflowAttempts(t.Context(), r.JobID)
	if err != nil || len(attempts) != 1 || attempts[0].ExecutionID != p.ExecutionID {
		t.Fatalf("missing linked admission: %+v %v", attempts, err)
	}
	if _, err = db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	attempts, err = db.WorkflowAttempts(t.Context(), r.JobID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("duplicate confirmation: %+v %v", attempts, err)
	}
}
func TestDurableWorkflowWorkerFenceAndNoticeRetention(t *testing.T) {
	db, p, _ := workflowStoreFixture(t)
	a := admitStoreWorkflow(t, db, p)
	if _, _, err := db.ClaimWorkflowAttempt(t.Context(), a.StartedAt, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second worker: %v", err)
	}
	stale := a
	stale.Fence++
	f := WorkflowFinish{State: "waiting_for_input", Revision: 3, Result: `{"partial":true}`, Questions: `[{"id":"q"}]`, DomainRef: "author-session", Summary: "answer needed", Notice: &WorkflowNotice{Event: "input", Body: "answer needed"}}
	if err := db.FinishWorkflowAttempt(t.Context(), stale, f, a.StartedAt); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("stale fence: %v", err)
	}
	if err := db.FinishWorkflowAttempt(t.Context(), a, f, a.StartedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	j, err := db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil || j.Revision != 3 || j.State != "waiting_for_input" || j.DomainRef != "author-session" {
		t.Fatalf("lost question state: %+v %v", j, err)
	}
	action, err := db.ReadOwnerAction(t.Context(), p.ID)
	if err != nil || action.State != "succeeded" {
		t.Fatalf("attempt did not settle action: %+v %v", action, err)
	}
	notices, err := db.WorkflowNotices(t.Context(), a.JobID)
	if err != nil || len(notices) != 1 || notices[0].State != "unavailable" {
		t.Fatalf("missing unavailable notice: %+v %v", notices, err)
	}
	if err = db.PurgeOwnerActionHistory(t.Context(), a.StartedAt.Add(100*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadOwnerAction(t.Context(), p.ID); err != nil {
		t.Fatalf("purged linked authority: %v", err)
	}
	if err = db.FinishWorkflowAttempt(t.Context(), a, f, a.StartedAt.Add(time.Second)); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("late duplicate output: %v", err)
	}
}
func TestDurableWorkflowConcurrentContinuationAdmission(t *testing.T) {
	db, p, r := workflowStoreFixture(t)
	a := admitStoreWorkflow(t, db, p)
	if err := db.FinishWorkflowAttempt(t.Context(), a, WorkflowFinish{State: "waiting_for_input", Revision: 2, Result: `{}`, Questions: `[{"id":"q"}]`}, a.StartedAt); err != nil {
		t.Fatal(err)
	}
	proposals := make([]OwnerAction, 2)
	for i := range proposals {
		q := p
		q.ID = fmt.Sprintf("answer-%d", i)
		q.ExecutionID = "owner-action:" + q.ID
		q.RequestKey = q.ID
		if err := db.CreateOwnerAction(t.Context(), q); err != nil {
			t.Fatal(err)
		}
		c := r
		c.ProposalID = q.ID
		c.Key = q.ID
		c.Kind = "continue"
		c.ExpectedRevision = 2
		if _, err := db.ReserveWorkflowRequest(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		proposals[i] = q
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, q := range proposals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := db.MutateOwnerAction(t.Context(), q.Principal, q.ID, "confirm", "", q.ConfirmNotBefore)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded, stale := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrWorkflowRevision):
			stale++
		default:
			t.Fatal(err)
		}
	}
	if succeeded != 1 || stale != 1 {
		t.Fatalf("answers admitted: success=%d stale=%d", succeeded, stale)
	}
	attempts, err := db.WorkflowAttempts(t.Context(), r.JobID)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("duplicate work: %+v %v", attempts, err)
	}
	for _, q := range proposals {
		current, e := db.ReadOwnerAction(t.Context(), q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.State == "awaiting_confirmation" {
			if _, e = db.ReadNaturalExecution(t.Context(), q.ExecutionID); !errors.Is(e, ErrNotFound) {
				t.Fatalf("failed admission left journal: %v", e)
			}
		}
	}
}
func TestDurableWorkflowDeadlineAndRecovery(t *testing.T) {
	db, p, _ := workflowStoreFixture(t)
	a := admitStoreWorkflow(t, db, p)
	f := WorkflowFinish{State: "uncertain", Result: `{"partial":"saved"}`, Questions: `[]`, Summary: "runner ended without receipt"}
	if err := db.FinishWorkflowAttempt(t.Context(), a, f, a.Deadline); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("late write accepted: %v", err)
	}
	running, err := db.RunningWorkflowAttempt(t.Context())
	if err != nil || running.ID != a.ID {
		t.Fatalf("lost recovery evidence: %+v %v", running, err)
	}
	if err = db.ReconcileWorkflowAttempt(t.Context(), running, f, a.Deadline.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishWorkflowAttempt(t.Context(), a, f, a.StartedAt); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("shutdown output survived reconciliation: %v", err)
	}
	j, err := db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil || j.State != "uncertain" || j.Result != f.Result {
		t.Fatalf("lost partial result: %+v %v", j, err)
	}
}
func TestDurableWorkflowNoticeClaimsNeverBlindlyReplay(t *testing.T) {
	db, p, _ := workflowStoreFixture(t)
	a := admitStoreWorkflow(t, db, p)
	f := WorkflowFinish{State: "succeeded", Result: `{}`, Questions: `[]`, Notice: &WorkflowNotice{Event: "terminal", Destination: "channel", Body: "complete"}}
	if err := db.FinishWorkflowAttempt(t.Context(), a, f, a.StartedAt); err != nil {
		t.Fatal(err)
	}
	n, err := db.ClaimWorkflowNotice(t.Context(), a.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	retry := a.StartedAt.Add(time.Second)
	if err = db.FinishWorkflowNotice(t.Context(), n.ID, "pending", "", "rate_limited", retry); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimWorkflowNotice(t.Context(), a.StartedAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ignored retry deadline: %v", err)
	}
	if n, err = db.ClaimWorkflowNotice(t.Context(), retry); err != nil {
		t.Fatal(err)
	}
	if err = db.ReconcileWorkflowNotices(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimWorkflowNotice(t.Context(), retry.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ambiguous send replayed: %v", err)
	}
	if err = db.ReconcileWorkflowNoticeReceipt(t.Context(), n.ID, "confirmed-message"); err != nil {
		t.Fatal(err)
	}
	notices, err := db.WorkflowNotices(t.Context(), a.JobID)
	if err != nil || notices[0].State != "delivered" || notices[0].MessageID != "confirmed-message" {
		t.Fatalf("positive receipt lost: %+v %v", notices, err)
	}
}

func TestDurableWorkflowSharedOutboxFencesAmbiguousDelivery(t *testing.T) {
	db, p, _ := workflowStoreFixture(t)
	a := admitStoreWorkflow(t, db, p)
	if err := db.FinishWorkflowAttempt(t.Context(), a, WorkflowFinish{State: "succeeded", Result: `{}`, Questions: `[]`, Notice: &WorkflowNotice{Event: "terminal", Destination: "channel", Body: "saved result"}}, a.StartedAt); err != nil {
		t.Fatal(err)
	}
	n, err := db.ClaimChatNotice(t.Context(), "discord", a.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimChatNotice(t.Context(), "discord", a.StartedAt.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outbox replayed sent workflow notice: %v", err)
	}
	next := a.StartedAt.Add(time.Second)
	if err = db.FinishWorkflowChatNotice(t.Context(), n, "pending", "", "rate_limited", next, a.StartedAt); err != nil {
		t.Fatal(err)
	}
	n, err = db.ClaimChatNotice(t.Context(), "discord", next)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishWorkflowChatNotice(t.Context(), n, "failed", "", "delivery_unconfirmed", time.Time{}, next); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimChatNotice(t.Context(), "discord", next.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outbox replayed ambiguous workflow notice: %v", err)
	}
	notices, err := db.WorkflowNotices(t.Context(), a.JobID)
	if err != nil || len(notices) != 1 || notices[0].State != "failed" {
		t.Fatalf("lost retained delivery failure: %+v %v", notices, err)
	}
	j, err := db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil || j.State != "succeeded" {
		t.Fatalf("delivery changed job: %+v %v", j, err)
	}
}

func TestDurableWorkflowEffectsRequireActiveAttemptWithinDeadline(t *testing.T) {
	db, p, _ := workflowStoreFixture(t)
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	check := func(at time.Time) error {
		return db.withTx(t.Context(), func(tx *sql.Tx) error { return ownerActionEffectAuthority(t.Context(), tx, p.ExecutionID, at) })
	}
	if err := check(p.ConfirmNotBefore); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("queued effect accepted: %v", err)
	}
	a, _, err := db.ClaimWorkflowAttempt(t.Context(), p.ConfirmNotBefore, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = check(a.StartedAt); err != nil {
		t.Fatalf("active authority rejected: %v", err)
	}
	deadline := a.StartedAt.Add(10 * time.Second)
	if err = db.SetWorkflowAttemptDeadline(t.Context(), a, deadline); err != nil {
		t.Fatal(err)
	}
	if err = check(deadline); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("late effect accepted: %v", err)
	}
	a.Deadline = deadline
	if err = db.ReconcileWorkflowAttempt(t.Context(), a, WorkflowFinish{State: "uncertain", Result: `{}`, Questions: `[]`}, deadline); err != nil {
		t.Fatal(err)
	}
	if err = check(a.StartedAt); err == nil {
		t.Fatal("stale worker effect survived reconciliation")
	}
}

func TestDurableWorkflowMigrationPreservesExistingOwnerActions(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var addition migration
	var prior []migration
	for _, m := range migrations {
		if m.name == "durable_workflows" {
			addition = m
		} else {
			prior = append(prior, m)
		}
	}
	if addition.version == 0 {
		t.Fatal("durable workflow migration absent")
	}
	if err = db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	owner := connectorTestOwner(t, db, 42)
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	p := OwnerAction{ID: "old", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "channel", DiscordOwnerID: "owner"}, RequestKey: "old", RequestDigest: "digest", Facts: `{"old":true}`, Payload: `{"old":true}`, Digest: "digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:old"}
	if err = db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err = db.applyMigration(t.Context(), addition); err != nil {
		t.Fatal(err)
	}
	got, err := db.ReadOwnerAction(t.Context(), p.ID)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("migration changed existing record: %+v %v", got, err)
	}
	if _, err = db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatalf("old action admission broken: %v", err)
	}
	if _, err = db.RunningWorkflowAttempt(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old action became workflow: %v", err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDurableWorkflowRejectedDispatchSettlesQueuedJobAndRetainsResults(t *testing.T) {
	for _, continuation := range []bool{false, true} {
		t.Run(fmt.Sprint(continuation), func(t *testing.T) {
			db, p, r := workflowStoreFixture(t)
			if continuation {
				a := admitStoreWorkflow(t, db, p)
				if err := db.FinishWorkflowAttempt(t.Context(), a, WorkflowFinish{State: "waiting_for_input", Revision: 1, Result: `{"state":"waiting_for_input","domain_ref":"author","session_revision":7,"items":[{"draft":"saved"}],"questions":[{"id":"q"}]}`, Questions: `[{"id":"q"}]`, DomainRef: "author"}, a.StartedAt); err != nil {
					t.Fatal(err)
				}
				p.ID = "continuation"
				p.RequestKey = p.ID
				p.ExecutionID = "owner-action:" + p.ID
				if err := db.CreateOwnerAction(t.Context(), p); err != nil {
					t.Fatal(err)
				}
				r.ProposalID = p.ID
				r.Key = p.ID
				r.Kind = "continue"
				r.ExpectedRevision = 1
				if _, err := db.ReserveWorkflowRequest(t.Context(), r); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
				t.Fatal(err)
			}
			if err := db.FinishOwnerAction(t.Context(), p.ID, "failed", `{"state":"failed","summary":"Authority was revoked before execution"}`, "authority_revoked", p.ConfirmNotBefore); err != nil {
				t.Fatal(err)
			}
			job, err := db.ReadWorkflowJob(t.Context(), r.JobID)
			if err != nil || job.State != "failed" {
				t.Fatalf("queued job orphaned: %+v %v", job, err)
			}
			attempts, err := db.WorkflowAttempts(t.Context(), r.JobID)
			if err != nil || attempts[len(attempts)-1].State != "failed" {
				t.Fatalf("queued attempt orphaned: %+v %v", attempts, err)
			}
			if continuation {
				var projection map[string]any
				if err = json.Unmarshal([]byte(job.Result), &projection); err != nil {
					t.Fatal(err)
				}
				items, itemsOK := projection["items"].([]any)
				revision, revisionOK := projection["session_revision"].(float64)
				if job.DomainRef != "author" || !itemsOK || len(items) != 1 || !revisionOK || revision != 7 || job.Questions != `[{"id":"q"}]` {
					t.Fatalf("previous evidence lost: %+v", job)
				}
			}
			if _, _, err = db.ClaimWorkflowAttempt(t.Context(), p.ConfirmNotBefore, time.Minute); !errors.Is(err, ErrNotFound) {
				t.Fatalf("refused dispatch became executable: %v", err)
			}
			notices, err := db.WorkflowNotices(t.Context(), r.JobID)
			if err != nil || len(notices) == 0 || notices[len(notices)-1].State != "unavailable" {
				t.Fatalf("missing failure event: %+v %v", notices, err)
			}
		})
	}
}

func TestDurableWorkflowUnreservedProposalCannotConfirm(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	p.ID = "unreserved-workflow"
	p.RequestKey = p.ID
	p.ExecutionID = "owner-action:" + p.ID
	p.Facts = `{"invocation":{"command":"workflow-analysis"}}`
	if err := db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); !errors.Is(err, ErrWorkflowRevision) {
		t.Fatalf("unreserved workflow admitted: %v", err)
	}
	got, err := db.ReadOwnerAction(t.Context(), p.ID)
	if err != nil || got.State != "awaiting_confirmation" {
		t.Fatalf("confirmation was not rolled back: %+v %v", got, err)
	}
	if _, err = db.ReadNaturalExecution(t.Context(), p.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unreserved execution exists: %v", err)
	}
}

func TestDurableWorkflowFIFOHeartbeatAndBoundedHistory(t *testing.T) {
	db, p, r := workflowStoreFixture(t)
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	second := p
	second.ID = "second"
	second.RequestKey = second.ID
	second.ExecutionID = "owner-action:" + second.ID
	if err := db.CreateOwnerAction(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	next := r
	next.Key = second.ID
	next.ProposalID = second.ID
	next.JobID = "second-job"
	if _, err := db.ReserveWorkflowRequest(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), second.Principal, second.ID, "confirm", "", second.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOwnerActionExecution(t.Context(), second.ID, second.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ClaimWorkflowAttempt(t.Context(), p.ConfirmNotBefore, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("later job overtook undispatched first job: %v", err)
	}
	if _, err := db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	a, _, err := db.ClaimWorkflowAttempt(t.Context(), p.ConfirmNotBefore, time.Minute)
	if err != nil || a.JobID != r.JobID {
		t.Fatalf("FIFO: %+v %v", a, err)
	}
	if err = db.HeartbeatWorkflowAttempt(t.Context(), a, a.StartedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err := db.RunningWorkflowAttempt(t.Context())
	if err != nil || !current.Deadline.Equal(a.Deadline) {
		t.Fatalf("heartbeat extended deadline: %+v %v", current, err)
	}
	stale := a
	stale.Fence++
	if err = db.HeartbeatWorkflowAttempt(t.Context(), stale, a.StartedAt); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("stale heartbeat accepted: %v", err)
	}
	if err = db.HeartbeatWorkflowAttempt(t.Context(), a, a.Deadline); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("late heartbeat accepted: %v", err)
	}
	if err = db.FinishWorkflowAttempt(t.Context(), a, WorkflowFinish{State: "succeeded", Revision: 1, Result: `{}`, Questions: `[]`, Notice: &WorkflowNotice{Event: "terminal", Body: "complete"}}, a.StartedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.HeartbeatWorkflowAttempt(t.Context(), a, a.StartedAt); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("finished heartbeat accepted: %v", err)
	}
	attempts, err := db.WorkflowRecentAttempts(t.Context(), r.JobID, 1)
	if err != nil || len(attempts) != 1 || attempts[0].ID != a.ID {
		t.Fatalf("bounded attempts: %+v %v", attempts, err)
	}
	eventAttempt, err := db.WorkflowAttemptAtRevision(t.Context(), r.JobID, 1)
	if err != nil || eventAttempt.ID != a.ID {
		t.Fatalf("event attempt: %+v %v", eventAttempt, err)
	}
	notices, err := db.WorkflowRecentNotices(t.Context(), r.JobID, 1)
	if err != nil || len(notices) != 1 {
		t.Fatalf("bounded notices: %+v %v", notices, err)
	}
	notice, err := db.ReadWorkflowNotice(t.Context(), notices[0].ID)
	if err != nil || notice.JobID != r.JobID {
		t.Fatalf("notice read: %+v %v", notice, err)
	}
}
