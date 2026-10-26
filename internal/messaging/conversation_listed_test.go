package messaging

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func listedRunningTasks(t *testing.T, run *factoryConversationRun, numbers ...int) {
	t.Helper()
	ctx := t.Context()
	repo, err := run.s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range numbers {
		session, err := run.s.db.GetOrCreateSession(ctx, repo, n)
		if err != nil {
			t.Fatal(err)
		}
		segment, err := run.s.db.GetOrCreateSegment(ctx, session.ID, 1, "status", "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := run.s.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "codex", TriggerCommentID: 1, StartedAt: run.at, LogPath: func(int) string { return "/tmp/status-fixture" }}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConversationStatusListedTaskGroundsProposal(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true}, "Yes ok do it")
	run.s.EnableResearch(&factoryUnitDiagnosis{}, nil)
	numbers := []int{1100}
	for n := 1; n <= 2*conversationListedLimit; n++ {
		numbers = append(numbers, n)
	}
	listedRunningTasks(t, run, numbers...)
	run.ordinal++
	result, _, err := run.lookup(t.Context(), Invocation{Command: "status", Arguments: map[string]string{}})
	if err != nil || !strings.Contains(result.Result, "#1100") {
		t.Fatalf("status lookup %q %v", result.Result, err)
	}
	if listed := run.takeListed(); len(listed) != conversationListedLimit {
		t.Fatalf("listed references %d, want the per-lookup cap %d", len(listed), conversationListedLimit)
	}
	refs, err := run.references(t.Context())
	if err != nil || len(refs) != conversationListedLimit {
		t.Fatalf("retained references=%d err=%v", len(refs), err)
	}
	var listed ConversationReference
	for _, ref := range refs {
		if ref.Arguments["issue"] == "1100" {
			listed = ref
		}
	}
	if listed.ID == "" || listed.Arguments["project"] != "acme/app" {
		t.Fatalf("listed task #1100 not referenceable: %+v", refs)
	}
	raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "propose", Command: "diagnose", Arguments: []ConversationArgument{{Name: "issue", Reference: listed.ID}, {Name: "project", Reference: listed.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "101", Text: "Yes ok do it"}}, Commands: run.s.ConversationCommands(), References: refs, OwnerID: run.s.owner, ChannelID: run.c.ChannelRef, Now: at}
	_, inv, err := DecodeConversationEnvelope(raw, request)
	if err != nil || inv.Arguments["issue"] != "1100" || inv.Arguments["project"] != "acme/app" {
		t.Fatalf("listed proposal %+v %v", inv, err)
	}
	if _, _, err = run.s.ValidateInvocation(inv); err != nil {
		t.Fatalf("listed proposal invalid: %v", err)
	}

	// An unknown or expired listed reference stays ungrounded and is named.
	for name, req := range map[string]ConversationDecodeRequest{
		"unknown": {Turns: request.Turns, Commands: request.Commands, References: []ConversationReference{}, OwnerID: request.OwnerID, ChannelID: request.ChannelID, Now: at},
		"expired": {Turns: request.Turns, Commands: request.Commands, References: refs, OwnerID: request.OwnerID, ChannelID: request.ChannelID, Now: listed.ExpiresAt.Add(time.Second)},
	} {
		_, _, err = DecodeConversationEnvelope(raw, req)
		var rejected *ConversationEnvelopeError
		if !errors.As(err, &rejected) || rejected.Argument == nil || rejected.Argument.Name != "issue" || !rejected.Ungrounded || !strings.HasPrefix(conversationEnvelopeReason(err), "issue: ") {
			t.Fatalf("%s listed reference: %v", name, err)
		}
	}
	later := *run
	later.at = listed.ExpiresAt.Add(time.Second)
	if refs, err = later.references(t.Context()); err != nil || len(refs) != 0 {
		t.Fatalf("expired listed references kept: %+v %v", refs, err)
	}
}

func TestConversationArgumentErrorsNameTheArgument(t *testing.T) {
	s := New(naturalExecutionDB(t), "discord", "1", nil, nil, FactoryStatus{})
	s.EnableResearch(nil, nil)
	_, _, err := s.ValidateInvocation(Invocation{Command: "diagnose", Arguments: map[string]string{"issue": "12", "note": strings.Repeat("x", 9000)}})
	var arg *ArgumentError
	if !errors.Is(err, ErrInvalidInvocation) || !errors.As(err, &arg) || arg.Name != "note" || arg.Limit != 8192 || !strings.Contains(err.Error(), "note: exceeds 8192 bytes") {
		t.Fatalf("oversize note error %v", err)
	}
	_, _, err = s.ValidateInvocation(Invocation{Command: "diagnose", Arguments: map[string]string{"issue": "twelve"}})
	if !errors.As(err, &arg) || arg.Name != "issue" {
		t.Fatalf("invalid issue error %v", err)
	}
	// The rejected argument reaches the decode reason and the envelope error.
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "1", Text: "1" + strings.Repeat("y", 8999)}}, Commands: s.ConversationCommands()}
	raw := []byte(`{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","source_id":"1","start":0,"end":1},{"name":"note","source_id":"1","start":0,"end":9000}]}`)
	_, _, err = DecodeConversationEnvelope(raw, request)
	if !errors.Is(err, ErrConversationEnvelope) || !errors.Is(err, ErrInvalidInvocation) || !strings.Contains(conversationEnvelopeReason(err), "note: exceeds 8192 bytes") {
		t.Fatalf("decode reason %v", err)
	}
	if reply := conversationCauseReply(err, time.Now()); !strings.Contains(reply, "note") || !strings.Contains(reply, "8192") {
		t.Fatalf("oversize owner reply %q", reply)
	}
}

func TestConversationUngroundedIssueCorrectionAndReply(t *testing.T) {
	// The issue number exists only in the factory's own earlier answer.
	bad := `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","value":"1100"}]}`
	provider := &factoryUnitProvider{outputs: []string{bad, bad}}
	run, at := factoryUnitRun(t, provider, "Yes ok do it")
	run.s.EnableResearch(&factoryUnitDiagnosis{}, nil)
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 {
		t.Fatalf("correction calls: %d", provider.calls)
	}
	var corrected factoryConversationPrompt
	if err := json.Unmarshal([]byte(provider.prompts[1]), &corrected); err != nil || !strings.HasPrefix(corrected.CorrectionReason, "issue: ") {
		t.Fatalf("correction reason %q %v", corrected.CorrectionReason, err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ReplyText, "issue") || !strings.Contains(reply.ReplyText, "#1100") || strings.Contains(reply.ReplyText, "format check") {
		t.Fatalf("owner reply %q", reply.ReplyText)
	}
}
