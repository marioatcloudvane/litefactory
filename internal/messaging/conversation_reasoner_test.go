package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// reasonerUnitProvider plays both the conversation model and the reasoner.
// Reasoner calls are recognised by their private reasoner state directory.
type reasonerUnitProvider struct {
	conversation, reasoner []string
	reasonerFail           bool
	conversationPrompts    []string
	reasonerRequests       []agent.ConversationRequest
}

func (p *reasonerUnitProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, agent.ConversationError(err, "before_launch", true)
	}
	session := r.Session
	if session.Resume == "" {
		session.Resume = "unit-session"
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	if strings.HasPrefix(filepath.Base(r.StateDirectory), "reasoner-") {
		p.reasonerRequests = append(p.reasonerRequests, r)
		if p.reasonerFail {
			return agent.ConversationResult{}, agent.ConversationError(errors.New("reasoner provider unavailable"), "stream", false)
		}
		value := p.reasoner[min(len(p.reasonerRequests)-1, len(p.reasoner)-1)]
		return agent.ConversationResult{Session: session, Answer: []byte(value)}, nil
	}
	p.conversationPrompts = append(p.conversationPrompts, r.Prompt)
	value := p.conversation[min(len(p.conversationPrompts)-1, len(p.conversation)-1)]
	return agent.ConversationResult{Session: session, Answer: []byte(value)}, nil
}

const unitConsult = `{"version":1,"kind":"consult","question":"Is the factory healthy and what runs?","tried":"Nothing yet; I am unsure which lookups answer both parts."}`

func reasonerUnitRun(t *testing.T, p *reasonerUnitProvider) (*factoryConversationRun, *reasonerUnitProvider) {
	t.Helper()
	run, _ := factoryUnitRun(t, &factoryUnitProvider{})
	runtime := run.s.conversations.Load()
	runtime.providers["claude"] = p
	runtime.reasoners["claude"] = ConversationReasoner{Model: "flagship-fixture", Effort: "high"}
	return run, p
}

func runReasonerTurn(t *testing.T, run *factoryConversationRun) string {
	t.Helper()
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, run.at); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", run.at)
	if err != nil {
		t.Fatal(err)
	}
	return reply.ReplyText
}

func conversationPromptAt(t *testing.T, p *reasonerUnitProvider, i int) factoryConversationPrompt {
	t.Helper()
	var prompt factoryConversationPrompt
	if err := json.Unmarshal([]byte(p.conversationPrompts[i]), &prompt); err != nil {
		t.Fatal(err)
	}
	return prompt
}

func reasonerMeterRows(t *testing.T, db *store.DB) map[string]string {
	t.Helper()
	snapshot, err := db.NewMeterSnapshot(t.Context(), store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snapshot.Close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snapshot.Next(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	attempts := map[string]string{}
	for _, row := range rows {
		if slices.Contains(row.Diagnostics, "conversation_reasoner_usage_not_observed") {
			attempts[row.AttemptID] = row.Outcome
		}
	}
	return attempts
}

func TestConversationConsultRunsReasonerLookupsAndAnswerUsesIt(t *testing.T) {
	run, p := reasonerUnitRun(t, &reasonerUnitProvider{
		conversation: []string{unitConsult, `{"version":1,"kind":"answer","text":"The factory is healthy and nothing is running."}`},
		reasoner: []string{
			`{"version":1,"kind":"lookup","command":"health","arguments":[]}`,
			`{"version":1,"kind":"lookup","command":"status","arguments":[]}`,
			`{"version":1,"kind":"result","text":"Healthy per the health lookup; the status lookup shows no active work."}`,
		},
	})
	if reply := runReasonerTurn(t, run); reply != "The factory is healthy and nothing is running." {
		t.Fatalf("answer not delivered: %q", reply)
	}
	if len(p.reasonerRequests) != 3 || len(p.conversationPrompts) != 2 {
		t.Fatalf("reasoner calls=%d conversation calls=%d", len(p.reasonerRequests), len(p.conversationPrompts))
	}
	capture, err := run.s.restoreConversationInstructions(t.Context(), run.c.ID, run.at)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range p.reasonerRequests {
		if request.Model != "flagship-fixture" || request.Effort != "high" || request.Session.Resume != "" || request.NativeInstructions != capture.Reasoner() || capture.Reasoner() == "" {
			t.Fatalf("reasoner request not a fresh flagship session with reasoner instructions: model=%q effort=%q resume=%q", request.Model, request.Effort, request.Session.Resume)
		}
		if request.StateDirectory == run.c.StateDir {
			t.Fatal("reasoner shared the conversation's native session state")
		}
	}
	var last reasonerPrompt
	if err := json.Unmarshal([]byte(p.reasonerRequests[2].Prompt), &last); err != nil {
		t.Fatal(err)
	}
	if len(last.Lookups) != 2 || last.Lookups[0].Command != "health" || last.Lookups[1].Command != "status" || last.LookupsLeft != conversationReasonerLookups-2 || !strings.Contains(last.Question, "healthy") {
		t.Fatalf("reasoner did not see its own lookups: %+v", last)
	}
	for _, def := range last.Commands {
		if def.Effect != ReadEffect {
			t.Fatalf("reasoner was offered a change command: %s", def.Name)
		}
	}
	answerPrompt := conversationPromptAt(t, p, 1)
	if answerPrompt.Evidence == nil || answerPrompt.Evidence.Source != "factory:reasoner" || !answerPrompt.Evidence.Complete || !strings.Contains(answerPrompt.Evidence.Result, "Healthy per the health lookup") || !strings.Contains(answerPrompt.Evidence.Result, "health for factory") {
		t.Fatalf("consult result not handed to the conversation model as evidence: %+v", answerPrompt.Evidence)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, x := range exchanges {
		kinds = append(kinds, x.Kind)
	}
	if !slices.Contains(kinds, "consult") || !slices.Contains(kinds, "evidence") {
		t.Fatalf("consult not journaled: %v", kinds)
	}
	meter := reasonerMeterRows(t, run.s.db)
	if len(meter) != 3 {
		t.Fatalf("reasoner calls metered=%d, want 3", len(meter))
	}
	for id, outcome := range meter {
		if outcome != "success" {
			t.Fatalf("reasoner attempt %s outcome %q", id, outcome)
		}
	}
}

func TestConversationConsultRunsAtMostOncePerTurn(t *testing.T) {
	run, p := reasonerUnitRun(t, &reasonerUnitProvider{
		conversation: []string{unitConsult, unitConsult, `{"version":1,"kind":"answer","text":"I could not find more than the health result."}`},
		reasoner:     []string{`{"version":1,"kind":"result","text":"Nothing more can be known from the factory."}`},
	})
	if reply := runReasonerTurn(t, run); reply != "I could not find more than the health result." {
		t.Fatalf("answer not delivered: %q", reply)
	}
	if len(p.reasonerRequests) != 1 || len(p.conversationPrompts) != 3 {
		t.Fatalf("reasoner calls=%d conversation calls=%d", len(p.reasonerRequests), len(p.conversationPrompts))
	}
	if note := conversationPromptAt(t, p, 2).ConsultNote; note == "" {
		t.Fatal("second consult was not refused with a note")
	}
}

func TestConversationConsultFailureDegradesToAnswerWithGap(t *testing.T) {
	run, p := reasonerUnitRun(t, &reasonerUnitProvider{
		conversation: []string{unitConsult, `{"version":1,"kind":"answer","text":"I could not work that out: the deeper check failed."}`},
		reasonerFail: true,
	})
	if reply := runReasonerTurn(t, run); reply != "I could not work that out: the deeper check failed." {
		t.Fatalf("turn did not survive the failed consult: %q", reply)
	}
	evidence := conversationPromptAt(t, p, 1).Evidence
	if evidence == nil || evidence.Complete || evidence.Source != "factory:reasoner" || len(evidence.Gaps) != 1 || !strings.Contains(evidence.Gaps[0], "provider failed") {
		t.Fatalf("failed consult not handed over as a gap: %+v", evidence)
	}
	c, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
	if err != nil || c.HealthReason != "" {
		t.Fatalf("failed consult paused the conversation: %+v %v", c.HealthReason, err)
	}
	meter := reasonerMeterRows(t, run.s.db)
	if len(meter) != 1 {
		t.Fatalf("failed reasoner call metered=%d, want 1", len(meter))
	}
	for _, outcome := range meter {
		if outcome != "failure" {
			t.Fatalf("failed reasoner call outcome %q", outcome)
		}
	}
}

func TestConversationReasonerCannotProposeActions(t *testing.T) {
	propose := `{"version":1,"kind":"propose","command":"diagnose","arguments":[{"name":"issue","value":"42"}]}`
	changeLookup := `{"version":1,"kind":"lookup","command":"diagnose","arguments":[{"name":"issue","value":"42"}]}`
	run, p := reasonerUnitRun(t, &reasonerUnitProvider{
		conversation: []string{unitConsult, `{"version":1,"kind":"answer","text":"I have no more than that."}`},
		reasoner:     []string{propose, changeLookup},
	})
	if reply := runReasonerTurn(t, run); reply != "I have no more than that." {
		t.Fatalf("answer not delivered: %q", reply)
	}
	if len(p.reasonerRequests) != 2 {
		t.Fatalf("reasoner calls=%d, want the call and one correction", len(p.reasonerRequests))
	}
	var corrected reasonerPrompt
	if err := json.Unmarshal([]byte(p.reasonerRequests[1].Prompt), &corrected); err != nil {
		t.Fatal(err)
	}
	if corrected.CorrectionReason == "" {
		t.Fatal("rejected proposal got no correction reason")
	}
	evidence := conversationPromptAt(t, p, 1).Evidence
	if evidence == nil || evidence.Complete || !strings.Contains(evidence.Gaps[0], "format check") {
		t.Fatalf("rejected reasoner output not reported as a gap: %+v", evidence)
	}
	if _, err := run.s.db.ReadNaturalDialogue(t.Context(), run.s.transport, run.s.owner, run.c.ChannelRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reasoner output created a pending action: %v", err)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range exchanges {
		if x.Kind == "propose" {
			t.Fatal("reasoner output was journaled as a proposal")
		}
	}
}

func TestConversationConsultEnvelopeNeedsQuestionAndTried(t *testing.T) {
	request := ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "101", Text: "Why is it slow?"}}}
	if envelope, _, err := DecodeConversationEnvelope([]byte(unitConsult), request); err != nil || envelope.Kind != "consult" || envelope.Question == "" || envelope.Tried == "" {
		t.Fatalf("valid consult rejected: %+v %v", envelope, err)
	}
	for _, raw := range []string{
		`{"version":1,"kind":"consult","question":"Why?"}`,
		`{"version":1,"kind":"consult","question":" ","tried":"status"}`,
		`{"version":1,"kind":"consult","question":"Why?","tried":"status","command":"health"}`,
		`{"version":1,"kind":"consult","question":"Why?","tried":"` + strings.Repeat("x", 4001) + `"}`,
		`{"version":1,"kind":"answer","text":"Fine.","question":"Why?"}`,
	} {
		if _, _, err := DecodeConversationEnvelope([]byte(raw), request); !errors.Is(err, ErrConversationEnvelope) {
			t.Fatalf("invalid consult accepted: %s", raw)
		}
	}
}
