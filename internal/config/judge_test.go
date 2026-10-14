package config

import (
	"strings"
	"testing"
)

func TestJudgeSettingsAndFallback(t *testing.T) {
	c := Defaults()
	claude := DefaultClaude()
	codex := DefaultCodex()
	c.Agents = Agents{Claude: &claude, Codex: &codex}
	c.Judge = JudgeSettings{JudgeOptions: JudgeOptions{Agent: "other"}, Providers: map[Kind]JudgeProviderSettings{KindClaude: {Thinking: "high", Aliases: map[string]string{"review": "opus"}}, KindCodex: {Thinking: "medium"}}}
	got, err := c.ResolveJudge(JudgeOptions{}, KindClaude, KindClaude)
	if err != nil || got.Provider != KindCodex || got.Thinking != "medium" {
		t.Fatalf("opposite provider defaults: %+v %v", got, err)
	}
	fallback, err := c.JudgeFallback(JudgeOptions{}, KindClaude, got, "configured provider unavailable")
	if err != nil || fallback.Provider != KindClaude || fallback.Thinking != "high" || fallback.FallbackReason == "" {
		t.Fatalf("fallback defaults: %+v %v", fallback, err)
	}
	if _, err = c.JudgeFallback(JudgeOptions{}, KindClaude, fallback, "again"); err == nil {
		t.Fatal("second fallback accepted")
	}
	override, err := c.ResolveJudge(JudgeOptions{Agent: "claude", Model: "review", Thinking: "max"}, KindCodex, KindCodex)
	if err != nil || override.Model != "opus" || override.Thinking != "max" {
		t.Fatalf("owner override: %+v %v", override, err)
	}
	if _, err = c.ResolveJudge(JudgeOptions{}, KindClaude, ""); err == nil {
		t.Fatal("unknown source accepted for other")
	}
	got, err = c.ResolveJudge(JudgeOptions{Model: "gpt-6-astra", Thinking: "xhigh"}, KindClaude, KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.JudgeFallback(JudgeOptions{Model: "gpt-6-astra", Thinking: "xhigh"}, KindClaude, got, "unavailable"); err == nil {
		t.Fatal("fallback discarded explicit incompatible model/effort")
	}
}

func TestJudgeLeavesThinkingUnsetAndRejectsUnsupported(t *testing.T) {
	c := Defaults()
	claude := DefaultClaude()
	c.Agents.Claude = &claude
	got, err := c.ResolveJudge(JudgeOptions{}, KindClaude, KindClaude)
	if err != nil || got.Thinking != "" {
		t.Fatalf("unset thinking: %+v %v", got, err)
	}
	for _, tc := range []struct {
		p Kind
		m string
		v Thinking
	}{{KindClaude, "claude-3-5-sonnet", "high"}, {KindClaude, "invented-opus", "max"}, {KindCodex, "gpt-4o", "high"}, {KindCodex, "gpt-made-up", "xhigh"}, {KindCodex, "gpt-6-astra", "minimal"}, {KindCodex, "gpt-6-luna", "ultra"}, {KindClaude, "claude-opus-4-6", "xhigh"}} {
		if err := ValidateThinking(tc.p, tc.m, tc.v); err == nil {
			t.Errorf("unsupported %+v accepted", tc)
		}
	}
}

func TestJudgeOtherUnconfiguredProviderResolvesBeforeAvailabilityFallback(t *testing.T) {
	c := Defaults()
	claude := DefaultClaude()
	c.Agents = Agents{Claude: &claude}
	request := JudgeOptions{Agent: "other"}
	selected, err := c.ResolveJudge(request, KindClaude, KindClaude)
	if err != nil || selected.Provider != KindCodex {
		t.Fatalf("availability decided during settings resolution: %+v %v", selected, err)
	}
	fallback, err := c.JudgeFallback(request, KindClaude, selected, "opposite provider is not available")
	if err != nil || fallback.Provider != KindClaude || fallback.FallbackReason == "" {
		t.Fatalf("fallback: %+v %v", fallback, err)
	}
}

func TestJudgeExactModelEffortCapabilities(t *testing.T) {
	for _, tc := range []struct {
		p Kind
		m string
		v Thinking
	}{
		{KindClaude, "opus", "xhigh"}, {KindClaude, "sonnet", "max"}, {KindClaude, "haiku", "high"},
		{KindClaude, "claude-opus-4-6", "max"}, {KindClaude, "claude-sonnet-5-5", "xhigh"},
		{KindCodex, "gpt-6-astra", "ultra"}, {KindCodex, "gpt-6.1-sol", "max"}, {KindCodex, "gpt-5.6-luna", "max"},
	} {
		if err := ValidateThinking(tc.p, tc.m, tc.v); err != nil {
			t.Errorf("verified capability %+v: %v", tc, err)
		}
	}
	if err := ValidateThinking(KindCodex, "unknown-literal", ""); err != nil {
		t.Fatal("native-default literal refused", err)
	}
}

func TestJudgeSharedUltraDefaultMatchesRequestOverride(t *testing.T) {
	shared, err := Decode(strings.NewReader("agents:\n  codex: {}\njudge:\n  agent: codex\n  thinking: ultra\n"))
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := shared.validateJudge(); len(diagnostics) != 0 {
		t.Fatalf("shared ultra default rejected: %v", diagnostics)
	}
	inherited, err := shared.ResolveJudge(JudgeOptions{}, KindCodex, KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	explicitConfig, err := Decode(strings.NewReader("agents:\n  codex: {}\njudge:\n  agent: codex\n"))
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := explicitConfig.ResolveJudge(JudgeOptions{Thinking: "ultra"}, KindCodex, KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	if inherited != explicit || inherited.Thinking != "ultra" {
		t.Fatalf("default=%+v request=%+v", inherited, explicit)
	}
	if _, err = shared.ResolveJudge(JudgeOptions{Agent: "claude"}, KindCodex, KindClaude); err == nil {
		t.Fatal("inherited ultra accepted for incompatible Claude provider")
	}
	incompatible, err := Decode(strings.NewReader("agents:\n  claude: {}\njudge:\n  providers:\n    claude:\n      thinking: ultra\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(incompatible.validateJudge()) == 0 {
		t.Fatal("provider-scoped incompatible effort passed configuration validation")
	}
	if _, err = incompatible.ResolveJudge(JudgeOptions{}, KindClaude, KindClaude); err == nil {
		t.Fatal("provider-scoped incompatible effort passed invocation resolution")
	}
}
