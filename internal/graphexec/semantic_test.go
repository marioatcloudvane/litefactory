package graphexec

import (
	"strings"
	"testing"
)

func TestSemanticDigestExecutionIdentity(t *testing.T) {
	source := `digraph { graph [team=builtin,mode=research,profile_version=1,start=work]; work [lf_action=lf_research_agent,lf_outcomes=success,lf_task=investigate,lf_model_tier=balanced,lf_limit=8]; end [lf_terminal=true]; work -> end [lf_on=success]; }`
	digest := func(s string) string {
		t.Helper()
		p, err := ParseProfile([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		d, err := SemanticDigest(p)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	base := digest(source)
	for _, tc := range []struct {
		name, source string
		same         bool
	}{
		{"formatting", "// comment\n" + strings.ReplaceAll(source, ";", ";\n"), true},
		{"task", strings.Replace(source, "investigate", "summarize", 1), false},
		{"model", strings.Replace(source, "balanced", "powerful", 1), false},
		{"limit", strings.Replace(source, "lf_limit=8", "lf_limit=7", 1), false},
		{"action", strings.Replace(source, "lf_research_agent", "lf_research_other", 1), false},
		{"edge", strings.Replace(source, "lf_on=success", "lf_on=failure", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (digest(tc.source) == base) != tc.same {
				t.Fatal("incorrect semantic identity")
			}
		})
	}
}
