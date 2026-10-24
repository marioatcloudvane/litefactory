package messaging

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestFactoryConversationTwoCorrectionsAcrossLookup(t *testing.T) {
	p := &factoryUnitProvider{outputs: []string{
		`{"version":1,"kind":"execute"}`,
		`{"version":1,"kind":"lookup","command":"health","arguments":[]}`,
		`{"version":1,"kind":"execute"}`,
		`{"version":1,"kind":"execute"}`,
	}}
	r, at := factoryUnitRun(t, p)
	if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
		t.Fatal(err)
	}
	if p.calls != 4 {
		t.Fatalf("calls=%d", p.calls)
	}
	rows, err := r.s.db.ListConversationExchanges(t.Context(), r.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	corrections, evidence := 0, 0
	for _, row := range rows {
		if row.Kind == "correction" {
			corrections++
			var prompt factoryConversationPrompt
			if err := json.Unmarshal([]byte(row.Request), &prompt); err != nil || !row.Complete || prompt.Correction == "" || prompt.CorrectionReason == "" {
				t.Fatalf("unprotected correction boundary: %+v %v", row, err)
			}
		}
		if row.Kind == "evidence" {
			evidence++
		}
	}
	if corrections != 2 || evidence != 1 {
		t.Fatalf("corrections=%d evidence=%d", corrections, evidence)
	}
	var afterLookup factoryConversationPrompt
	if err := json.Unmarshal([]byte(p.prompts[2]), &afterLookup); err != nil || afterLookup.Correction != "" || afterLookup.CorrectionReason != "" {
		t.Fatalf("successful lookup retained defect feedback: %+v %v", afterLookup, err)
	}
	reply, err := r.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil || !strings.Contains(reply.ReplyText, "limit") && !strings.Contains(reply.ReplyText, "budget") {
		t.Fatalf("missing bounded failure: %+v %v", reply, err)
	}
}

type correctionPreStartProvider struct {
	delegate *factoryUnitProvider
	requests int
}

func (p *correctionPreStartProvider) RunConversation(ctx context.Context, request agent.ConversationRequest) (agent.ConversationResult, error) {
	p.requests++
	if p.requests == 2 {
		return agent.ConversationResult{}, agent.ConversationError(&ConversationAccessError{}, "before_launch", true)
	}
	return p.delegate.RunConversation(ctx, request)
}

func TestFactoryConversationPreStartRetryReusesCorrection(t *testing.T) {
	p := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"execute"}`, `{"version":1,"kind":"answer","text":"The request is understood."}`}}
	r, at := factoryUnitRun(t, p)
	provider := &correctionPreStartProvider{delegate: p}
	r.s.conversations.Load().providers["claude"] = provider
	if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
		t.Fatal(err)
	}
	if worked, err := r.s.ConversationStep(t.Context(), at.Add(2*time.Minute)); err != nil || !worked {
		t.Fatalf("retry: worked=%t %v", worked, err)
	}
	rows, err := r.s.db.ListConversationExchanges(t.Context(), r.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	corrections, providers := 0, 0
	for _, row := range rows {
		if row.Kind == "correction" {
			corrections++
		}
		if row.Kind == "provider" {
			providers++
		}
	}
	if corrections != 1 || providers != 2 || p.calls != 2 || provider.requests != 3 {
		t.Fatalf("reservation charged again: corrections=%d providers=%d calls=%d requests=%d", corrections, providers, p.calls, provider.requests)
	}
}

func TestFactoryConversationFullJournalRefusesProvider(t *testing.T) {
	p := &factoryUnitProvider{}
	r, _ := factoryUnitRun(t, p)
	capture, err := r.s.restoreConversationInstructions(t.Context(), r.c.ID, r.at)
	if err != nil {
		t.Fatal(err)
	}
	r.ordinal = 15
	raw, err := json.Marshal(factoryConversationPrompt{Instructions: capture.Factory()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.invoke(t.Context(), string(raw)); err == nil || !strings.Contains(err.Error(), "slot budget") {
		t.Fatalf("full journal admission: %v", err)
	}
	if p.calls != 0 || r.calls != 0 || r.ordinal != 15 {
		t.Fatalf("provider admitted without result capacity: calls=%d state=%+v", p.calls, r)
	}
}

func TestFactoryConversationRejectsInvalidCorrectionBoundary(t *testing.T) {
	for _, test := range []struct {
		name     string
		complete bool
		request  string
	}{
		{name: "incomplete", request: `{}`},
		{name: "malformed", complete: true, request: `{`},
		{name: "wrong instructions", complete: true, request: `{"instructions":"replacement","correction":"invented"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, _ := factoryUnitRun(t, &factoryUnitProvider{})
			if err := r.s.db.SaveConversationExchange(t.Context(), r.t, store.ConversationExchange{TurnID: r.t.ID, Ordinal: 1, Kind: "correction", Complete: test.complete, Request: test.request, CapturedAt: r.at}); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := r.resumeBoundary(t.Context(), &factoryConversationPrompt{}); err == nil {
				t.Fatal("invalid correction boundary accepted")
			}
		})
	}
}

func TestFactoryConversationSavedOutcomeSurvivesInstructionPause(t *testing.T) {
	r, at := factoryUnitRun(t, &factoryUnitProvider{})
	r.savedOutcomes = "The confirmed action was saved."
	if err := r.s.db.SetConversationHealth(t.Context(), r.c.ID, "retained instructions invalid", at); err != nil {
		t.Fatal(err)
	}
	if err := r.failed(t.Context(), errConversationInstructions); err != nil {
		t.Fatal(err)
	}
	reply, err := r.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil || !strings.Contains(reply.ReplyText, r.savedOutcomes) || !strings.Contains(reply.ReplyText, "could not be completed") || !strings.Contains(reply.ReplyText, "No action was repeated") {
		t.Fatalf("known outcome or limitation lost: %+v %v", reply, err)
	}
}
