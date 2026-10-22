package daemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func publishHandoffBook(t *testing.T, db *store.DB, id string, base int64) store.StandardsRevision {
	t.Helper()
	writer := &standards.Service{Store: db, OwnerID: 71}
	kind := "category.add"
	if base != 0 {
		kind = "category.replace"
	}
	proposal, err := writer.Create(t.Context(), id, base, []standards.Operation{{Kind: kind, Category: &standards.Category{ID: "go", Name: "Go", Description: id}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Approve(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	revision, err := writer.Publish(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func bindHandoffBook(t *testing.T, db *store.DB, root int64) standards.RuntimeStatus {
	t.Helper()
	runtime := &standards.Runtime{Store: db, RootRunID: fmt.Sprint(root), ConsumerKey: "worker"}
	status, err := runtime.Bind(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if status.Integrity != "verified" {
		t.Fatalf("book not verified: %+v", status)
	}
	return status
}

func handoffOwnerLaunch(t *testing.T, db *store.DB, session store.Session, trigger, root int64) store.WorkflowAdmission {
	t.Helper()
	if _, err := db.AdmitWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	pin, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: testDigest("a"), TeamSnapshotDigest: testDigest("b")})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: trigger, DefinitionID: pin.ID, Start: "budget", ContentDigest: testDigest("c"), TopologyDigest: testDigest("d"), Files: []byte(`{"role":"eA=="}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordStandardsGraphInvocation(t.Context(), launch.LaunchID, root); err != nil {
		t.Fatal(err)
	}
	if err = db.SetGraphInstanceStatus(t.Context(), launch.Instance.ID, store.GraphFailed, time.Time{}, launch.Instance.StartedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	return launch
}

func TestStandardsPrivateScheduleFreezesFailedOwnerInvocation(t *testing.T) {
	f := newImplementDaemonFixture(t)
	db := f.d.db
	ctx := t.Context()
	worker, err := db.GetOrCreateSession(ctx, f.repo, 11)
	if err != nil {
		t.Fatal(err)
	}
	row := f.row(t, "story-001")
	if err = db.SetImplementWorkerSession(ctx, row.Parent, row.Generation, row.Key, worker.ID, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	row = f.row(t, row.Key)
	first := publishHandoffBook(t, db, "original-story-book", 0)
	const storyRoot int64 = -101
	bindHandoffBook(t, db, storyRoot)
	if err = db.SaveStandardsStoryInvocation(ctx, row.Parent, row.Generation, row.Key, row.StateAt, storyRoot); err != nil {
		t.Fatal(err)
	}
	second := publishHandoffBook(t, db, "new-owner-book", first.ID)
	const ownerRoot int64 = -202
	bindHandoffBook(t, db, ownerRoot)
	failed := handoffOwnerLaunch(t, db, worker, 3000, ownerRoot)
	schedule, _, err := db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: worker.ID, SourceCommentID: implementWorkID(row, failed.Instance.ID), OwnerHandle: "owner", Payload: implementWorkPayload, DueAt: failed.Instance.StartedAt.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	// Production discovery freezes the failed instance's source before dispatch.
	if err = f.d.freezeImplementStandardsSchedule(ctx, row, schedule); err != nil {
		t.Fatal(err)
	}
	frozen, err := db.GetStandardsScheduledInvocation(ctx, worker.ID, schedule.TriggerID())
	if err != nil || frozen != ownerRoot {
		t.Fatalf("schedule froze %d, want failed owner %d (story source %d): %v", frozen, ownerRoot, storyRoot, err)
	}
	// A later owner launch changes the latest worker instance and current book.
	// Dispatching this already-created schedule must still use its exact source.
	third := publishHandoffBook(t, db, "later-owner-book", second.ID)
	const laterRoot int64 = -303
	bindHandoffBook(t, db, laterRoot)
	latest := handoffOwnerLaunch(t, db, worker, 4000, laterRoot)
	if err = f.d.freezeImplementStandardsSchedule(ctx, row, schedule); err != nil {
		t.Fatal(err)
	}
	frozen, err = db.GetStandardsScheduledInvocation(ctx, worker.ID, schedule.TriggerID())
	if err != nil || frozen != ownerRoot {
		t.Fatalf("replayed discovery replaced frozen source: %d %v", frozen, err)
	}
	runtime := &standards.Runtime{Store: db, RootRunID: fmt.Sprint(frozen), ConsumerKey: "worker"}
	status, err := runtime.Status(ctx)
	if err != nil || status.Integrity != "verified" || status.Binding.RevisionID != second.ID {
		t.Fatalf("scheduled runtime changed book after latest owner changed: %+v %v", status, err)
	}
	// A previously unseen schedule is authorized only by the exact work ID it
	// names. A predecessor's ID cannot borrow the latest owner's source book.
	for _, test := range []struct {
		name                   string
		source, root, revision int64
	}{
		{name: "original story work", source: implementWorkID(row, 0), root: storyRoot, revision: first.ID},
		{name: "matching latest retry", source: implementRetryWorkID(row, latest.Instance.ID, 1), root: laterRoot, revision: third.ID},
		{name: "unmatched predecessor retry", source: implementRetryWorkID(row, failed.Instance.ID, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			pending, _, err := db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: worker.ID, SourceCommentID: test.source, OwnerHandle: "owner", Payload: implementWorkPayload, DueAt: latest.Instance.StartedAt.Add(time.Minute)})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.d.freezeImplementStandardsSchedule(t.Context(), row, pending); err != nil {
				t.Fatal(err)
			}
			root, err := db.GetStandardsScheduledInvocation(t.Context(), worker.ID, pending.TriggerID())
			if err != nil {
				t.Fatal(err)
			}
			if test.root != 0 && root != test.root {
				t.Fatalf("work ID selected root %d, want %d", root, test.root)
			}
			runtime := &standards.Runtime{Store: db, RootRunID: fmt.Sprint(root), ConsumerKey: "worker"}
			status, err := runtime.Status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if test.root == 0 {
				if status.Integrity != "unavailable" || status.Binding.RevisionID != 0 || status.Reason == "" || root == storyRoot || root == ownerRoot || root == laterRoot {
					t.Fatalf("unmatched work ID borrowed available guidance: %+v", status)
				}
			} else if status.Integrity != "verified" || status.Binding.RevisionID != test.revision {
				t.Fatalf("work ID changed its source book: %+v", status)
			}
		})
	}
}
