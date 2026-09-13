package store

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func aggregateStoreFixture(t *testing.T) (*DB, ResultBinding, ResultAggregate, SubmissionAttempt) {
	t.Helper()
	db, source := submissionFixture(t, "engineering")
	selected := []ResultStoryRevision{}
	for _, story := range source.Stories {
		selected = append(selected, ResultStoryRevision{Story: story.Story, Revision: story.Revision})
	}
	b, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: source.Parent, Mode: "implement", WorkContext: "aggregate", RunID: source.RunID, Attempt: "aggregate-attempt", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2, FrozenStories: selected})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := submissionSnapshot(source, "aggregate-snapshot")
	a := ResultAggregate{ResultID: b.ResultID, RunID: b.RunID, Reference: "aggregate-snapshot", Attempt: b.Attempt, Parent: b.Task, ContextHash: b.ContextHash, Digest: snapshot.Digest, Selected: selected, Files: snapshot.Files, Checks: []ResultAggregateCheck{{Reference: "aggregate-check", ExecutionID: "aggregate-execution", Kind: "aggregate", Name: "Synthetic retained aggregate fact", Digest: snapshot.Digest, Evidence: "Offline synthetic persistence fixture", RunID: b.RunID, Attempt: b.Attempt, Executed: true, Passed: true}}}
	return db, b, a, source
}
func TestResultAggregate_ImmutableReopenAndSourceIsolation(t *testing.T) {
	db, b, a, _ := aggregateStoreFixture(t)
	if err := db.RetainResultAggregate(t.Context(), b, a); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetResultAggregate(t.Context(), b, a.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RetainResultAggregate(t.Context(), b, a); err != nil {
		t.Fatal("exact retention replay failed", err)
	}
	a.Files[0].Content[0] = '!'
	a.Checks[0].Evidence = "mutated"
	a.Selected[0].Revision = "changed"
	retained, err := db.GetResultAggregate(t.Context(), b, before.Reference)
	if err != nil || !reflect.DeepEqual(before, retained) {
		t.Fatal("retained aggregate aliases input", err)
	}
	path := db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fresh.Close(); err != nil {
			t.Error(err)
		}
	}()
	reopened, err := fresh.GetResultAggregate(t.Context(), b, before.Reference)
	if err != nil || !reflect.DeepEqual(before, reopened) {
		t.Fatal("association or bytes lost after reopen", err)
	}
	if before.ContextHash != b.ContextHash || before.Checks[0].RunID != b.RunID || before.Checks[0].Attempt != b.Attempt || before.SelectedHash == "" {
		t.Fatal("missing first-class attribution")
	}
	if revisions, err := fresh.ResultRevisionNumbers(t.Context(), b); err != nil || len(revisions) != 0 {
		t.Fatal("aggregate retention mutated result revision")
	}
}
func TestResultAggregate_RejectsWrongBindingAndScope(t *testing.T) {
	for _, fault := range []string{"context", "parent", "result", "run", "attempt", "set", "revision", "missing revision", "digest", "check digest", "check run", "check attempt", "story kind", "unexecuted pass"} {
		t.Run(fault, func(t *testing.T) {
			db, b, a, _ := aggregateStoreFixture(t)
			switch fault {
			case "context":
				a.ContextHash = "other"
			case "parent":
				a.Parent = "other"
			case "result":
				a.ResultID++
			case "run":
				a.RunID++
			case "attempt":
				a.Attempt = "other"
			case "set":
				a.Selected = a.Selected[:1]
			case "revision":
				a.Selected[0].Revision = submissionTestDigest([]byte("other"))
			case "missing revision":
				a.Selected[0].Revision = ""
			case "digest":
				a.Digest = submissionTestDigest(nil)
			case "check digest":
				a.Checks[0].Digest = submissionTestDigest(nil)
			case "check run":
				a.Checks[0].RunID++
			case "check attempt":
				a.Checks[0].Attempt = "other"
			case "story kind":
				a.Checks[0].Kind = "story"
			case "unexecuted pass":
				a.Checks[0].Executed = false
			}
			if err := db.RetainResultAggregate(t.Context(), b, a); err == nil {
				t.Fatal("mismatched aggregate accepted")
			}
			if _, err := db.GetResultAggregate(t.Context(), b, a.Reference); !errors.Is(err, ErrNotFound) {
				t.Fatal("failed aggregate left partial snapshot", err)
			}
		})
	}
}
func TestResultAggregate_RejectsStoryRelabellingAndAtomicCheckConflict(t *testing.T) {
	db, b, a, source := aggregateStoreFixture(t)
	prior := submissionSnapshot(source, "story-proof")
	if err := db.RetainSubmissionSnapshot(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	check := SubmissionCheck{RunID: source.RunID, Attempt: source.Attempt, Reference: "story-check", SnapshotRef: prior.Reference, Name: "Scoped synthetic check", Executed: true, Passed: true, Provenance: []byte(`{"scope":"story"}`), Result: []byte(`{"passed":true}`)}
	if err := db.RetainSubmissionCheck(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	a.Checks[0].Reference = check.Reference
	if err := db.RetainResultAggregate(t.Context(), b, a); err == nil {
		t.Fatal("story-only check relabelled")
	}
	a.Checks[0].Reference = "aggregate-check"
	if err := db.RetainResultAggregate(t.Context(), b, a); err != nil {
		t.Fatal(err)
	}
	conflicting := a
	conflicting.Reference = "conflicting-snapshot"
	if err := db.RetainResultAggregate(t.Context(), b, conflicting); err == nil {
		t.Fatal("immutable check reused for another aggregate")
	}
	if _, err := db.GetResultAggregate(t.Context(), b, conflicting.Reference); !errors.Is(err, ErrNotFound) {
		t.Fatal("check conflict left orphaned snapshot")
	}
	after, err := db.GetSubmissionSnapshot(t.Context(), source.RunID, prior.Reference)
	if err != nil || !bytes.Equal(after.Files[0].Content, prior.Files[0].Content) {
		t.Fatal("prior scoped proof changed")
	}
	afterCheck, err := db.GetSubmissionCheck(t.Context(), source.RunID, check.Reference)
	if err != nil || !reflect.DeepEqual(check, afterCheck) {
		t.Fatal("prior scoped check changed")
	}
}
