package turn

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// capturedContributionWithCheck mirrors capturedContribution's engineering
// shape but lets the caller pick the check reference, so two different
// stories can be made to report the identical agent-supplied check
// identity (e.g. "build") the way issue #309 described: two independent
// scoped snapshots, each retaining a check under the same human-chosen name.
func capturedContributionWithCheck(t *testing.T, worktree, story, checkID string) agent.Contribution {
	t.Helper()
	content := []byte("package sample\n")
	path := story + ".go"
	mustProgress(t, os.WriteFile(filepath.Join(worktree, path), content, 0600))
	files := []agent.StorySnapshotFile{{Path: path, Content: content, SHA256: agent.StoryDigest(content)}}
	digest := agent.SnapshotDigest(files)
	return agent.Contribution{
		Stage: "engineering", Story: story, Outcome: "engineering_complete",
		Engineering: &agent.EngineeringContribution{SnapshotID: "snapshot-" + story, CheckIDs: []string{checkID}},
		Capture: &agent.ContributionCapture{
			ImplementationDigest: digest,
			Files:                []agent.ContributionCaptureFile{{Path: path, Content: base64.StdEncoding.EncodeToString(content), SHA256: files[0].SHA256}},
			Checks:               []agent.ContributionCaptureCheck{{ID: checkID, Name: "build", Passed: true, Evidence: "ok " + story, ImplementationDigest: digest}},
		},
	}
}

// TestSubmissionSessionScopesChecksBySnapshotAcrossStories is the regression
// test for issue #309 (US-001, AC-002): two stories bound to the same run
// each retain a check under the identical agent-supplied reference "build",
// scoped to their own distinct snapshot. Before the fix, the second story's
// retention collided with the first's under the old (run_id,reference) key
// and was rejected as an immutable conflict; both must now preflight and
// submit successfully, and every production reader (here,
// [store.DB.GetSubmissionCheck] as the submission session itself calls it)
// must resolve exactly the correct snapshot's check, never the other
// story's.
func TestSubmissionSessionScopesChecksBySnapshotAcrossStories(t *testing.T) {
	f, _, session, o := submissionSessionFixture(t, store.WorkflowSeparated)

	first := capturedContributionWithCheck(t, o.Worktree, "US-1", "build")
	response1 := callContribution(t, session, "submit", first)
	var r1 store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(response1.Result, &r1))
	if r1.EngineeringState != "recorded" {
		t.Fatalf("US-1's engineering contribution using check id %q was not recorded: %+v", "build", r1)
	}

	second := capturedContributionWithCheck(t, o.Worktree, "US-2", "build")
	response2 := callContribution(t, session, "submit", second)
	var r2 store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(response2.Result, &r2))
	if r2.EngineeringState != "recorded" {
		t.Fatalf("US-2's engineering contribution reusing check id %q under a distinct snapshot collided: %+v", "build", r2)
	}

	ev1, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	ev2, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-2")
	mustProgress(t, err)
	if len(ev1.Payload) == 0 || len(ev2.Payload) == 0 {
		t.Fatal("engineering evidence missing for one of the two stories")
	}

	checkA, err := f.db.GetSubmissionCheck(t.Context(), o.RunID, "snapshot-US-1", "build")
	mustProgress(t, err)
	checkB, err := f.db.GetSubmissionCheck(t.Context(), o.RunID, "snapshot-US-2", "build")
	mustProgress(t, err)
	if checkA.SnapshotRef == checkB.SnapshotRef {
		t.Fatal("both stories' identically-named checks resolved to the same snapshot")
	}
	if checkA.Reference != "build" || checkB.Reference != "build" {
		t.Fatalf("unexpected check references: %+v %+v", checkA, checkB)
	}

	// A lookup under the wrong snapshot must miss, never silently resolve to
	// the other story's check.
	if _, err := f.db.GetSubmissionCheck(t.Context(), o.RunID, "snapshot-US-1", "does-not-exist"); err == nil {
		t.Fatal("unrelated reference under a real snapshot was found")
	}
}
