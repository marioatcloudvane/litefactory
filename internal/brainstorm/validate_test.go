package brainstorm

import (
	"encoding/json"
	"strings"
	"testing"
)

func scanResultJSON(t *testing.T, comment string, questions bool) []byte {
	t.Helper()
	result := map[string]any{
		"outcome": "completed",
		"comment": comment,
		"payload": map[string]any{"research": map[string]any{
			"request":     "how do retries work?",
			"findings":    []any{map[string]any{"id": "F1", "text": "The worker retries twice.", "evidence": []any{map[string]any{"path": "internal/worker/retry.go", "line": 12}}}},
			"decisions":   []any{map[string]any{"id": "D1", "text": "Keep two retries."}},
			"assumptions": []any{map[string]any{"id": "A1", "text": "The default config is used."}},
		}},
	}
	if questions {
		result["outcome"] = "incomplete"
		result["reason"] = "one point needs the owner"
		result["questions"] = []any{map[string]any{"id": "Q1", "audience": "business", "text": "Should retries be configurable?"}}
	}
	raw, err := json.Marshal(map[string]any{"result": result})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestValidateScanResultAcceptsResearchShape(t *testing.T) {
	r, d := ValidateScanResult(scanResultJSON(t, "The worker retries twice, then gives up.", false))
	if !d.Valid || r.Answer != "The worker retries twice, then gives up." || len(r.Research) == 0 {
		t.Fatalf("result = %+v diagnostics = %+v", r, d)
	}
	r, d = ValidateScanResult(scanResultJSON(t, "Mostly settled; one point is open.", true))
	if !d.Valid || len(r.Questions) != 1 || r.Questions[0] != "Should retries be configurable?" {
		t.Fatalf("questions = %+v %+v", r, d)
	}
}

func TestValidateScanResultReusesResearchChecks(t *testing.T) {
	raw := []byte(`{"result":{"outcome":"completed","comment":"ok","payload":{"research":{"request":"q","findings":[{"id":"1bad","text":""}]}}}}`)
	if _, d := ValidateScanResult(raw); d.Valid || len(d.Diagnostics) < 2 {
		t.Fatalf("diagnostics = %+v", d)
	}
	raw = []byte(`{"result":{"outcome":"completed","comment":"ok","payload":{"plan":{}}}}`)
	if _, d := ValidateScanResult(raw); d.Valid {
		t.Fatalf("non-research payload accepted")
	}
}

func TestAnswerDiagnosticsOnePerRule(t *testing.T) {
	cases := map[string]string{
		"answer_length": strings.Repeat("é", MaxAnswerCodePoints+1),
		"answer_url":    "See https://example.com for more.",
		"answer_link":   "Read [the guide](guide) first.",
		"answer_table":  "| a | b |",
		"answer_hex":    "It changed in commit 3addf395.",
	}
	for code, answer := range cases {
		got := AnswerDiagnostics(answer)
		if len(got) != 1 || got[0].Code != code || got[0].Path != "$.result.comment" {
			t.Fatalf("%s: %+v", code, got)
		}
	}
	all := AnswerDiagnostics(strings.Repeat("x", MaxAnswerCodePoints) + " https://a.b [x](y) 3addf395\n| a | b |")
	if len(all) != 5 {
		t.Fatalf("all rules = %+v", all)
	}
	for _, ok := range []string{strings.Repeat("é", MaxAnswerCodePoints), "About 1500000 requests a day.", "The facade was defaced.", "Two retries, then it stops."} {
		if got := AnswerDiagnostics(ok); len(got) != 0 {
			t.Fatalf("%q: %+v", ok, got)
		}
	}
	if _, d := ValidateScanResult(scanResultJSON(t, "Read https://example.com twice.", false)); d.Valid || len(d.Diagnostics) != 1 || d.Diagnostics[0].Code != "answer_url" {
		t.Fatalf("speech rule in validator = %+v", d)
	}
}

func TestValidateScanResultRejectsOversizedResult(t *testing.T) {
	raw := []byte(`{"result":{"outcome":"completed","comment":"` + strings.Repeat("a", MaxResultBytes) + `"}}`)
	if _, d := ValidateScanResult(raw); d.Valid || d.Diagnostics[0].Code != "limit" {
		t.Fatalf("oversized = %+v", d)
	}
}
