package config

import "testing"

func TestBugAgentJudgePrecedenceCharacterization(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, shared string
		recorded, want         Kind
	}{
		{"recorded Codex", "", "", KindCodex, KindCodex},
		{"explicit second opinion", "codex", "claude", KindClaude, KindCodex},
		{"shared preference", "", "claude", KindCodex, KindClaude},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Defaults()
			claude, codex := DefaultClaude(), DefaultCodex()
			c.Agents = Agents{Claude: &claude, Codex: &codex}
			c.Judge.Agent = tc.shared
			request := JudgeOptions{Agent: tc.explicit}
			got, err := c.ResolveJudge(request, tc.recorded, tc.recorded)
			if err != nil || got.Provider != tc.want {
				t.Fatalf("judge provider = %+v, %v; want %s", got, err, tc.want)
			}
			if _, err := c.JudgeFallback(request, tc.recorded, got, "provider unavailable"); err == nil {
				t.Fatal("named or recorded provider silently fell back")
			}
		})
	}
}
