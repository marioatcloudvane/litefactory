package messaging

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type standardsGateOperations struct {
	db       *store.DB
	executed []StandardsRequest
}

func (f *standardsGateOperations) Execute(_ context.Context, r StandardsRequest) (StandardsResult, error) {
	f.executed = append(f.executed, r)
	return StandardsResult{Text: "approved"}, nil
}
func (*standardsGateOperations) Read(context.Context, StandardsRequest) (StandardsResult, error) {
	return StandardsResult{}, nil
}

// These fixtures replace only the domain boundary; shared policy and SQLite
// admission stay real in transport tests.
func (f *standardsGateOperations) ResolveFrozen(ctx context.Context, _ owneraction.Principal, r StandardsRequest) (owneraction.Facts, error) {
	args := maps.Clone(r.Arguments)
	if args == nil {
		args = map[string]string{}
	}
	kind, action, _ := strings.Cut(r.Operation, ".")
	args["action"] = action
	if kind == "author" && action != "start" {
		if args["id"] == "" {
			args["id"] = "conversation-discord:3:nl-exec:abc"
		}
		if args["version"] == "" {
			args["version"] = "4"
		}
	}
	target := args["id"]
	if target == "" {
		target = "saved-target"
	}
	repo, _ := strconv.ParseInt(args["repository_id"], 10, 64)
	reference := ""
	if repo > 0 && f.db != nil {
		registered, err := f.db.GetRepo(ctx, repo)
		if err != nil {
			return owneraction.Facts{}, err
		}
		reference = registered.Forge + ":" + registered.Slug()
	}
	return owneraction.Facts{Invocation: owneraction.Invocation{Command: "standards-" + kind, Arguments: args}, RepositoryID: repo, RepositoryRef: reference, TargetKind: kind, TargetID: target, TargetRevision: "version 4", Summary: r.Operation, Estimate: "unknown", Payload: `{"exact":true}`}, nil
}
func (*standardsGateOperations) RecheckFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) error {
	return nil
}
func (f *standardsGateOperations) ExecuteFrozen(ctx context.Context, _ owneraction.Principal, r StandardsRequest, _ owneraction.Facts) (StandardsResult, error) {
	return f.Execute(ctx, r)
}
func (*standardsGateOperations) RecoverFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) (StandardsResult, bool, error) {
	return StandardsResult{}, false, nil
}

type gateApproval struct{}

func (gateApproval) Send(_ context.Context, p owneraction.Proposal) (string, error) {
	return "approval:" + p.ID, nil
}

func installGateActions(t *testing.T, s *Service, clock func() time.Time) owneraction.Principal {
	t.Helper()
	if _, err := s.db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := s.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "3", DiscordOwnerID: "7"}
	actions := owneraction.New(s.db, OwnerActionDomain{Service: s}, clock)
	actions.SetDiscord("7", []string{"3"})
	actions.SetDelivery(gateApproval{})
	s.EnableOwnerActions(actions, func(context.Context, string) (owneraction.Principal, error) { return principal, nil })
	return principal
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
	ops := &standardsGateOperations{db: db}
	service.EnableStandards(ops)
	clock := at
	installGateActions(t, service, func() time.Time { return clock })
	next := 100
	run := func(text string, at time.Time) *factoryConversationRun {
		t.Helper()
		clock = at
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

func TestStandardsApprovalRequiresConversationAndOwnerTap(t *testing.T) {
	s, ops, start := standardsGateFixture(t)
	s.conversations.Load().factory = true
	now := time.Now().UTC()
	sender, ok := s.sender.(*standardsDeliverySender)
	if !ok {
		t.Fatal("fixture sender missing")
	}
	run := start("approve reviewed proposal", now)
	if err := run.propose(t.Context(), Invocation{Command: "standards-proposal", Arguments: map[string]string{"action": "approve", "id": "proposal-1", "version": "2", "digest": "change"}}, ConversationEnvelope{}, nil); err != nil {
		t.Fatal(err)
	}
	deliverAll(t, s, sender, "prompt", now)
	page, err := s.ownerActions.History(t.Context(), mustGatePrincipal(t, s), "", 10)
	if err != nil || len(page.Proposals) != 1 {
		t.Fatalf("proposal: %+v %v", page, err)
	}
	proposal := page.Proposals[0]
	run = start("yes", now.Add(5*time.Second))
	run.t.ReplyTo = "prompt"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatalf("confirm: %v %v", handled, err)
	}
	if _, err = s.ownerActions.Step(t.Context(), proposal.ID); err != nil {
		t.Fatal(err)
	}
	if len(ops.executed) != 0 {
		t.Fatal("binding action ran before owner tap")
	}
	if _, err = s.ownerActions.Reaction(t.Context(), owneraction.Reaction{ChannelID: "3", MessageID: "approval:" + proposal.ID, UserID: "7", Emoji: "✅"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ownerActions.Step(t.Context(), proposal.ID); err != nil {
		t.Fatal(err)
	}
	if len(ops.executed) != 1 || ops.executed[0].Operation != "proposal.approve" || ops.executed[0].Arguments["digest"] != "change" {
		t.Fatalf("exact approval missing: %+v", ops.executed)
	}
}

func mustGatePrincipal(t *testing.T, s *Service) owneraction.Principal {
	t.Helper()
	p, e := s.ownerPrincipal(t.Context(), "3")
	if e != nil {
		t.Fatal(e)
	}
	return p
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

func TestFactoryChangingActionsCannotUseRetiredPolicyWithoutSharedService(t *testing.T) {
	s, ops, start := standardsGateFixture(t)
	s.EnableOwnerActions(nil, nil)
	now := time.Now().UTC()
	sender, ok := s.sender.(*standardsDeliverySender)
	if !ok {
		t.Fatal("fixture sender missing")
	}
	s.conversations.Load().factory = true
	for _, action := range []string{"answer", "edit", "publish"} {
		run := start("change the standard", now)
		if err := run.propose(t.Context(), Invocation{Command: "standards-author", Arguments: map[string]string{"action": action, "text": "change it"}}, ConversationEnvelope{}, nil); err != nil {
			t.Fatal(err)
		}
		deliverAll(t, s, sender, "refusal", now)
	}
	if len(ops.executed) != 0 {
		t.Fatalf("retired author bypass executed: %+v", ops.executed)
	}
	legacy := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: `{"Invocation":{"Command":"standards-author","Arguments":{"action":"publish"}}}`, Digest: "legacy-digest", Code: "legacy-code", ExpiresAt: now.Add(time.Minute), UpdatedAt: now}
	if err := s.db.SaveNaturalDialogue(t.Context(), legacy, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.db.BindNaturalPrompt(t.Context(), legacy, "legacy-prompt"); err != nil {
		t.Fatal(err)
	}
	run := start("yes", now.Add(6*time.Second))
	run.t.ReplyTo = "legacy-prompt"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatalf("legacy confirmation refusal: %v %v", handled, err)
	}
	if len(ops.executed) != 0 {
		t.Fatal("legacy pending work executed")
	}
	if _, err := s.db.ReadNaturalExecution(t.Context(), "nl-exec:legacy-code"); err == nil {
		t.Fatal("legacy pending work was admitted")
	}
}
