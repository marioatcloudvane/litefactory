package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/response"
)

func interpretationRequest() InterpretationRequest {
	return InterpretationRequest{Turns: []DialogueTurn{{SourceID: "123", Text: "draft hello"}}, Commands: []CommandDefinition{{Name: "draft", NaturalLanguage: true, Effect: ChangeEffect, Arguments: []Argument{{Name: "note", Type: TextArgument, Required: true}}}}}
}

func TestDecodeInterpretationBindsExactOwnerBytes(t *testing.T) {
	r := interpretationRequest()
	raw := []byte(`{"version":1,"kind":"propose","command":"draft","arguments":[{"name":"note","source_id":"123","start":6,"end":11}]}`)
	result, inv, err := DecodeInterpretation(raw, r)
	if err != nil || result.Kind != "propose" || inv.Arguments["note"] != "hello" {
		t.Fatalf("result=%+v invocation=%+v error=%v", result, inv, err)
	}
}

func TestDecodeInterpretationRejectsAmbiguity(t *testing.T) {
	valid := `{"version":1,"kind":"propose","command":"draft","arguments":[{"name":"note","source_id":"123","start":6,"end":11}]}`
	cases := []string{
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(valid, `"start":6`, `"start":6,"start":6`, 1),
		strings.Replace(valid, `"start":6,`, "", 1),
		strings.Replace(valid, `"end":11`, `"end":99`, 1),
		strings.Replace(valid, `"source_id":"123"`, `"source_id":"invented"`, 1),
		strings.Replace(valid, `"note"`, `"unknown"`, 1),
		strings.Replace(valid, `"draft"`, `"repair"`, 1),
		strings.Replace(valid, `"arguments":[`, `"arguments":null,"extra":[`, 1),
		valid + ` {}`, `{"version":1,"kind":"confirm"}`, `{"version":1,"kind":"ignore","question":"yes"}`,
		`{"version":1,"kind":"clarify","question":null}`, strings.Repeat(" ", interpretationLimit+1),
	}
	for i, raw := range cases {
		if _, _, err := DecodeInterpretation([]byte(raw), interpretationRequest()); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	r := interpretationRequest()
	r.Turns[0].Text = "> draft hello"
	quoted := strings.Replace(strings.Replace(valid, `"start":6`, `"start":8`, 1), `"end":11`, `"end":13`, 1)
	if _, _, err := DecodeInterpretation([]byte(quoted), r); err == nil {
		t.Error("quoted source accepted")
	}
}

type interpretationProvider struct {
	calls    int
	deadline bool
	prompt   string
	err      error
}

func (p *interpretationProvider) Present(ctx context.Context, repo int64, identity, phase, prompt string) ([]byte, error) {
	p.calls++
	p.prompt = prompt
	deadline, ok := ctx.Deadline()
	p.deadline = ok && time.Until(deadline) > 30*time.Second && time.Until(deadline) <= response.Budget
	if repo != 0 || phase != "generation" || !strings.HasPrefix(identity, "interpretation-") {
		return nil, errors.New("bad ownership")
	}
	return []byte(`{"version":1,"kind":"ignore"}`), p.err
}
func TestNativeInterpreterBoundedProviderCall(t *testing.T) {
	p := &interpretationProvider{}
	n := &NativeInterpreter{Model: p}
	if _, err := n.Interpret(context.Background(), interpretationRequest()); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || !p.deadline || !strings.Contains(p.prompt, "no tools or execution authority") {
		t.Fatalf("provider=%+v", p)
	}
	p.err = errors.New("provider unavailable")
	if _, err := n.Interpret(context.Background(), interpretationRequest()); err == nil {
		t.Error("provider failure hidden")
	}
	r := interpretationRequest()
	r.Turns[0].Text = strings.Repeat("x", 32769)
	if _, err := n.Interpret(context.Background(), r); err == nil || p.calls != 2 {
		t.Error("oversized request reached provider")
	}
}

func TestNativeInterpreterPromptReadsTyposAndFollowUps(t *testing.T) {
	p := &interpretationProvider{}
	r := interpretationRequest()
	r.PreviousReply = "If you mean the whole project, I can summarize it."
	if _, err := (&NativeInterpreter{Model: p}).Interpret(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"staud" means status`, `"livrary" means litefactory`, "never treat a typo as a cut-off", "proposes the status command without asking back", "only when two or more readings remain plausible", "previous_reply", "never a source for arguments", "16-character confirmation code", "8-character feature preview code", "12-character research question token", "confirmation authorization is deterministic", `"previous_reply":"If you mean the whole project`} {
		if !strings.Contains(p.prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}

func TestDecodeInterpretationRejectsPreviousReplySpans(t *testing.T) {
	r := interpretationRequest()
	r.PreviousReply = "Delivered proposal (context only): Confirmation code: a03a88fba8cffa35; Proposed action: draft; Project: marioatcloudvane/litefactory"
	for _, source := range []string{"previous_reply", "unknown"} {
		raw := `{"version":1,"kind":"propose","command":"draft","arguments":[{"name":"note","source_id":"` + source + `","start":6,"end":10}]}`
		if _, _, err := DecodeInterpretation([]byte(raw), r); err == nil {
			t.Errorf("span from %s accepted", source)
		}
	}
}

func TestReportedQuestionsPreserveRoutingAndTargets(t *testing.T) {
	s := New(nil, "discord", "7", nil, nil, nil)
	s.EnableAsk(nil)
	s.EnableResearch(nil, nil)
	for _, tc := range []struct {
		text, command, target string
		values                [][2]string
		reject                bool
	}{
		{text: "Which tasks are running at the moment, and which ones are waiting on me?", command: "health", target: "status"},
		{text: "Is anything stuck? Why isn't issue 649 moving?", command: "diagnose", target: "diagnose", values: [][2]string{{"issue", "649"}, {"note", "Is anything stuck? Why isn't issue 649 moving?"}}},
		{text: "What happened with bug 726, is it delivered?", command: "ask", target: "ask", values: [][2]string{{"question", "What happened with bug 726, is it delivered?"}}},
		{text: "What happened with bug 726, is it delivered?", command: "details", reject: true},
		{text: "What happened with bug 726, is it delivered?", command: "ask", values: [][2]string{{"question", "is it delivered?"}}, reject: true},
	} {
		t.Run(tc.command+tc.text, func(t *testing.T) {
			r := InterpretationRequest{Turns: []DialogueTurn{{SourceID: "1", Text: tc.text}}, Commands: s.NaturalCommands(), PreviousReply: "completed: #723"}
			raw, err := (spanInterpreter{command: tc.command, values: tc.values}).Interpret(t.Context(), r)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.values) == 0 {
				raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"arguments":[]}`)
			}
			_, inv, err := DecodeInterpretation(raw, r)
			if tc.reject {
				if err == nil {
					t.Fatalf("accepted wrong target: %+v", inv)
				}
				return
			}
			if err != nil || inv.Command != tc.target {
				t.Fatalf("%+v %v", inv, err)
			}
		})
	}
}
