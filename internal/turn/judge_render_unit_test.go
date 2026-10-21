package turn

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// judgeProvenanceMarkers are factory debug details that belong in the saved report
// JSON only, never in the posted judgment.
var judgeProvenanceMarkers = []string{"<details>", "Review details", "Target:", "Provider:", "Observed model:", "Provider version:", "Availability fallback:", "Evidence cutoff:", "Captured repository commit:", "digest", "sha256:", "Evidence read:", "Based on", "Execution:", "This review is advisory"}

func assertJudgeTextWithoutProvenance(t *testing.T, text string) {
	t.Helper()
	for _, marker := range judgeProvenanceMarkers {
		if strings.Contains(text, marker) {
			t.Fatalf("judgment text carries provenance %q:\n%s", marker, text)
		}
	}
}

func decodeJudgeSavedReport(t *testing.T, raw []byte) judgeSavedReport {
	t.Helper()
	var report judgeSavedReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("saved judge report: %v: %s", err, raw)
	}
	return report
}

func TestJudgeTextOmitsEvidenceInventory(t *testing.T) {
	const gap = "Path is outside the authorized evidence ceiling."
	const unavailable = 5000
	evidence := []agentresult.JudgeEvidence{
		{ID: "E-cov-1", Kind: "code", Reference: "https://example.test/a.go", Digest: "sha256:aa", Available: true},
		{ID: "E-uncited-available", Kind: "code", Reference: "https://example.test/z.go", Digest: "sha256:zz", Available: true},
	}
	for i := 0; i < unavailable; i++ {
		evidence = append(evidence, agentresult.JudgeEvidence{ID: fmt.Sprintf("E-aiteam-%04d", i), Kind: "code", Reference: fmt.Sprintf("local:.aiteam/file-%04d.md", i), Available: false, Gap: gap})
	}
	p := JudgePresentation{
		SourceName: "task", SourceReference: "graph:1", Provider: "codex", Model: "m", Thinking: "high",
		Report: agentresult.JudgeReport{
			Version: 1, Verdict: "concerns_found", Summary: "summary", Coverage: []string{"E-cov-1"},
			Findings: []agentresult.JudgeFinding{{Severity: "high", Concern: "c1", Impact: "i1", Correction: "x1", Evidence: []agentresult.JudgeCitation{{EvidenceID: "E-cov-1", StartLine: 3, EndLine: 7}}}},
		},
		Evidence: evidence,
	}
	text := JudgeText(p)
	if len(text) >= 4096 {
		t.Fatalf("rendered %d bytes for a large inventory, want the judgment only:\n%s", len(text), text)
	}
	for _, bad := range []string{"E-aiteam-", gap, "inventory", "E-cov-1", "E-uncited-available", "example.test", "graph:1"} {
		if strings.Contains(text, bad) {
			t.Fatalf("rendered evidence inventory %q:\n%s", bad, text)
		}
	}
	assertJudgeTextWithoutProvenance(t, text)
}

func TestJudgeTextLeadsWithSummaryAndNamedFindings(t *testing.T) {
	p := JudgePresentation{
		SourceName: "saved Plan", SourceReference: "result:1631", Provider: "codex", Model: "m", Thinking: "high",
		ObservedModel: "observed", ProviderVersion: "1.2.3", FallbackReason: "fallback", Cutoff: "2026-10-21T00:00:00Z", Commit: "abc123", LocalDigest: "sha256:local",
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
	summary, findings := strings.Index(text, "# Summary\n"), strings.Index(text, "# Findings\n")
	if summary != 0 || findings < summary {
		t.Fatalf("want summary, then findings:\n%s", text)
	}
	for _, want := range []string{
		"**Challenged:** saved Plan · **Verdict:** 2 concerns\n",
		"## Finding 1: No proof that learning works\n",
		"## Finding 2: Exceptions are disconnected\n",
		"**Why it matters:** The book stays empty.\n",
		"**What to change:** Add acceptance with real incidents.\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	for _, bad := range []string{"result:1631", "local:source", "observed", "1.2.3", "fallback", "2026-10-21", "abc123", "codex"} {
		if strings.Contains(text, bad) {
			t.Fatalf("judgment text carries provenance value %q:\n%s", bad, text)
		}
	}
	assertJudgeTextWithoutProvenance(t, text)
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
