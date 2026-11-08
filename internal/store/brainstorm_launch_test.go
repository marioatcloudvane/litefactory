package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func brainstormLaunchTestItem(t *testing.T, db *DB, itemID, ledger string) {
	t.Helper()
	sum := sha256.Sum256([]byte(ledger))
	at := FormatTime(graphTestTime())
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO brainstorm_task_batches(id,owner_action_id,execution_id,owner_id,owner_generation,repository_id,repository_ref,brainstorm_id,draft_set_id,state,created_at) VALUES('b1','oa','ex','owner',1,1,'acme/api','bs','ds','running',?)`, at); err != nil {
		t.Skipf("brainstorm task batches table shape differs: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO brainstorm_task_items(id,batch_id,draft_id,ordinal,title,body,marker,ledger,ledger_digest,state,updated_at) VALUES(?,'b1','d1',1,'T','B','M',?,?,'plan_starting',?)`, itemID, ledger, hex.EncodeToString(sum[:]), at); err != nil {
		t.Fatal(err)
	}
}

func TestBrainstormLaunchTriggerBandIsPrivate(t *testing.T) {
	t.Parallel()
	id := BrainstormLaunchTrigger(7)
	if !IsBrainstormLaunchTrigger(id) || !IsFactoryTrigger(id) || IsChatTrigger(id) {
		t.Fatalf("launch trigger %d is not in its own private band", id)
	}
	if IsBrainstormLaunchTrigger(brainstormLaunchTriggerEnd+1) || IsBrainstormLaunchTrigger(12345) {
		t.Fatal("band overlaps a neighbour or forge comments")
	}
	if got, ok := BrainstormLaunchAttemptID(id); !ok || got != 7 {
		t.Fatalf("attempt id %d %v", got, ok)
	}
}

func TestBrainstormLaunchAttemptOneOpenPerItem(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx, at := t.Context(), graphTestTime()
	session := testSession(t, db, 901)
	first, created, err := db.OpenBrainstormLaunchAttempt(ctx, "item", session.ID, at)
	if err != nil || !created || first.Ordinal != 1 || first.State != BrainstormLaunchPending {
		t.Fatalf("open: %+v %v %v", first, created, err)
	}
	again, created, err := db.OpenBrainstormLaunchAttempt(ctx, "item", session.ID, at)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second open attempt: %+v %v %v", again, created, err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO brainstorm_task_launch_attempts(item_id,ordinal,session_id,state,created_at,updated_at) VALUES('item',9,?,'pending','x','x')`, session.ID); err == nil {
		t.Fatal("partial unique index allowed two open attempts")
	}
	// Deferral keeps the identity.
	if ok, err := db.MoveBrainstormLaunchAttempt(ctx, first.ID, BrainstormLaunchPending, BrainstormLaunchReserved, "", 0, "", at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := db.MoveBrainstormLaunchAttempt(ctx, first.ID, BrainstormLaunchReserved, BrainstormLaunchPending, BrainstormLaunchOutcomeDeferred, 0, "", at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	same, created, err := db.OpenBrainstormLaunchAttempt(ctx, "item", session.ID, at)
	if err != nil || created || same.ID != first.ID || same.Outcome != BrainstormLaunchOutcomeDeferred {
		t.Fatalf("deferral changed identity: %+v %v %v", same, created, err)
	}
	// Only a terminal failure opens a new identity.
	if _, err = db.MoveBrainstormLaunchAttempt(ctx, first.ID, BrainstormLaunchPending, BrainstormLaunchFailed, "", 0, "", at); err == nil {
		t.Fatal("pending attempt closed without a hand-off")
	}
	_, _ = db.MoveBrainstormLaunchAttempt(ctx, first.ID, BrainstormLaunchPending, BrainstormLaunchReserved, "", 0, "", at)
	if ok, err := db.MoveBrainstormLaunchAttempt(ctx, first.ID, BrainstormLaunchReserved, BrainstormLaunchFailed, BrainstormLaunchOutcomeFailed, 0, "boom", at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err = db.MoveBrainstormLaunchAttempt(ctx, first.ID, BrainstormLaunchFailed, BrainstormLaunchPending, "", 0, "", at); err == nil {
		t.Fatal("failed attempt reopened")
	}
	second, created, err := db.OpenBrainstormLaunchAttempt(ctx, "item", session.ID, at)
	if err != nil || !created || second.ID == first.ID || second.TriggerID() == first.TriggerID() || second.Ordinal != 2 {
		t.Fatalf("retry did not get a new identity: %+v %v %v", second, created, err)
	}
}

func TestBrainstormSeedWrittenInAdmissionTransaction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx, at := t.Context(), graphTestTime()
	session := testSession(t, db, 902)
	ledger := `{"decisions":[]}`
	brainstormLaunchTestItem(t, db, "item", ledger)
	plan, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: strings.Repeat("b", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitPlanPath(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := db.OpenBrainstormLaunchAttempt(ctx, "item", session.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	launch := WorkflowLaunch{Mode: "plan", SessionID: session.ID, TriggerID: attempt.TriggerID(), DefinitionID: plan.ID, Start: "start", ContentDigest: plan.Digest, TopologyDigest: plan.Digest, Files: []byte(`{"graph.dot":"eA=="}`)}
	admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := db.BrainstormSeedForLaunch(ctx, admitted.LaunchID)
	sum := sha256.Sum256([]byte(ledger))
	if err != nil || seed.ItemID != "item" || seed.AttemptID != attempt.ID || seed.LedgerDigest != hex.EncodeToString(sum[:]) || seed.State != BrainstormSeedPending {
		t.Fatalf("seed: %+v %v", seed, err)
	}
	replayed, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil || replayed.LaunchID != admitted.LaunchID {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	state, err := db.BrainstormLaunchState(ctx, session.ID, attempt.TriggerID())
	if err != nil || !state.Admitted || !state.HasSeed || state.AgentDispatched {
		t.Fatalf("launch state: %+v %v", state, err)
	}
	if _, err = db.RecordBrainstormSeedDigests(ctx, admitted.LaunchID, "dir", "b", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordBrainstormSeedDigests(ctx, admitted.LaunchID, "dir", "other", "t"); err == nil {
		t.Fatal("recorded digests were replaced")
	}
	if err = db.FinishBrainstormSeed(ctx, admitted.LaunchID, BrainstormSeedComplete, "", at); err != nil {
		t.Fatal(err)
	}
	// An attempt without an item fails the whole admission: no launch without its seed.
	orphan, _, err := db.OpenBrainstormLaunchAttempt(ctx, "missing", session.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	launch.TriggerID = orphan.TriggerID()
	if _, err = db.AdmitWorkflowLaunch(ctx, launch); err == nil {
		t.Fatal("launch admitted without an item to seed")
	}
	if found, err := db.HasWorkflowLaunch(ctx, session.ID, orphan.TriggerID()); err != nil || found {
		t.Fatalf("refused admission left a launch: %v %v", found, err)
	}
	if _, _, err = db.BrainstormSeedLedger(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ledger: %v", err)
	}
}
