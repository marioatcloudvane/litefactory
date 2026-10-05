package agentresult_test

import (
	"encoding/json"
	"reflect"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func TestResearchContinuationContract(t *testing.T) {
	for _, variant := range []string{"valid", "no reason", "no findings", "questions", "completed", "blocked", "false"} {
		t.Run(variant, func(t *testing.T) {
			r, ctx := createFixture("research", "incomplete")
			r.Reason = "Trace callers"
			r.Payload.Research.Continue = true
			r.Payload.Research.Findings = []ar.Finding{{ID: "f1", Text: "SQL stays in store"}}
			switch variant {
			case "no reason":
				r.Reason = ""
			case "no findings":
				r.Payload.Research.Findings = nil
			case "questions":
				r.Questions = []ar.Question{{ID: "q1", Audience: "business", Text: "Which scope?"}}
			case "completed":
				r.Outcome = "completed"
				r.Reason = ""
			case "blocked":
				r.Outcome = "blocked"
			case "false":
				r.Payload.Research.Continue = false
			}
			d := ar.ValidateResult(r, ctx)
			want := variant == "valid" || variant == "false"
			if d.Valid != want || !d.Complete {
				t.Fatalf("valid=%v want=%v diagnostics=%+v", d.Valid, want, d)
			}
		})
	}
}

func TestDecodeResearchNode_ContinuationAgreement(t *testing.T) {
	cases := []struct {
		name      string
		response  string
		flag      bool
		wantValid bool
	}{
		{"explicit partial result/retains cumulative findings", "continue", true, true},
		{"response alone/rejects implicit continuation", "continue", false, false},
		{"done response/rejects unfinished continuation", "done", true, false},
		{"ask response/rejects continuation without owner question", "ask", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, ctx := createFixture("research", "incomplete")
			r.Reason = "Trace remaining callers"
			r.Payload.Research.Continue = tc.flag
			r.Payload.Research.Findings = []ar.Finding{
				{ID: "prior", Text: "SQL remains owned by store"},
				{ID: "new", Text: "The caller passes its context"},
			}
			want := ar.ResearchNode{Version: 1, Response: tc.response, Result: r}
			data, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			got, diagnostics := ar.DecodeResearchNode(data, ctx)
			if diagnostics.Valid != tc.wantValid || !diagnostics.Complete {
				t.Fatalf("valid=%v want=%v diagnostics=%+v", diagnostics.Valid, tc.wantValid, diagnostics)
			}
			if tc.wantValid {
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("decoded=%+v want=%+v", got, want)
				}
			} else if len(diagnostics.Diagnostics) == 0 || !reflect.DeepEqual(got, ar.ResearchNode{}) {
				t.Fatalf("rejected result retained candidate: %+v diagnostics=%+v", got, diagnostics)
			}
		})
	}
}
