package planbrief

import (
	"strings"
	"testing"
)

func TestParseOutcomeStrictContract(t *testing.T) {
	const valid = `{"version":1,"parent":"github:o/r#1","generation":1,"run_id":257,"attempt":"fresh-token","outcome":"discussion"}`
	cases := []struct {
		name, data string
		valid      bool
	}{
		{"discussion", valid, true}, {"completed", strings.Replace(valid, "discussion", "completed", 1), true},
		{"missing", "", false}, {"malformed", "{", false}, {"array", "[]", false},
		{"unknown field", strings.Replace(valid, `"version":1`, `"extra":1,"version":1`, 1), false},
		{"duplicate", strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1), false},
		{"case alias", strings.Replace(valid, `"version"`, `"Version"`, 1), false},
		{"missing attempt", strings.Replace(valid, `"attempt":"fresh-token",`, "", 1), false},
		{"null attempt", strings.Replace(valid, `"fresh-token"`, `null`, 1), false},
		{"blank attempt", strings.Replace(valid, `"fresh-token"`, `" "`, 1), false},
		{"wrong type", strings.Replace(valid, `"run_id":257`, `"run_id":"257"`, 1), false},
		{"zero generation", strings.Replace(valid, `"generation":1`, `"generation":0`, 1), false},
		{"unsupported version", strings.Replace(valid, `"version":1`, `"version":2`, 1), false},
		{"unknown outcome", strings.Replace(valid, "discussion", "waiting", 1), false},
		{"trailing object", valid + `{}`, false}, {"oversized", valid + strings.Repeat(" ", MaxOutcomeBytes), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseOutcome([]byte(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("outcome=%+v err=%v valid=%v", got, err, tc.valid)
			}
			if tc.valid && (got.Parent != "github:o/r#1" || got.RunID != 257 || got.Attempt != "fresh-token") {
				t.Fatalf("lost binding: %+v", got)
			}
		})
	}
}
func TestParseWorkingContextStrictContract(t *testing.T) {
	const valid = `{"version":1,"parent":"github:o/r#1","branch":"lf/1-work","feature":"work","scope":"scope","decisions":"decisions","assumptions":"none","questions":"why?"}`
	cases := []struct {
		name, data string
		valid      bool
	}{{"valid", valid, true}, {"unknown", strings.Replace(valid, `"version":1`, `"extra":true,"version":1`, 1), false}, {"duplicate", strings.Replace(valid, `"scope":"scope"`, `"scope":"scope","scope":"other"`, 1), false}, {"null", strings.Replace(valid, `"questions":"why?"`, `"questions":null`, 1), false}, {"missing identity", strings.Replace(valid, `"parent":"github:o/r#1",`, "", 1), false}, {"blank questions", strings.Replace(valid, "why?", " ", 1), false}, {"too large", valid + strings.Repeat(" ", MaxContextBytes), false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseWorkingContext([]byte(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("context=%+v err=%v valid=%v", got, err, tc.valid)
			}
		})
	}
}
