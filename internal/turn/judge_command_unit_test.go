package turn

import (
	"strings"
	"testing"
)

func TestJudgeOptionsQuotedLiteralSettings(t *testing.T) {
	got, err := ParseJudgeOptions(`--agent other --focus "Check $(echo secret) and missing evidence" --target='result:7:2:digest' --model flagship --thinking high`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "other" || got.Target != "result:7:2:digest" || got.Focus != "Check $(echo secret) and missing evidence" || got.Model != "flagship" || got.Thinking != "high" {
		t.Fatalf("unexpected parsed settings: %+v", got)
	}
	if got, err := ParseJudgeOptions(""); err != nil || got.Target != "" || got.Agent != "" {
		t.Fatalf("bare judge settings: %+v %v", got, err)
	}
}

func TestJudgeOptionsRejectAmbiguousSettings(t *testing.T) {
	for _, text := range []string{
		`--agent codex --agent=claude`, `--focus a --focus b`, `--unknown value`,
		`--agent`, `--focus --target x`, `--target=""`, `--focus 'unclosed`,
		`--agent shell`, `free text`, strings.Repeat("x", 16385),
	} {
		t.Run(text[:min(len(text), 48)], func(t *testing.T) {
			if _, err := ParseJudgeOptions(text); err == nil {
				t.Fatalf("accepted %q", text)
			}
		})
	}
}
