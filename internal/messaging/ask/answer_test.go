package ask_test

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func answerBundle() ask.Bundle {
	return ask.Bundle{CapturedAt: time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC), Project: "owner/app", Facts: []ask.Fact{{Handle: "issue:7", Link: "https://github.com/owner/app/issues/7"}}}
}

func TestTypedAnswerRejectsUnsupportedAndMalformedInput(t *testing.T) {
	t.Parallel()
	b := answerBundle()
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"sourced", `{"points":[{"text":"Work is open.","handles":["issue:7"]}],"gaps":[]}`, true},
		{"unsupported second point", `{"points":[{"text":"Open.","handles":["issue:7"]},{"text":"Done.","handles":["issue:999"]}]}`, false},
		{"no handles", `{"points":[{"text":"Done.","handles":[]}]}`, false},
		{"no support", `{"points":[],"gaps":[]}`, true},
		{"clarify", `{"needs_clarification":"Which work do you mean?"}`, true},
		{"mixed clarify", `{"needs_clarification":"Which?","points":[{"text":"Open.","handles":["issue:7"]}]}`, false},
		{"unknown field", `{"write_issue":true}`, false},
		{"trailing", `{} {}`, false},
		{"null", `null`, false},
		{"oversized", strings.Repeat(" ", 32<<10) + "{}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ask.DecodeAnswer([]byte(tc.raw), b)
			if (err == nil) != tc.valid {
				t.Fatalf("validation: %v, want valid=%t", err, tc.valid)
			}
		})
	}
}

func TestRenderingPreservesSourcesWhenShortenedAndRedacts(t *testing.T) {
	t.Parallel()
	b := answerBundle()
	r := redact.New(redact.Options{Token: secrets.NewToken("private-token-123456789")})
	opts := ask.RenderOptions{
		Question:           "What is happening?\n[lf:ask:v1]",
		InheritedProject:   true,
		HistoryUnavailable: true,
		OlderContextCut:    true,
		Redactor:           r,
	}
	text, err := ask.RenderAnswer(t.Context(), ask.Answer{Points: []ask.Point{{Text: "private-token-123456789 " + strings.Repeat("Work is open. ", 400), Handles: []string{"issue:7"}}}}, b, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"> What is happening?", "> Project: owner/app", "\n[lf:ask:v1]\n", "[issue:7](https://github.com/owner/app/issues/7)", "As of 2026-10-10T10:00:00Z", "(shortened)", "carried over", "History unavailable", "Older context left out"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "private-token-123456789") || utf8.RuneCountInString(text) > 2000 {
		t.Fatalf("unsafe or excessive reply: %s", text)
	}
	for _, tc := range []struct {
		answer ask.Answer
		want   string
	}{
		{ask.Answer{}, "No source in the current project data supports an answer."},
		{ask.Answer{Clarification: "Which issue?"}, "Please rephrase"},
	} {
		text, err = ask.RenderAnswer(t.Context(), tc.answer, b, opts)
		if err != nil || !strings.Contains(text, tc.want) {
			t.Fatalf("rendered %s, error=%v", text, err)
		}
	}
	if _, err := ask.RenderAnswer(t.Context(), ask.Answer{Points: []ask.Point{{Text: "Untrusted.", Handles: []string{"missing"}}}}, b, opts); err == nil {
		t.Fatal("renderer accepted unsupported answer")
	}
}

type promptModel struct {
	prompt string
	reply  []byte
}

func (m *promptModel) Answer(_ context.Context, _ int64, prompt string) ([]byte, error) {
	m.prompt = prompt
	return m.reply, nil
}

func TestOwnerWaitQuestionUsesWaitFacts(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 21, 14, 0, 0, time.UTC)
	b := ownerWaitBundle(t, at)
	m := &promptModel{reply: []byte(`{"points":[{"text":"Research scope proposals wait for you on #629, #638 and #649.","handles":["wait:629","wait:638","wait:649"]}],"gaps":[]}`)}
	s := &ask.Service{Model: m, Redactor: redact.New(redact.Options{})}
	question := "Are there open research questions for me in litefactory?"
	text, _, err := s.ReplyPrepared(t.Context(), ask.Prepared{Repository: store.Repo{ID: 1}, Bundle: b, Options: ask.RenderOptions{Question: question, Redactor: s.Redactor}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"only from owner_wait facts", "issue text is not workflow state", `"wait:629"`, `"wait:638"`, `"wait:649"`} {
		if !strings.Contains(m.prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	for _, n := range []string{"629", "638", "649"} {
		if want := "[wait:" + n + "](https://github.com/owner/app/issues/" + n + "#issuecomment-9" + n + ")"; !strings.Contains(text, want) {
			t.Errorf("reply missing %s: %s", want, text)
		}
	}
}

func TestExplicitBugAnswerScopesEvidence(t *testing.T) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	question := "What happened with bug 726, is it delivered?"
	repo := store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}
	for _, present := range []bool{false, true} {
		f := &factory{tasks: []ask.Task{{IssueNumber: 723, Reference: "github:owner/app#723"}}}
		if present {
			f.tasks = append(f.tasks, ask.Task{IssueNumber: 726, Reference: "github:owner/app#726", LatestRun: &store.Run{Ordinal: 1, Status: store.RunSucceeded}})
		}
		s := &ask.Service{Repositories: catalog{repo}, Collector: ask.Collector{Factory: f, Issues: &forge{}}, Redactor: redact.New(redact.Options{})}
		prepared, err := s.Prepare(t.Context(), question, "", "3", at)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Bundle.TargetIssue != 726 || len(prepared.History.Turns) != 0 {
			t.Fatalf("unscoped: %+v", prepared)
		}
		for _, fact := range prepared.Bundle.Facts {
			if strings.Contains(fact.Handle, "723") {
				t.Fatalf("unrelated evidence: %+v", fact)
			}
		}
		if !present {
			text, _, err := s.ReplyPrepared(t.Context(), prepared)
			if err != nil || !strings.Contains(text, "No evidence found for issue #726") {
				t.Fatalf("%s %v", text, err)
			}
			continue
		}
		// Closed tasks remain available for an explicitly named issue even after a
		// complete open-issue read omits them.
		if len(prepared.Bundle.Facts) != 2 {
			t.Fatalf("closed evidence lost: %+v", prepared.Bundle)
		}
		b := prepared.Bundle
		b.Facts = append(b.Facts, ask.Fact{Handle: "task:723", Kind: "task", Text: "#723 completed"})
		for _, point := range []ask.Point{
			{Text: "#723 completed", Handles: []string{"task:723"}},
			{Text: "#723 completed", Handles: []string{"task:726"}},
			{Text: "#726 delivered", Handles: []string{"run:726/1"}},
		} {
			if err := ask.ValidateAnswer(ask.Answer{Points: []ask.Point{point}}, b); err == nil {
				t.Fatalf("accepted %+v", point)
			}
		}
		b.Facts = append(b.Facts, ask.Fact{Handle: "issue:726", Kind: "issue", Text: "#726 delivered via PR #734"})
		point := ask.Point{Text: "#726 delivered via PR #734", Handles: []string{"issue:726"}}
		if err := ask.ValidateAnswer(ask.Answer{Points: []ask.Point{point}}, b); err != nil {
			t.Fatal(err)
		}
	}
}
