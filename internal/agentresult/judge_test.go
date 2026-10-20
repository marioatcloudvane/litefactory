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

func TestJudgeConcernsSurviveMissingEssentialEvidence(t *testing.T) {
	evidence := []JudgeEvidence{{ID: "source", Available: true, Lines: 3}, {ID: "comment:9", Essential: true}}
	r := JudgeReport{Version: 1, Verdict: "concerns_found", Summary: "Two stories lack acceptance proof.", Coverage: []string{"source"},
		Findings: []JudgeFinding{{Title: "No proof that learning works", Severity: "high", Concern: "c", Impact: "i", Correction: "x", Evidence: []JudgeCitation{{EvidenceID: "source", StartLine: 1, EndLine: 2}}}}}
	if d := ValidateJudge(r, evidence); d.Valid {
		t.Fatal("concerns with an undisclosed essential gap were accepted")
	}
	r.Uncertainty = []string{"comment:9 was too long to capture, so its requirements were not checked."}
	if d := ValidateJudge(r, evidence); !d.Valid {
		t.Fatalf("concerns with a disclosed gap were refused: %+v", d)
	}
	r.Findings[0].Title = "line one\nline two"
	if d := ValidateJudge(r, evidence); d.Valid {
		t.Fatal("multi-line title accepted")
	}
}
