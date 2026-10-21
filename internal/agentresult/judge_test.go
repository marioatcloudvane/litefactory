package agentresult

import "testing"

func judgeConcern(c JudgeCitation) JudgeReport {
	return JudgeReport{Version: 1, Verdict: "concerns_found", Summary: "Concern", Findings: []JudgeFinding{{Severity: "high", Concern: "Unsupported claim", Impact: "Wrong decision", Correction: "Add evidence", Evidence: []JudgeCitation{c}}}}
}

func TestJudgeCitationsNameRepositoryOrContextFiles(t *testing.T) {
	for _, c := range []JudgeCitation{{Path: "internal/turn/judge.go", StartLine: 2, EndLine: 3}, {Path: "result.md"}, {Path: ".aiteam/plan/PLAN.md", StartLine: 4}} {
		if d := ValidateJudge(judgeConcern(c)); !d.Valid {
			t.Fatalf("refused %+v: %+v", c, d)
		}
	}
	for _, c := range []JudgeCitation{{Path: ""}, {Path: "/etc/passwd"}, {Path: "../outside.go"}, {Path: "a/../../b"}, {Path: `dir\file.go`}, {Path: "a.go", StartLine: 5, EndLine: 2}, {Path: "a.go", EndLine: 2}} {
		if d := ValidateJudge(judgeConcern(c)); d.Valid {
			t.Fatalf("accepted %+v", c)
		}
	}
	r := judgeConcern(JudgeCitation{Path: "a.go"})
	r.Coverage = []string{"a.go", "/abs.go"}
	if d := ValidateJudge(r); d.Valid {
		t.Fatal("accepted absolute coverage path")
	}
}

func TestDecodeJudgeRejectsAuthorityAndDuplicateFields(t *testing.T) {
	for _, raw := range []string{
		`{"report":{"version":1,"verdict":"no_material_concerns","summary":"ok","uncertainty":[],"findings":[],"approval":true}}`,
		`{"report":{"version":1,"version":1,"verdict":"no_material_concerns","summary":"ok","uncertainty":[],"findings":[]}}`,
	} {
		if _, d := DecodeJudge([]byte(raw)); d.Valid {
			t.Fatal("accepted invalid authored envelope")
		}
	}
	if _, d := DecodeJudge([]byte(`{"report":{"version":1,"verdict":"no_material_concerns","summary":"ok","uncertainty":[],"findings":[]}}`)); !d.Valid {
		t.Fatalf("refused a clean report without coverage: %+v", d)
	}
}

func TestJudgeVerdictShape(t *testing.T) {
	r := judgeConcern(JudgeCitation{Path: "a.go"})
	r.Findings[0].Title = "line one\nline two"
	if d := ValidateJudge(r); d.Valid {
		t.Fatal("multi-line title accepted")
	}
	r = JudgeReport{Version: 1, Verdict: "unable_to_assess", Summary: "Blind"}
	if d := ValidateJudge(r); d.Valid {
		t.Fatal("unable to assess without uncertainty accepted")
	}
	r.Uncertainty = []string{"The repository could not be checked out."}
	if d := ValidateJudge(r); !d.Valid {
		t.Fatalf("refused: %+v", d)
	}
}
