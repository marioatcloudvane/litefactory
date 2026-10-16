package agentresult

import "testing"

func TestJudgeRefusesUnverifiedEvidenceAndCleanGaps(t *testing.T) {
	evidence := []JudgeEvidence{{ID: "source", Available: true, Essential: true, Lines: 4}}
	r := JudgeReport{Version: 1, Verdict: "concerns_found", Summary: "Concern", Coverage: []string{"source"}, Findings: []JudgeFinding{{Severity: "high", Concern: "Unsupported claim", Impact: "Wrong decision", Correction: "Add evidence", Evidence: []JudgeCitation{{EvidenceID: "source", StartLine: 2, EndLine: 3}}}}}
	if d := ValidateJudge(r, evidence); !d.Valid {
		t.Fatalf("valid report: %+v", d)
	}
	r.Findings[0].Evidence[0].EndLine = 5
	if d := ValidateJudge(r, evidence); d.Valid {
		t.Fatal("accepted line beyond captured bytes")
	}
	r.Verdict = "no_material_concerns"
	r.Findings = nil
	evidence = append(evidence, JudgeEvidence{ID: "repository", Essential: true})
	if d := ValidateJudge(r, evidence); d.Valid {
		t.Fatal("essential missing evidence became clean")
	}
	r.Verdict = "unable_to_assess"
	r.Uncertainty = []string{"Repository unavailable"}
	if d := ValidateJudge(r, evidence); !d.Valid {
		t.Fatalf("gap assessment: %+v", d)
	}
}

func TestDecodeJudgeRejectsAuthorityAndDuplicateFields(t *testing.T) {
	for _, raw := range []string{
		`{"report":{"version":1,"verdict":"no_material_concerns","summary":"ok","coverage":["source"],"uncertainty":[],"findings":[],"approval":true}}`,
		`{"report":{"version":1,"version":1,"verdict":"no_material_concerns","summary":"ok","coverage":["source"],"uncertainty":[],"findings":[]}}`,
	} {
		if _, d := DecodeJudge([]byte(raw), []JudgeEvidence{{ID: "source", Available: true}}); d.Valid {
			t.Fatal("accepted invalid authored envelope")
		}
	}
}

func TestJudgeRequiresPinnedSourceCoverage(t *testing.T) {
	r := JudgeReport{Version: 1, Verdict: "no_material_concerns", Summary: "Reviewed code", Coverage: []string{"code"}}
	evidence := []JudgeEvidence{{ID: "source", Available: true, Essential: true}, {ID: "code", Available: true}}
	if d := ValidateJudge(r, evidence); d.Valid {
		t.Fatal("clean assessment omitted pinned source")
	}
}
