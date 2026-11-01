package stagedecision

import (
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
)

func TestDecisionBindingApprovalExecutesOnlyAfterExactDiscordEvidence(t *testing.T) {
	f := newDecisionFixture(t)
	r := f.request()
	r.Change = ""
	p := f.propose(t, "approve_stage", r)
	f.now = f.now.Add(5 * time.Second)
	p, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	p, err = f.actions.Step(t.Context(), p.ID)
	decisionCheck(t, err)
	if p.State != "awaiting_discord" {
		t.Fatalf("approval skipped binding evidence: %+v", p)
	}
	_, err = f.actions.Reaction(t.Context(), owneraction.Reaction{ChannelID: "200", MessageID: "approval-message", UserID: "100", Emoji: "✅", ReceivedAt: f.now})
	decisionCheck(t, err)
	p, err = f.actions.Step(t.Context(), p.ID)
	decisionCheck(t, err)
	if p.State != "succeeded" {
		t.Fatalf("confirmed approval did not commit: %+v", p)
	}
	v, err := f.service.Get(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	decisionCheck(t, err)
	if v.Transition == nil || v.Transition.State != "applied" || v.Transition.Operation != "approve_stage" || wait.ReplyBody != "done" || wait.ReplySource != "chat" {
		t.Fatalf("approval did not use exact current owner boundary: %+v %+v", v, wait)
	}
	_, err = f.actions.Reaction(t.Context(), owneraction.Reaction{ChannelID: "200", MessageID: "approval-message", UserID: "100", Emoji: "✅", ReceivedAt: f.now})
	decisionCheck(t, err)
	after, err := f.actions.Step(t.Context(), p.ID)
	decisionCheck(t, err)
	if after.ExecutionID != p.ExecutionID || after.Result != p.Result {
		t.Fatal("repeated Discord evidence replayed decision")
	}
}
