package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func stageDecisionFixture(t *testing.T) (*DB, StageDecisionTarget, OwnerAction, context.Context, time.Time) {
	t.Helper()
	db, in := chatResearchFixture(t)
	session, err := db.GetSessionByTaskRef(t.Context(), in.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := db.CurrentTaskAdvisoryGuard(t.Context(), in.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	target := StageDecisionTarget{TaskRef: in.TaskRef, RepositoryID: session.RepoID, RepositoryRef: "github:acme/research", SessionID: session.ID, InstanceID: in.InstanceID, Workflow: "research", Stage: "shaping", Operation: "revise_task", Guard: guard, Revision: guard, WaitKey: in.DeliveryKey, QuestionID: in.QuestionCommentID, SourceNode: "shape", Effect: "Admit revision guidance"}
	p, ctx, at := admitStageDecisionFixture(t, db, target, in.At)
	return db, target, p, ctx, at
}

func admitStageDecisionFixture(t *testing.T, db *DB, target StageDecisionTarget, at time.Time) (OwnerAction, context.Context, time.Time) {
	t.Helper()
	owner := connectorTestOwner(t, db, 7)
	facts, _ := json.Marshal(struct {
		TargetID       string `json:"target_id"`
		TargetRevision string `json:"target_revision"`
		Invocation     struct {
			Command string `json:"command"`
		} `json:"invocation"`
	}{TargetID: target.TaskRef, TargetRevision: target.Revision, Invocation: struct {
		Command string `json:"command"`
	}{Command: target.Operation}})
	payload, _ := json.Marshal(struct {
		Target  StageDecisionTarget `json:"target"`
		Request struct {
			Change string `json:"change"`
		} `json:"request"`
	}{Target: target, Request: struct {
		Change string `json:"change"`
	}{Change: "done"}})
	p := OwnerAction{ID: "stage", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "7", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "200", DiscordOwnerID: "100"}, RequestKey: "stage-key", RequestDigest: "stage-request", Facts: string(facts), Payload: string(payload), Digest: "stage-digest", Scope: "repository:actions", RepositoryID: target.RepositoryID, RepositoryRef: target.RepositoryRef, Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:stage"}
	if target.Operation == "approve_stage" {
		p.Tier = "binding"
		p.ApprovalChannel = "200"
	}
	if err := db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	at = p.ConfirmNotBefore
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", at); err != nil {
		t.Fatal(err)
	}
	if p.Tier == "binding" {
		if ok, e := db.ClaimOwnerActionDelivery(t.Context(), p.ID, at); e != nil || !ok {
			t.Fatalf("delivery claim: %v %v", ok, e)
		}
		if e := db.FinishOwnerActionDelivery(t.Context(), p.ID, "confirmed-message", "", at); e != nil {
			t.Fatal(e)
		}
		if _, e := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "reaction", `{"message":"confirmed-message","emoji":"✅"}`, at); e != nil {
			t.Fatal(e)
		}
	}
	if ok, e := db.ClaimOwnerActionExecution(t.Context(), p.ID, at); e != nil || !ok {
		t.Fatalf("claim: %v %v", ok, e)
	}
	ctx := WithOwnerActionEffect(t.Context(), p.ExecutionID, func() time.Time { return at })
	return p, ctx, at
}

func TestStageDecisionAtomicReceiptAndNoApprovalFromRevision(t *testing.T) {
	db, target, p, ctx, at := stageDecisionFixture(t)
	receipt, err := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at)
	if err != nil || receipt.State != "applied" || receipt.ContinuationRef == "" {
		t.Fatalf("apply: %+v %v", receipt, err)
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), target.InstanceID)
	if err != nil || wait.State != "claimed" || wait.ReplyBody == "done" || wait.ReplySource != "chat" {
		t.Fatalf("revision became approval: %+v %v", wait, err)
	}
	again, err := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at)
	if err != nil || again != receipt {
		t.Fatalf("replay receipt: %+v %v", again, err)
	}
	if recovered, ok, e := db.RecoverStageDecision(ctx, p.ExecutionID, at); e != nil || !ok || recovered != receipt {
		t.Fatalf("recover: %+v %v %v", recovered, ok, e)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM graph_dispatches WHERE instance_id=?`, target.InstanceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate dispatch: %d %v", count, err)
	}
}

func TestStageDecisionMissingReceiptFencesSurvivingExecutor(t *testing.T) {
	db, target, p, ctx, at := stageDecisionFixture(t)
	receipt, ok, err := db.RecoverStageDecision(ctx, p.ExecutionID, at)
	if err != nil || !ok || receipt.State != "not_applied" || receipt.TaskRef != target.TaskRef || receipt.Operation != target.Operation {
		t.Fatalf("recovery: %+v %v %v", receipt, ok, err)
	}
	late, err := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at)
	if err != nil || late.State != "not_applied" {
		t.Fatalf("late executor: %+v %v", late, err)
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), target.InstanceID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("late mutation: %+v %v", wait, err)
	}
}

func TestStageDecisionRecoveryRacesAtomicCommit(t *testing.T) {
	for range 6 {
		db, target, p, ctx, at := stageDecisionFixture(t)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var results [2]StageDecisionReceipt
		var errs [2]error
		wg.Go(func() { <-start; results[0], errs[0] = db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at) })
		wg.Go(func() { <-start; results[1], _, errs[1] = db.RecoverStageDecision(ctx, p.ExecutionID, at) })
		close(start)
		wg.Wait()
		if errs[0] != nil || errs[1] != nil || results[0] != results[1] {
			t.Fatalf("race: %+v %v", results, errs)
		}
		wait, e := db.GetGraphOwnerWait(t.Context(), target.InstanceID)
		if e != nil {
			t.Fatal(e)
		}
		if (results[0].State == "applied") != (wait.State == "claimed") {
			t.Fatalf("receipt disagrees with state: %+v %+v", results[0], wait)
		}
	}
}

func TestStageDecisionStaleGuardRefusesWithoutMutation(t *testing.T) {
	db, target, p, ctx, at := stageDecisionFixture(t)
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_owner_waits SET state='claimed',reply_id=103 WHERE delivery_key=?`, target.WaitKey); err != nil {
		t.Fatal(err)
	}
	receipt, err := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at)
	if err != nil || receipt.State != "refused" {
		t.Fatalf("stale: %+v %v", receipt, err)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM graph_dispatches`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale dispatch: %d %v", count, err)
	}
}

func TestStageDecisionAuthorityRefusalIsAtomic(t *testing.T) {
	db, target, p, ctx, at := stageDecisionFixture(t)
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE connector_owner SET generation=generation+1`); err != nil {
		t.Fatal(err)
	}
	_, err := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at)
	if err == nil {
		t.Fatal("revoked owner applied a decision")
	}
	if _, err = db.GetStageDecisionReceipt(t.Context(), p.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("false receipt: %v", err)
	}
	wait, e := db.GetGraphOwnerWait(t.Context(), target.InstanceID)
	if e != nil || wait.State != "waiting" {
		t.Fatalf("revoked mutation: %+v %v", wait, e)
	}
}

func TestStageDecisionRecoveryFenceAcrossIndependentConnections(t *testing.T) {
	for range 4 {
		db, target, p, ctx, at := stageDecisionFixture(t)
		other, err := Open(t.Context(), db.Path())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		var mutationErr, recoveryErr error
		wg.Go(func() { <-start; _, mutationErr = db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at) })
		wg.Go(func() { <-start; _, _, recoveryErr = other.RecoverStageDecision(ctx, p.ExecutionID, at) })
		close(start)
		wg.Wait()
		// Independent SQLite snapshots can return BUSY rather than wait for another
		// writer. Reconciliation reads the winning durable receipt after both finish.
		receipt, found, reconcileErr := other.RecoverStageDecision(ctx, p.ExecutionID, at)
		if reconcileErr != nil || !found {
			t.Fatalf("reconcile after contention: %v (apply %v, recover %v)", reconcileErr, mutationErr, recoveryErr)
		}
		late, lateErr := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at)
		if lateErr != nil || late != receipt {
			t.Fatalf("late mutation crossed durable fence: %+v %+v %v", receipt, late, lateErr)
		}
		wait, waitErr := other.GetGraphOwnerWait(t.Context(), target.InstanceID)
		if waitErr != nil || (receipt.State == "applied") != (wait.State == "claimed") {
			t.Fatalf("cross-connection state: %+v %+v %v", receipt, wait, waitErr)
		}
		if err = other.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStageDecisionEffectUsesCurrentClock(t *testing.T) {
	db, target, p, _, at := stageDecisionFixture(t)
	expired := WithOwnerActionEffect(t.Context(), p.ExecutionID, func() time.Time { return at.Add(time.Hour) })
	if _, err := db.ApplyStageDecision(expired, target, "done", p.ExecutionID, at); err == nil {
		t.Fatal("captured clock bypassed current expiry")
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), target.InstanceID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("expired mutation: %+v %v", wait, err)
	}
}

func TestStageDecisionUnresolvedEffectBlocksNewRequestUntilRecovery(t *testing.T) {
	db, target, p, ctx, at := stageDecisionFixture(t)
	captureDecisionProfile(t, db, target.InstanceID, decisionResearchProfile)
	if _, err := db.ResolveStageDecisionTarget(t.Context(), target.TaskRef, target.Stage, target.Operation); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("unresolved proposal accepted: %v", err)
	}
	fresh := p
	fresh.ID = "fresh"
	fresh.ExecutionID = "owner-action:fresh"
	fresh.RequestKey = "fresh"
	if err := db.CreateOwnerAction(t.Context(), fresh); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("unresolved new key created: %v", err)
	}
	if _, _, err := db.RecoverStageDecision(ctx, p.ExecutionID, at); err != nil {
		t.Fatal(err)
	}
	current, err := db.ResolveStageDecisionTarget(t.Context(), target.TaskRef, target.Stage, target.Operation)
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err = json.Unmarshal([]byte(fresh.Facts), &facts); err != nil {
		t.Fatal(err)
	}
	facts["target_revision"] = current.Revision
	encoded, _ := json.Marshal(facts)
	fresh.Facts = string(encoded)
	var payload map[string]any
	if err = json.Unmarshal([]byte(fresh.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	payload["target"] = current
	encoded, _ = json.Marshal(payload)
	fresh.Payload = string(encoded)
	if err = db.CreateOwnerAction(t.Context(), fresh); err != nil {
		t.Fatalf("proven no-effect stranded fresh request: %v", err)
	}
	if _, err = db.MutateOwnerAction(t.Context(), fresh.Principal, fresh.ID, "confirm", "", at); err != nil {
		t.Fatal(err)
	}
	if ok, e := db.ClaimOwnerActionExecution(t.Context(), fresh.ID, at); e != nil || !ok {
		t.Fatalf("fresh claim: %v %v", ok, e)
	}
	freshCtx := WithOwnerActionEffect(t.Context(), fresh.ExecutionID, func() time.Time { return at })
	if receipt, e := db.ApplyStageDecision(freshCtx, current, "done", fresh.ExecutionID, at); e != nil || receipt.State != "applied" {
		t.Fatalf("fresh unchanged target not applied: %+v %v", receipt, e)
	}
	if receipt, e := db.ApplyStageDecision(ctx, target, "done", p.ExecutionID, at); e != nil || receipt.State != "not_applied" {
		t.Fatalf("old execution replayed after fresh request: %+v %v", receipt, e)
	}
}
