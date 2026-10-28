package agentresult_test

import (
	"encoding/json"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func TestGraphPlan_StorySetSizingContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, fields, reason string
		valid                bool
	}{
		{name: "omitted", valid: true},
		{name: "blank is advisory", fields: `,"size":"","size_reason":"  "`, reason: "  ", valid: true},
		{name: "invalid size is advisory", fields: `,"size":"XL","size_reason":"Small change"`, reason: "Small change", valid: true},
		{name: "280 Unicode code points", fields: `,"size":"S","size_reason":` + sizingJSON(strings.Repeat("界", 280)), reason: strings.Repeat("界", 280), valid: true},
		{name: "padding retained", fields: `,"size":"M","size_reason":"  A bounded change  "`, reason: "  A bounded change  ", valid: true},
		{name: "281 Unicode code points", fields: `,"size_reason":` + sizingJSON(strings.Repeat("é", 281))},
		{name: "line feed", fields: `,"size_reason":"First\nsecond"`},
		{name: "carriage return", fields: `,"size_reason":"First\rsecond"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, d := ar.Create(setJSON(`{"id":"s1","name":"One","intent":"One change"`+tc.fields+`}`), graphContext())
			if d.Valid != tc.valid {
				t.Fatalf("valid=%v want %v: %v", d.Valid, tc.valid, diagnosticCodes(d))
			}
			if tc.valid {
				if got := r.Payload.Plan.StorySet.Entries[0].SizeReason; got != tc.reason {
					t.Fatalf("reason changed: %q", got)
				}
			} else if !hasDiagnostic(d, "$.result.payload.plan.storyset.entries[0].size_reason", "size_reason") {
				t.Fatalf("missing shape diagnostic: %v", diagnosticCodes(d))
			}
		})
	}
}

func sizingJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestGraphPlan_StorySetSizingWrongJSONTypes(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"size", "size_reason"} {
		for _, value := range []string{`42`, `true`, `[]`, `{}`, `null`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				t.Parallel()
				_, d := ar.Create(setJSON(`{"id":"s1","name":"One","intent":"One change","`+field+`":`+value+`}`), graphContext())
				if d.Valid {
					t.Fatal("wrong JSON type accepted")
				}
			})
		}
	}
}

func TestGraphPlan_StorySetSizingDoesNotRelaxStructure(t *testing.T) {
	t.Parallel()
	sized := func(s string) string {
		return strings.TrimSuffix(s, "}") + `,"size":"S","size_reason":"One small change"}`
	}
	for name, entries := range map[string][]string{
		"duplicate ids":   {sized(entry("a", "One", "A")), sized(entry("a", "Two", "B"))},
		"duplicate names": {sized(entry("a", "One", "A")), sized(entry("b", "ONE", "B"))},
		"cycle":           {sized(entry("a", "One", "A", "b")), sized(entry("b", "Two", "B", "a"))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, d := ar.Create(setJSON(entries...), graphContext()); d.Valid {
				t.Fatal("invalid structure accepted with sizing")
			}
		})
	}
}
