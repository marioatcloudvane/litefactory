package ask_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
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
