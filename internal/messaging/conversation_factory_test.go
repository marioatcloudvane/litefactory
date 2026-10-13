package messaging

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

type factoryUnitProvider struct {
	outputs []string
	calls   int
	fail    bool
	prompts []string
}

func (p *factoryUnitProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	p.calls++
	p.prompts = append(p.prompts, r.Prompt)
	session := r.Session
	if session.Resume == "" {
		session.Resume = "unit-session"
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	if p.fail {
		return agent.ConversationResult{}, errors.New("provider completion unavailable")
	}
	value := p.outputs[min(p.calls-1, len(p.outputs)-1)]
	return agent.ConversationResult{Session: session, Answer: []byte(value)}, nil
}

func factoryUnitRun(t *testing.T, p *factoryUnitProvider, input ...string) (*factoryConversationRun, time.Time) {
	t.Helper()
	text := "How is the factory?"
	if len(input) > 0 {
		text = input[0]
	}
	db := naturalExecutionDB(t)
	at := time.Now().UTC()
	service := New(db, "discord", "1", []string{"2"}, nil, FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	root := filepath.Join(t.TempDir(), "sessions")
	binding := ConversationBinding{Channel: "2", Name: "owner", Provider: "claude", Model: "fixture"}
	if err := service.EnableConversations(t.Context(), ConversationOptions{Bindings: []ConversationBinding{binding}, StateRoot: root, Providers: map[string]ConversationProvider{"claude": p}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
		t.Fatal(err)
	}
	c, err := db.ReadConversation(t.Context(), "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(t.Context(), "2", "100"); err != nil {
		t.Fatal(err)
	}
	scan, err := db.ReadNaturalScan(t.Context(), "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StageNaturalScan(t.Context(), scan, store.NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "101"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "101", OwnerID: "1", Text: text, CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitConversationTurn(t.Context(), c.ID, "101", "protected prompt", at); err != nil {
		t.Fatal(err)
	}
	turn, err := db.ClaimConversationTurn(t.Context(), c.ID, "unit-worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return &factoryConversationRun{s: service, c: c, b: binding, t: turn, at: at}, at
}

func TestFactoryConversationLookupAndCorrectionShareBudget(t *testing.T) {
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"lookup","command":"health","arguments":[]}`, `{"version":1,"kind":"lookup","command":"health","arguments":[]}`, `{"version":1,"kind":"execute"}`, `{"version":1,"kind":"lookup","command":"health","arguments":[]}`}}
	run, at := factoryUnitRun(t, provider)
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 4 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, exchange := range exchanges {
		if exchange.Kind == "evidence" {
			evidence++
		}
	}
	if evidence != 2 {
		t.Fatalf("read was performed after the fourth invocation: evidence=%d", evidence)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ReplyText, "budget") {
		t.Fatalf("unexpected result %q", reply.ReplyText)
	}
}

func TestFactoryConversationNarrationFailureRetainsOutcome(t *testing.T) {
	provider := &factoryUnitProvider{fail: true}
	run, at := factoryUnitRun(t, provider)
	const saved = "Answer saved for acme/app#42."
	if err := run.narrate(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReplyText != "Saved action outcome:\n"+saved {
		t.Fatalf("saved outcome changed: %q", reply.ReplyText)
	}
	if provider.calls != 1 {
		t.Fatalf("uncertain provider input replayed: calls=%d", provider.calls)
	}
}

type factoryUnitDiagnosis struct {
	calls   int
	request DiagnosisRequest
}

func (d *factoryUnitDiagnosis) Queue(_ context.Context, request DiagnosisRequest) error {
	d.calls++
	d.request = request
	return nil
}

func TestFactoryConversationFirstCancellationSkipsProvider(t *testing.T) {
	provider := &factoryUnitProvider{fail: true}
	run, at := factoryUnitRun(t, provider, "cancel")
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatalf("cancel invoked provider %d times", provider.calls)
	}
	c, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if c.HealthReason != "" || c.SessionHandle != "" {
		t.Fatalf("cancel paused or created a session: %+v", c)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReplyText != "Cancelled the pending request." {
		t.Fatalf("cancel response %q", reply.ReplyText)
	}
}

func TestFactoryConversationAsyncDiagnosisSavedOutcome(t *testing.T) {
	const code = "1234567890abcdef"
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"The saved diagnosis is ready."}`}}
	run, at := factoryUnitRun(t, provider, code)
	diagnosis := &factoryUnitDiagnosis{}
	run.s.EnableResearch(diagnosis, nil)
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	payload := naturalPayload{Invocation: Invocation{Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "42", "note": "check current state"}}, Channel: "2", Project: "acme/app", RepoID: repo.ID}
	encoded, digest, err := naturalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: encoded, Digest: digest, Code: code, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = run.s.db.SaveNaturalDialogue(t.Context(), dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.BindNaturalPrompt(t.Context(), dialogue, "delivered-proposal"); err != nil {
		t.Fatal(err)
	}
	// The proposed action was generated in this already established native session.
	if err = run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	run.c.SessionHandle = "unit-session"
	handled, err := run.confirm(t.Context())
	if err != nil || !handled {
		t.Fatalf("confirmation handled=%v err=%v", handled, err)
	}
	if diagnosis.calls != 1 || diagnosis.request.Target != "github:acme/app#42" {
		t.Fatalf("queue calls=%d request=%+v", diagnosis.calls, diagnosis.request)
	}
	if provider.calls != 0 {
		t.Fatal("queued action made an unnecessary model call")
	}
	action, err := run.s.db.ClaimChatDiagnosis(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if action.State != "working" || action.ConversationTurnID != run.t.ID {
		t.Fatalf("async row %+v", action)
	}
	note, err := run.s.ConversationDiagnosisInput(t.Context(), action, at)
	if err != nil || note != "check current state" {
		t.Fatalf("saved exact diagnosis input %q %v", note, err)
	}

	queued, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.FinishConversationReply(t.Context(), queued, "queue-notice", at); err != nil {
		t.Fatal(err)
	}
	const saved = "Findings: the current task is waiting on the owner."
	if err = run.s.db.CompleteChatDiagnosisResult(t.Context(), "discord", action.MessageID, action.AnswerAttempts, saved, at, "success"); err != nil {
		t.Fatal(err)
	}
	if _, err = run.s.db.ClaimChatReply(t.Context(), "discord", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ordinary delivery claimed diagnosis: %v", err)
	}
	scan, err := run.s.db.ReadNaturalScan(t.Context(), "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.StageNaturalScan(t.Context(), scan, store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: "102"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "102", OwnerID: "1", Text: "What did it find?", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = run.s.db.AdmitConversationTurn(t.Context(), run.c.ID, "102", "protected followup", at); err != nil {
		t.Fatal(err)
	}
	next, err := run.s.db.ClaimConversationTurn(t.Context(), run.c.ID, "unit-worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.runFactoryConversation(t.Context(), run.c, run.b, next, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !strings.Contains(provider.prompts[0], saved) || diagnosis.calls != 1 {
		t.Fatalf("same-session handoff calls=%d queued=%d", provider.calls, diagnosis.calls)
	}
	execution, err := run.s.db.ReadNaturalExecution(t.Context(), action.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != "finished" || execution.Outcome != saved {
		t.Fatalf("execution %+v", execution)
	}
	link, err := run.s.db.GetConversationActionLink(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	if link.Outcome != saved {
		t.Fatalf("saved link %+v", link)
	}
	pending, err := run.s.db.ListPendingConversationActionOutcomes(t.Context(), run.c.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("outcome eligible again: %+v %v", pending, err)
	}
	result, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ReplyText, saved) {
		t.Fatalf("saved outcome absent %q", result.ReplyText)
	}
}

func TestFactoryConversationJournalProtectsDecodedOutput(t *testing.T) {
	const secret = "private-output-value"
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"private-output-value"}`}}
	run, at := factoryUnitRun(t, provider)
	run.s.Protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, secret, "[redacted]"), nil
	}
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, exchange := range exchanges {
		if strings.Contains(exchange.Request+exchange.Result, secret) {
			t.Fatal("decoded provider output bypassed journal protection")
		}
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReplyText != "[redacted]" {
		t.Fatalf("unprotected output %q", reply.ReplyText)
	}
}

type uncertainFactoryConfirmer struct{ calls int }

func (f *uncertainFactoryConfirmer) Confirm(ctx context.Context, _ string, _ string, _ string, _ string, _ time.Time) (string, error) {
	f.calls++
	presentation.Assess(ctx, presentation.Uncertain)
	return "Issue creation outcome is uncertain.", nil
}
func TestFactoryConversationPreservesUncertainDomainOutcome(t *testing.T) {
	const code = "1234567890abcdef"
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true}, code)
	confirmer := &uncertainFactoryConfirmer{}
	run.s.EnableFeatures(nil, confirmer)
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	preview := store.ChatIssuePreview{ID: "preview", Transport: "discord", ChannelRef: "2", SourceMessageID: "draft", Code: "12345678", ContentHash: "hash", Title: "Feature", Body: "Exact content", RepoID: repo.ID, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	if err = run.s.db.SaveChatIssuePreview(t.Context(), preview); err != nil {
		t.Fatal(err)
	}
	payload := naturalPayload{Invocation: Invocation{Command: "confirm", Arguments: map[string]string{"code": preview.Code}}, Channel: "2", Project: "acme/app", RepoID: repo.ID, PreviewID: preview.ID, PreviewHash: preview.ContentHash, Question: preview.Title + "\n" + preview.Body}
	raw, digest, err := naturalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	d := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: raw, Digest: digest, Code: code, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = run.s.db.SaveNaturalDialogue(t.Context(), d, 0); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.BindNaturalPrompt(t.Context(), d, "delivered"); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	run.c.SessionHandle = "unit-session"
	handled, err := run.confirm(t.Context())
	if err != nil || !handled {
		t.Fatalf("confirm %v %v", handled, err)
	}
	execution, err := run.s.db.ReadNaturalExecution(t.Context(), "nl-exec:"+code)
	if err != nil {
		t.Fatal(err)
	}
	if confirmer.calls != 1 || execution.State != "uncertain" || execution.Outcome != "Issue creation outcome is uncertain." {
		t.Fatalf("uncertainty lost: %+v calls=%d", execution, confirmer.calls)
	}
	reply, err := run.s.db.ReadChatReply(t.Context(), "discord", execution.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if reply.OutcomeStatus != string(presentation.Uncertain) {
		t.Fatalf("saved status %q", reply.OutcomeStatus)
	}
}
