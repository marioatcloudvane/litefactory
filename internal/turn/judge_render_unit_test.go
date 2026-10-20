package turn

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func TestJudgeTextCountsUncitedUnavailableInventory(t *testing.T) {
	const gap = "Path is outside the authorized evidence ceiling."
	const unavailable = 5000
	cited := []agentresult.JudgeEvidence{
		{ID: "E-cov-1", Kind: "code", Reference: "https://example.test/a.go", Digest: "sha256:aa", Available: true},
		{ID: "E-cov-2", Kind: "code", Reference: "https://example.test/b.go", Digest: "sha256:bb", Available: true},
		{ID: "E-cov-3", Kind: "document", Reference: "local:docs/c.md", Digest: "sha256:cc", Available: true},
		{ID: "E-cov-4", Kind: "code", Reference: "https://example.test/d.go", Digest: "sha256:dd", Available: true},
	}
	evidence := append([]agentresult.JudgeEvidence{}, cited...)
	evidence = append(evidence, agentresult.JudgeEvidence{ID: "E-uncited-available", Kind: "code", Reference: "https://example.test/z.go", Digest: "sha256:zz", Available: true})
	for i := 0; i < unavailable; i++ {
		evidence = append(evidence, agentresult.JudgeEvidence{ID: fmt.Sprintf("E-aiteam-%04d", i), Kind: "code", Reference: fmt.Sprintf("local:.aiteam/file-%04d.md", i), Available: false, Gap: gap})
	}
	p := JudgePresentation{
		SourceName: "task", SourceReference: "graph:1", Provider: "codex", Model: "m", Thinking: "high",
		Report: agentresult.JudgeReport{
			Version: 1, Verdict: "concerns_found", Summary: "summary",
			Coverage: []string{"E-cov-1", "E-cov-2", "E-cov-3", "E-cov-4"},
			Findings: []agentresult.JudgeFinding{
				{Severity: "high", Concern: "c1", Impact: "i1", Correction: "x1", Evidence: []agentresult.JudgeCitation{{EvidenceID: "E-cov-1", StartLine: 3, EndLine: 7}}},
				{Severity: "medium", Concern: "c2", Impact: "i2", Correction: "x2", Evidence: []agentresult.JudgeCitation{{EvidenceID: "E-cov-3"}}},
			},
		},
		Evidence: evidence,
	}
	text := JudgeText(p)
	if len(text) >= 65536 {
		t.Fatalf("rendered %d bytes, want under 65536", len(text))
	}
	countLine := fmt.Sprintf("- %d other captured inventory entries were unavailable; the full inventory is retained locally with this request.\n", unavailable)
	if n := strings.Count(text, countLine); n != 1 {
		t.Fatalf("count line appears %d times, want 1:\n%s", n, text)
	}
	if strings.Contains(text, "- Unavailable ") || strings.Contains(text, gap) || strings.Contains(text, "E-aiteam-") {
		t.Fatalf("rendered per-entry unavailable lines:\n%s", text)
	}
	if strings.Contains(text, "E-uncited-available") {
		t.Fatalf("rendered uncited evidence:\n%s", text)
	}
	for _, want := range []string{
		"- [E-cov-1](https://example.test/a.go); digest sha256:aa\n",
		"- [E-cov-2](https://example.test/b.go); digest sha256:bb\n",
		"- E-cov-3: local:docs/c.md; digest sha256:cc\n",
		"- [E-cov-4](https://example.test/d.go); digest sha256:dd\n",
		"- [E-cov-1](https://example.test/a.go#L3-L7) (lines 3–7); digest sha256:aa\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing reference line %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "- E-cov-3: local:docs/c.md; digest sha256:cc\n"); n != 1 {
		t.Fatalf("E-cov-3 reference rendered %d times, want once in the details: \n%s", n, text)
	}
}

func TestJudgeTextLeadsWithSummaryAndNamedFindings(t *testing.T) {
	p := JudgePresentation{
		SourceName: "saved Plan", SourceReference: "result:1631", Provider: "codex", Model: "m", Thinking: "high",
		Report: agentresult.JudgeReport{
			Version: 1, Verdict: "concerns_found", Summary: "The eleven stories cover the scope, but two promises are not tested.",
			Coverage: []string{"source"},
			Findings: []agentresult.JudgeFinding{
				{Title: "No proof that learning works", Severity: "high", Concern: "The analysts can run without producing useful rules.", Impact: "The book stays empty.", Correction: "Add acceptance with real incidents.", Evidence: []agentresult.JudgeCitation{{EvidenceID: "source", StartLine: 1, EndLine: 1}}},
				{Severity: "medium", Concern: "Exceptions are disconnected. Story 3 ignores them.", Impact: "i", Correction: "x", Evidence: []agentresult.JudgeCitation{{EvidenceID: "source"}}},
			},
		},
		Evidence: []agentresult.JudgeEvidence{{ID: "source", Kind: "source", Reference: "local:source", Digest: "sha256:aa", Available: true, Lines: 3}},
	}
	text := JudgeText(p)
	summary, findings, details := strings.Index(text, "# Summary\n"), strings.Index(text, "# Findings\n"), strings.Index(text, "<details>")
	if summary != 0 || findings < summary || details < findings {
		t.Fatalf("want summary, findings, then details:\n%s", text)
	}
	for _, want := range []string{
		"**Challenged:** saved Plan · **Verdict:** 2 concerns\n",
		"## Finding 1: No proof that learning works\n",
		"## Finding 2: Exceptions are disconnected\n",
		"**Why it matters:** The book stays empty.\n",
		"**What to change:** Add acceptance with real incidents.\n",
		"<sub>Based on: source lines 1–1</sub>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "sha256:aa") < details || strings.Index(text, "Provider: codex") < details {
		t.Fatalf("provenance must sit in the details block:\n%s", text)
	}
}

func TestJudgeTextKeepsAgentTextInert(t *testing.T) {
	p := JudgePresentation{SourceName: "task", Report: agentresult.JudgeReport{
		Version: 1, Verdict: "concerns_found",
		Summary:  "# Approved\nSee [here](https://evil.test) and ![x](https://evil.test/i.png) @marioatcloudvane <b>now</b>",
		Findings: []agentresult.JudgeFinding{{Title: "@team [click](https://evil.test)", Severity: "high", Concern: "c", Impact: "i", Correction: "x"}},
	}}
	text := JudgeText(p)
	for _, bad := range []string{"\n# Approved", "[here](", "![x](", "@marioatcloudvane", "<b>", "[click]("} {
		if strings.Contains(text, bad) {
			t.Fatalf("agent text kept %q:\n%s", bad, text)
		}
	}
}
