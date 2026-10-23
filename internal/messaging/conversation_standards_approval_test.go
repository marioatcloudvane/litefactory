package messaging

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type standardsGateOperations struct {
	executed []StandardsRequest
}

func (f *standardsGateOperations) Execute(_ context.Context, r StandardsRequest) (StandardsResult, error) {
	f.executed = append(f.executed, r)
	return StandardsResult{Text: "approved"}, nil
}
func (*standardsGateOperations) Read(context.Context, StandardsRequest) (StandardsResult, error) {
	return StandardsResult{}, nil
}

func standardsGateFixture(t *testing.T) (*Service, *standardsGateOperations, func(string, time.Time) *factoryConversationRun) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, store.Conversation{Transport: "discord", ChannelRef: "3", OwnerID: "7", Name: "standards-gates", Provider: "claude", StateDir: t.TempDir()}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	sender := &standardsDeliverySender{result: Delivery{Confirmed: true, PromptID: "remote-prompt"}}
	service := New(db, "discord", "7", []string{"3"}, sender, FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.conversations.Store(&conversationRuntime{bindings: map[string]ConversationBinding{"3": {Provider: "claude"}}, check: func(context.Context, string) error { return nil }, accessAfter: map[string]time.Time{}})
	ops := &standardsGateOperations{}
	service.EnableStandards(ops)
	next := 100
	run := func(text string, at time.Time) *factoryConversationRun {
		t.Helper()
		next++
		id := fmt.Sprint(next)
		scan, e := db.ReadNaturalScan(ctx, "3")
		if e != nil {
			t.Fatal(e)
		}
		following := scan
		following.HighWater = id
		if e = db.StageNaturalScan(ctx, scan, following, []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, CreatedAt: at}}, true); e != nil {
			t.Fatal(e)
		}
		if _, e = db.AdmitConversationTurn(ctx, c.ID, id, "fixture-prompt", at); e != nil {
			t.Fatal(e)
		}
		turn, e := db.ClaimConversationTurn(ctx, c.ID, "gate-worker", at, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.SaveConversationSession(ctx, turn, "gate-native-session"); e != nil {
			t.Fatal(e)
		}
		return &factoryConversationRun{s: service, c: c, t: turn, at: at}
	}
	return service, ops, run
}

func TestStandardsApprovalIsAnOrdinaryConfirmedAction(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	now := time.Now().UTC()
	p := naturalPayload{Invocation: Invocation{Command: "standards-proposal", Arguments: map[string]string{"action": "approve", "id": "proposal-1", "version": "2", "digest": "change"}}, Channel: "3"}
	if refusal, err := s.resolveNatural(ctx, &p, now); err != nil || refusal != "" {
		t.Fatalf("approval proposal refused: %q %v", refusal, err)
	}
	payload, digest, err := naturalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: digest, Code: "approve-code", ExpiresAt: now.Add(time.Minute), UpdatedAt: now}
	if err = s.db.SaveNaturalDialogue(ctx, dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.db.BindNaturalPrompt(ctx, dialogue, "proposal-prompt"); err != nil {
		t.Fatal(err)
	}
	run := start("yes", now.Add(time.Second))
	run.t.ReplyTo = "proposal-prompt"
	if handled, err := run.confirm(ctx); !handled || err != nil {
		t.Fatalf("confirmation not handled: %v %v", handled, err)
	}
	if len(ops.executed) != 1 || ops.executed[0].Operation != "proposal.approve" || ops.executed[0].Arguments["digest"] != "change" {
		t.Fatalf("approval did not run through the confirmed action: %+v", ops.executed)
	}
}

type standardsGateRecovery struct {
	standardsGateOperations
	recovered []StandardsRequest
}

func (f *standardsGateRecovery) Recover(_ context.Context, r StandardsRequest) (StandardsResult, bool, error) {
	f.recovered = append(f.recovered, r)
	return StandardsResult{Text: "Committed author session saved-author version 2"}, true, nil
}

func TestStandardsConversationRecoversCommittedOutcomeBeforeUncertainChatRecovery(t *testing.T) {
	ctx := t.Context()
	s, _, start := standardsGateFixture(t)
	now := time.Now().UTC()
	run := start("confirm saved operation", now)
	p := naturalPayload{Invocation: Invocation{Command: "standards-author", Arguments: map[string]string{"action": "answer", "id": "saved-author", "version": "1", "question_id": "question-1", "text": "Relevant package checks"}}, Channel: "3"}
	payload, digest, err := naturalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: digest, Code: "saved-code", ExpiresAt: now.Add(time.Minute), UpdatedAt: now}
	if err = s.db.SaveNaturalDialogue(ctx, dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.db.BindNaturalPrompt(ctx, dialogue, "preview-prompt"); err != nil {
		t.Fatal(err)
	}
	reply := store.ChatReply{Transport: "discord", MessageID: "nl-exec:saved-code", ChannelRef: "3", Command: "standards-author", Arguments: p.Invocation.Arguments}
	if err = s.db.AuthorizeConversationDialogue(ctx, run.t, dialogue, run.t.SourceID, reply, now); err != nil {
		t.Fatal(err)
	}
	if err = s.db.StartNaturalExecution(ctx, reply.MessageID, now); err != nil {
		t.Fatal(err)
	}
	// The response-only recovery fixture represents an upstream committed receipt;
	// the daemon adapter test separately verifies it against the shared domain store.
	recovery := &standardsGateRecovery{}
	path := s.db.Path()
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resumed := New(db, "discord", "7", []string{"3"}, nil, FactoryStatus{DB: db})
	resumed.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	resumed.EnableStandards(recovery)
	if err = resumed.recoverStandardsConversationAction(ctx, run.t, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	execution, err := db.ReadNaturalExecution(ctx, reply.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != "finished" || execution.Outcome != "Committed author session saved-author version 2" {
		t.Fatalf("committed operation became uncertain: %+v", execution)
	}
	if len(recovery.executed) != 0 || len(recovery.recovered) != 1 {
		t.Fatal("chat recovery repeated a mutation")
	}
	request := recovery.recovered[0]
	if request.OperationKey != "discord:3:"+reply.MessageID || request.MessageID != run.t.SourceID {
		t.Fatalf("recovery lost original operation identity: %+v", request)
	}
	if err = resumed.recoverStandardsConversationAction(ctx, run.t, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(recovery.recovered) != 1 {
		t.Fatal("finished outcome unnecessarily reconciled again")
	}
}
