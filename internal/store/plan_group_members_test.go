package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func planMemberFixture(t *testing.T) (*DB, GraphInstance, GraphNodeAttempt) {
	t.Helper()
	db := newTestDB(t)
	s := testSession(t, db, 913)
	now := graphTestTime()
	d, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: fmt.Sprintf("%064x", 123), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	i, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: s.ID, DefinitionID: d.ID, CurrentNode: "group", StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "group", StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return db, i, a
}
func memberStart(t *testing.T, parent int64, role string) PlanGroupMemberRunStart {
	return PlanGroupMemberRunStart{ParentAttemptID: parent, Role: role, Node: "member-" + role, Basis: "basis", Agent: "codex", SessionRef: "private-" + role, StartedAt: graphTestTime().Add(time.Second), LogPath: func(n int) string { return filepath.Join(t.TempDir(), fmt.Sprintf("%d.jsonl", n)) }}
}
func TestPlanGroupMemberIsolationAndCompletion(t *testing.T) {
	db, i, parent := planMemberFixture(t)
	runs := []Run{}
	attempts := []GraphNodeAttempt{}
	segments := []Segment{}
	for _, role := range []string{"story", "technical", "acceptance"} {
		run, seg, attempt, member, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, parent.ID, role))
		if err != nil {
			t.Fatal(err)
		}
		if member.ParentAttemptID != parent.ID || member.AttemptID != attempt.ID || run.SessionID != i.SessionID || attempt.RunID != run.ID || seg.ID != run.SegmentID || !IsPrivateTrigger(run.TriggerCommentID) || IsGraphTrigger(run.TriggerCommentID) {
			t.Fatalf("bad isolated identities: %+v %+v %+v", member, run, attempt)
		}
		runs = append(runs, run)
		segments = append(segments, seg)
		attempts = append(attempts, attempt)
	}
	for j := range runs {
		for k := j + 1; k < len(runs); k++ {
			if runs[j].ID == runs[k].ID || segments[j].ID == segments[k].ID || segments[j].SessionRef == segments[k].SessionRef {
				t.Fatal("member conversations shared")
			}
		}
	}
	if _, _, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, parent.ID, "story")); err == nil {
		t.Fatal("duplicate active role admitted")
	}
	if err := db.CompletePlanGroupMember(t.Context(), attempts[0].ID, GraphSucceeded, "completed", []byte(`{"contribution":true}`), graphTestTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePlanGroupMember(t.Context(), attempts[0].ID, GraphSucceeded, "completed", []byte(`{"contribution":true}`), graphTestTime().Add(2*time.Second)); err != nil {
		t.Fatal("duplicate completion", err)
	}
	if err := db.CompletePlanGroupMember(t.Context(), attempts[0].ID, GraphSucceeded, "completed", []byte(`{"changed":true}`), graphTestTime().Add(2*time.Second)); err == nil {
		t.Fatal("duplicate result replaced retained output")
	}
	outputs, err := db.ListGraphOutputs(t.Context(), attempts[0].ID)
	if err != nil || len(outputs) != 1 {
		t.Fatal("output replayed", outputs, err)
	}
	current, err := db.GetGraphInstance(t.Context(), i.ID)
	if err != nil || current.CurrentNode != "group" {
		t.Fatal("member moved outer graph", current, err)
	}
	members, err := db.ListPlanGroupMembers(t.Context(), parent.ID)
	if err != nil || len(members) != 3 {
		t.Fatal("membership lost", members, err)
	}
	if _, _, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, parent.ID, "story")); err != nil {
		t.Fatal("continuation refused", err)
	}
}
func TestPlanGroupMemberSharedResultWritersAndStaleParent(t *testing.T) {
	db, i, parent := planMemberFixture(t)
	session, err := db.GetSession(t.Context(), i.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []ResultBinding{}
	for _, role := range []string{"story", "technical", "acceptance"} {
		run, _, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, parent.ID, role))
		if err != nil {
			t.Fatal(err)
		}
		binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: session.TaskRef, Mode: "plan", WorkContext: "parallel/" + role, Attempt: role, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	for _, binding := range bindings {
		if err := db.CheckResultWriter(t.Context(), binding); err != nil {
			t.Fatal("sibling binding revoked", err)
		}
	}
	instance := fmt.Sprintf("%064x", 1)
	if err := db.StartMCPInstance(t.Context(), instance); err != nil {
		t.Fatal(err)
	}
	leases := []MCPLease{}
	for _, binding := range bindings {
		lease := MCPLease{Binding: binding, Instance: instance, PermissionDigest: fmt.Sprintf("%064x", 2), ExpiresAt: time.Now().Add(time.Hour)}
		if err := db.BindMCPLease(t.Context(), lease); err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	for _, lease := range leases {
		if err := db.CheckMCPLease(t.Context(), lease); err != nil {
			t.Fatal("sibling MCP lease revoked", err)
		}
	}
	replacement, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: session.TaskRef, Mode: "plan", WorkContext: bindings[0].WorkContext, Attempt: "story-new-transport", RunID: bindings[0].RunID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	replacementLease := MCPLease{Binding: replacement, Instance: instance, PermissionDigest: fmt.Sprintf("%064x", 3), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.BindMCPLease(t.Context(), replacementLease); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckResultWriter(t.Context(), bindings[0]); err == nil {
		t.Fatal("same-run replaced result writer remained active")
	}
	if err := db.CheckMCPLease(t.Context(), leases[0]); err == nil {
		t.Fatal("same-run replaced transport remained active")
	}
	if err := db.CheckMCPLease(t.Context(), replacementLease); err != nil {
		t.Fatal("replacement transport unavailable", err)
	}
	for j := 1; j < len(bindings); j++ {
		if err := db.CheckResultWriter(t.Context(), bindings[j]); err != nil {
			t.Fatal("transport replacement revoked sibling writer", err)
		}
		if err := db.CheckMCPLease(t.Context(), leases[j]); err != nil {
			t.Fatal("transport replacement revoked sibling lease", err)
		}
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), parent.ID, GraphCancelled, "", graphTestTime().Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		if err := db.CheckResultWriter(t.Context(), binding); err == nil {
			t.Fatal("expired parent retained writer authority")
		}
		if err := db.CheckResultReader(t.Context(), binding); err != nil {
			t.Fatal("historical reader lost", err)
		}
	}
}
func TestPlanGroupMemberAllocationRollbackAndPlanOnly(t *testing.T) {
	db, _, parent := planMemberFixture(t)
	in := memberStart(t, parent.ID, "story")
	in.LogPath = func(int) string { return "" }
	if _, _, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), in); err == nil {
		t.Fatal("blank path admitted")
	}
	members, err := db.ListPlanGroupMembers(t.Context(), parent.ID)
	if err != nil || len(members) != 0 {
		t.Fatal("partial reservation leaked", members, err)
	}
	ordinary := graphTestInstance(t, db)
	attempt := graphTestAttempt(t, db, ordinary, 1)
	if _, _, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, attempt.ID, "story")); err == nil {
		t.Fatal("non-Plan group admitted")
	}
	if _, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: ordinary.ID, Node: "not-current", StartedAt: graphTestTime()}); err == nil {
		t.Fatal("ordinary reservation weakened")
	}
}

func TestPlanGroupMemberRecoveryRetainsAcceptedResultsAndExpiresOrphans(t *testing.T) {
	db, instance, parent := planMemberFixture(t)
	run, _, child, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, parent.ID, "story"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: session.TaskRef, Mode: "plan", WorkContext: "parallel/story", Attempt: "accepted-before-crash", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := db.RecordResult(t.Context(), resultRecord(binding, "lost-reply", 0))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AppliedRevision != 1 {
		t.Fatal("result was not retained")
	}
	fresh, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: session.TaskRef, Mode: "plan", WorkContext: binding.WorkContext, Attempt: "new-nonce-without-submission", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if fresh == binding {
		t.Fatal("fresh transport did not get a separate binding")
	}
	actual, err := db.PlanGroupMemberResultBinding(t.Context(), child.ID)
	if err != nil || actual != binding {
		t.Fatal("unsubmitted nonce replaced accepted result attribution", actual, err)
	}
	finishedRun, _, finishedChild, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, parent.ID, "technical"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePlanGroupMember(t.Context(), finishedChild.ID, GraphSucceeded, "completed", []byte(`{}`), graphTestTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	// The process stops after its receipt, before child completion or collection.
	if err := db.FinishGraphNodeAttempt(t.Context(), parent.ID, GraphCancelled, "", graphTestTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGraphInstanceStatus(t.Context(), instance.ID, GraphCancelled, time.Time{}, graphTestTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	successor, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: instance.SessionID, DefinitionID: instance.DefinitionID, CurrentNode: instance.CurrentNode, StartedAt: graphTestTime().Add(3 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	instance = successor
	next, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, StartedAt: graphTestTime().Add(3 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CloseOrphanPlanGroupMembers(t.Context(), instance.ID, next.ID, graphTestTime().Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetGraphNodeAttempt(t.Context(), child.ID)
	if err != nil || got.Status != GraphCancelled {
		t.Fatal("orphan not closed", got, err)
	}
	completedChild, err := db.GetGraphNodeAttempt(t.Context(), finishedChild.ID)
	if err != nil || completedChild.Status != GraphSucceeded {
		t.Fatal("recovery replaced completed child", completedChild, err)
	}
	completedRun, err := db.GetRun(t.Context(), finishedRun.ID)
	if err != nil || completedRun.Status != RunSucceeded {
		t.Fatal("completed child run leaked", completedRun, err)
	}
	gotRun, err := db.GetRun(t.Context(), run.ID)
	if err != nil || gotRun.Status != RunFailed {
		t.Fatal("orphan run not closed", gotRun, err)
	}
	recovered, err := db.PlanGroupMemberResultBinding(t.Context(), child.ID)
	if err != nil || recovered != binding {
		t.Fatal("accepted result binding lost", recovered, err)
	}
	revision, err := db.GetResultRevision(t.Context(), recovered, 0)
	if err != nil || revision.Revision != 1 {
		t.Fatal("accepted result lost", revision, err)
	}
	if err := db.CheckResultWriter(t.Context(), recovered); err == nil {
		t.Fatal("orphan writer resurrected")
	}
	members, err := db.ListPlanGroupMembersByBasis(t.Context(), instance.ID, "basis")
	if err != nil || len(members) != 2 || members[0].AttemptID != child.ID {
		t.Fatal("basis recovery lost child", members, err)
	}
	if wrong, err := db.ListPlanGroupMembersByBasis(t.Context(), instance.ID, "changed"); err != nil || len(wrong) != 0 {
		t.Fatal("stale basis adopted", wrong, err)
	}
	// Recovery must not close a current parent's live siblings.
	_, _, live, _, err := db.AllocatePlanGroupMemberRun(t.Context(), memberStart(t, next.ID, "story"))
	if err != nil {
		t.Fatal("orphan blocked replacement", err)
	}
	if err := db.CloseOrphanPlanGroupMembers(t.Context(), instance.ID, next.ID, graphTestTime().Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	liveState, err := db.GetGraphNodeAttempt(t.Context(), live.ID)
	if err != nil || liveState.Status != GraphRunning {
		t.Fatal("live member cancelled", liveState, err)
	}
	if _, err := db.PlanGroupMemberResultBinding(t.Context(), live.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing result invented", err)
	}
}

func TestPlanGroupMemberSessionReferenceUsesReservedOrdinal(t *testing.T) {
	db, _, parent := planMemberFixture(t)
	in := memberStart(t, parent.ID, "story")
	in.SessionRef = ""
	calls := 0
	ordinal := 0
	in.SessionRefForOrdinal = func(n int) (string, error) { calls++; ordinal = n; return fmt.Sprintf("session-%d", n), nil }
	_, segment, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || ordinal != segment.Ordinal || segment.SessionRef != fmt.Sprintf("session-%d", ordinal) {
		t.Fatal("reference was not bound to reserved segment", segment, calls, ordinal)
	}
	in = memberStart(t, parent.ID, "technical")
	in.SessionRef = ""
	in.SessionRefForOrdinal = func(int) (string, error) { return "", fmt.Errorf("reference unavailable") }
	if _, _, _, _, err := db.AllocatePlanGroupMemberRun(t.Context(), in); err == nil {
		t.Fatal("reference error ignored")
	}
	members, err := db.ListPlanGroupMembers(t.Context(), parent.ID)
	if err != nil || len(members) != 1 {
		t.Fatal("failed reference leaked reservation", members, err)
	}
}
