package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func agentResultFixture(t *testing.T, mode string) (*store.DB, store.Session, AgentResultServiceOptions) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "results.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "example", Name: "results", RemoteURL: "git@github.com:example/results.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 263)
	if err != nil {
		t.Fatal(err)
	}
	a := agentResultRun(t, db, session, mode, "attempt-1", 1)
	return db, session, AgentResultServiceOptions{DB: db, Attempt: a, Resolve: func(context.Context) (ar.Context, error) {
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: mode, QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}}, nil
	}, ReadTargets: ar.EditTargets(mode), EditTargets: ar.EditTargets(mode)}
}
func agentResultRun(t *testing.T, db *store.DB, session store.Session, mode, attempt string, trigger int64) store.ResultAttempt {
	t.Helper()
	seg, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "result", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: mode, Agent: store.AgentClaude, TriggerCommentID: trigger, LogPath: func(n int) string { return fmt.Sprintf("run-%d.log", n) }})
	if err != nil {
		t.Fatal(err)
	}
	return store.ResultAttempt{Task: session.TaskRef, Mode: mode, WorkContext: "owner-approved-work", RunID: run.ID, Attempt: attempt, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}
}
func resultCreate(mode string) []byte {
	return []byte(fmt.Sprintf(`{"result":{"outcome":"incomplete","reason":"Further work remains","comment":"Initial result","payload":{"%s":%s}}}`, mode, map[string]string{"research": `{"request":"Inspect boundary"}`, "plan": `{"stories":[{"key":"US-001","title":"Draft","user_story":{"want":"consistent results"}}]}`, "implement": `{}`, "validate": `{}`, "spike": `{"request":"Explore boundary"}`}[mode]))
}
func TestAgentResultService_PreflightSubmissionAndContinuation(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			db, session, o := agentResultFixture(t, mode)
			service, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			before, err := db.GetRun(t.Context(), o.Attempt.RunID)
			if err != nil {
				t.Fatal(err)
			}
			data := resultCreate(mode)
			d, err := service.Preflight(t.Context(), data)
			if err != nil || !d.Valid {
				t.Fatalf("preflight: %+v %v", d, err)
			}
			revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
			if err != nil || len(revisions) != 0 {
				t.Fatal("preflight allocated revision")
			}
			if _, err = db.GetResultReceipt(t.Context(), service.Binding(), o.Attempt.RunID, "create"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("preflight allocated receipt")
			}
			response, err := service.Submit(t.Context(), "create", data)
			if err != nil || !response.Validation.Valid || response.Receipt == nil || response.Snapshot == nil {
				t.Fatalf("submission: %+v %v", response, err)
			}
			if response.Receipt.Latest.Input != "accepted" || response.Receipt.Latest.Engineering != "pending" || response.Receipt.Latest.Acceptance != "pending" || response.Receipt.Latest.Publication != "pending" {
				t.Fatal("input acceptance confused with workflow decisions")
			}
			if response.Snapshot.Meta.RunID != o.Attempt.RunID || response.Snapshot.Meta.ResultRevision != 1 || response.Snapshot.Meta.RequestID != "create" {
				t.Fatal("factory metadata not bound")
			}
			p, diag, err := service.Read(t.Context(), 0, []ar.Target{{Field: "comment"}})
			if err != nil || !diag.Valid || p.CompleteSnapshot || p.Revision != 1 {
				t.Fatal("scoped read not revision-bound")
			}
			snapshot, err := db.GetResultRevision(t.Context(), service.Binding(), 1)
			if err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(snapshot.Snapshot)
			o.Attempt = agentResultRun(t, db, session, mode, "attempt-2", 2)
			fresh, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Continuation"}]}`)
			next, err := fresh.Submit(t.Context(), "update", update)
			if err != nil || next.Snapshot == nil || next.Snapshot.Meta.ResultRevision != 2 || next.Snapshot.Meta.RunID != o.Attempt.RunID || next.Snapshot.Meta.AttemptID != "attempt-2" {
				t.Fatalf("continuation attribution: %+v %v", next, err)
			}
			replay, err := service.Submit(t.Context(), "create", data)
			if err != nil || replay.Snapshot == nil || replay.Snapshot.Meta.ResultRevision != 1 {
				t.Fatal("revoked exact retry lost original application", err)
			}
			if _, err = service.Submit(t.Context(), "forbidden", update); !errors.Is(err, store.ErrResultAuthority) {
				t.Fatal("revoked new submission accepted", err)
			}
			if _, err = service.Submit(t.Context(), "create", append(bytes.Clone(data), ' ')); !errors.Is(err, store.ErrResultCorrelation) {
				t.Fatal("changed bytes reused correlation", err)
			}
			old, err := db.GetResultRevision(t.Context(), fresh.Binding(), 1)
			if err != nil || !bytes.Equal(original, old.Snapshot) {
				t.Fatal("history overwritten")
			}
			after, err := db.GetRun(t.Context(), before.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("application result altered immutable run/report", err)
			}
		})
	}
}
func TestAgentResultService_CorrectionStaleAndNoWritePreflight(t *testing.T) {
	db, _, o := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	bad := []byte(`{"result":{"outcome":"completed","comment":"","payload":{"research":{"request":"Inspect"}}}}`)
	rejected, err := service.Submit(t.Context(), "bad", bad)
	if err != nil || rejected.Validation.Valid || rejected.Receipt == nil || rejected.Receipt.InputState != "rejected" || rejected.Snapshot != nil {
		t.Fatal("invalid result became applied", err)
	}
	data := resultCreate("research")
	if _, err = service.Submit(t.Context(), "bad", data); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatal("correction reused old correlation")
	}
	accepted, err := service.Submit(t.Context(), "corrected", data)
	if err != nil || accepted.Snapshot == nil {
		t.Fatal(err)
	}
	receiptBefore, err := service.Receipt(t.Context(), service.Binding().RunID, "corrected")
	if err != nil {
		t.Fatal(err)
	}
	invalidMerge := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"}]}`)
	for range 2 {
		d, err := service.Preflight(t.Context(), invalidMerge)
		if err != nil || d.Valid {
			t.Fatal("invalid complete merge passed preflight")
		}
	}
	receiptAfter, err := service.Receipt(t.Context(), service.Binding().RunID, "corrected")
	if err != nil || !reflect.DeepEqual(receiptBefore, receiptAfter) {
		t.Fatal("preflight changed durable receipt")
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	if err != nil || !reflect.DeepEqual(revisions, []int64{1}) {
		t.Fatal("preflight changed history")
	}
	update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"next"}]}`)
	if _, err = service.Submit(t.Context(), "next", update); err != nil {
		t.Fatal(err)
	}
	stale, err := service.Submit(t.Context(), "stale", update)
	if err != nil || stale.Validation.Valid || stale.Validation.CurrentRevision != 2 || stale.Validation.ExpectedRevision != 1 || stale.Receipt != nil {
		t.Fatal("stale update was recorded", err)
	}
	if _, err = db.GetResultReceipt(t.Context(), service.Binding(), service.Binding().RunID, "stale"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("stale update allocated request")
	}
}
func TestAgentResultService_EvidenceReadsAndFrozenScopeRefresh(t *testing.T) {
	f, _, legacy, options := submissionSessionFixture(t, 2)
	contribution := capturedContribution(t, options.Worktree, "US-1", "engineering")
	result := callContribution(t, legacy, "submit", contribution)
	var priorReceipt store.SubmissionReceipt
	if err := json.Unmarshal(result.Result, &priorReceipt); err != nil {
		t.Fatal(err)
	}
	if priorReceipt.InputState != "valid" || priorReceipt.EngineeringState != "recorded" {
		t.Fatalf("fixture contribution not recorded: %+v", priorReceipt)
	}
	reference := contribution
	reference.Capture = nil
	referenceBytes, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	missing := false
	changedRevision := false
	resolver := func(ctx context.Context) (ar.Context, error) {
		reads++
		scope := legacy.scopeForContribution(ctx, referenceBytes)
		story := scope.Stories["US-1"]
		if missing {
			story.SnapshotRetained = false
		}
		if changedRevision {
			story.Revision = agent.StoryDigest([]byte("new requirement"))
		}
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "implement", Stories: map[string]agent.ContributionStory{"US-1": story}}, nil
	}
	service, err := BindAgentResultService(t.Context(), AgentResultServiceOptions{DB: f.db, Attempt: store.ResultAttempt{Task: f.session.TaskRef, Mode: "implement", WorkContext: "frozen", RunID: options.RunID, Attempt: "result-attempt", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: resolver, ReadTargets: ar.EditTargets("implement"), EditTargets: ar.EditTargets("implement")})
	if err != nil {
		t.Fatal(err)
	}
	authored, err := json.Marshal(ar.CreateInput{Result: ar.Result{Outcome: "completed", Comment: "Retained engineering", Payload: ar.Payload{Implement: &ar.Implement{Stories: []ar.ImplementationStory{{Story: "US-1", Outcome: "engineering_complete", Engineering: reference.Engineering}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.db.GetSubmissionSnapshot(t.Context(), options.RunID, reference.Engineering.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := service.Preflight(t.Context(), authored)
	if err != nil || !d.Valid {
		t.Fatalf("retained evidence preflight: %+v %v", d, err)
	}
	missing = true
	rejected, err := service.Submit(t.Context(), "stale-evidence", authored)
	if err != nil || rejected.Validation.Valid || rejected.Snapshot != nil || reads < 3 {
		t.Fatal("submit reused preflight evidence without fresh reads", err)
	}
	missing = false
	changedRevision = true
	if _, err = service.Preflight(t.Context(), authored); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatal("changed frozen revision retained writing", err)
	}
	after, err := f.db.GetSubmissionSnapshot(t.Context(), options.RunID, reference.Engineering.SnapshotID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("new result changed retained v2 proof")
	}
}
func TestAgentResultService_ReopenAndReadOnlyAccess(t *testing.T) {
	db, _, o := agentResultFixture(t, "plan")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	data := resultCreate("plan")
	first, err := service.Submit(t.Context(), "create", data)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.EndResultAttempt(t.Context(), service.Binding()); err != nil {
		t.Fatal(err)
	}
	if _, d, err := service.Read(t.Context(), 1, []ar.Target{{Field: "payload.plan.stories", ID: "US-001"}}); err != nil || !d.Valid {
		t.Fatal("historical read lost after revocation", err)
	}
	if _, err = service.Preflight(t.Context(), data); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatal("historical reader gained writing")
	}
	path := db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	// Reconnection preserves the already authorized reader binding, without
	// binding a new writer or reaching into runtime factory state.
	reader, err := OpenAgentResultReader(t.Context(), reopened, service.Binding(), ar.EditTargets("plan"))
	if err != nil {
		t.Fatal(err)
	}
	service = reader
	replay, err := service.Submit(t.Context(), "create", data)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("lost-response replay changed after reopen", err)
	}
}

func TestAgentResultReader_DoesNotGrantWriter(t *testing.T) {
	db, _, o := agentResultFixture(t, "research")
	writer, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	data := resultCreate("research")
	if _, err = writer.Submit(t.Context(), "create", data); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenAgentResultReader(t.Context(), db, writer.Binding(), []ar.Target{{Field: "comment"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, d, err := reader.Read(t.Context(), 0, []ar.Target{{Field: "comment"}}); err != nil || !d.Valid {
		t.Fatal("authorized projection refused", err)
	}
	if _, d, err := reader.Read(t.Context(), 0, []ar.Target{{Field: "payload.research.request"}}); err != nil || d.Valid {
		t.Fatal("reader broadened scope", err)
	}
	update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"unauthorized"}]}`)
	if _, err = reader.Submit(t.Context(), "new", update); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatal("read-only service acquired active writer", err)
	}
	if _, err = reader.Observe(t.Context(), writer.Binding().RunID, "create", "publication", "delivered"); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatal("read-only service acquired observation authority", err)
	}
	if _, err = reader.Submit(t.Context(), "create", data); err != nil {
		t.Fatal("exact historical retry blocked", err)
	}
}
