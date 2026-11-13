package store

import (
	"errors"
	"testing"
	"time"
)

// #1514: an owner reply that meets an open question whose publication is still
// owed waits for that question; it is not a broken graph invariant, which the
// daemon would eventually set aside with the raw store error.
func TestGraphReplyToUnpublishedQuestionWaits(t *testing.T) {
	db, _, instance, session := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	stop := GraphStop{Key: "unpublished-stop", InstanceID: instance.ID, Origin: "repair", Cause: "blocked", Reason: "The merge was refused", Candidate: []byte(`{"reason":"blocked"}`), At: at, AllowRunless: true}
	if err := db.ParkGraphNode(ctx, stop); err != nil {
		t.Fatal(err)
	}
	reply := GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: 102, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Workflow: "bug", DeliveryKey: stop.Key, Body: "ship", At: at.Add(time.Minute)}
	for _, step := range []struct {
		name    string
		prepare func()
	}{
		{"no delivery yet", func() {}},
		{"delivery not sent", func() {
			if _, err := db.EnsureReportDelivery(ctx, ReportDelivery{Key: stop.Key, SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#457", TriggerNamespace: "issue-body", Purpose: "graph-stop:" + stop.Key, BotUserID: 99, Body: "Work stopped", Marker: stop.Key}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		name := step.name
		step.prepare()
		_, err := db.ClaimGraphOwnerReply(ctx, reply)
		if !errors.Is(err, ErrGraphOwnerWait) || errors.Is(err, ErrGraphInvariant) {
			t.Fatalf("%s: err = %v, want the reply held for the unposted question", name, err)
		}
		wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
		if err != nil || wait.State != "waiting" || wait.ReplyID != 0 {
			t.Fatalf("%s: held reply changed the wait: %+v %v", name, wait, err)
		}
	}
}
