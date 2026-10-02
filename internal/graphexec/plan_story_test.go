package graphexec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestParsePlanStoryRequest_Bounds(t *testing.T) {
	base := graphexec.PlanStoryRequest{RequestID: "request-1", Operation: "create", Stage: "story", Candidate: "candidate-1", SharedRevision: "shared-1"}
	cases := []struct {
		name    string
		mutate  func(*graphexec.PlanStoryRequest)
		suffix  string
		invalid bool
	}{
		{name: "bounded creation"},
		{name: "unsupported operation", mutate: func(r *graphexec.PlanStoryRequest) { r.Operation = "delete" }, invalid: true},
		{name: "unsupported stage", mutate: func(r *graphexec.PlanStoryRequest) { r.Stage = "implementation" }, invalid: true},
		{name: "late creation", mutate: func(r *graphexec.PlanStoryRequest) { r.Stage = "technical" }, invalid: true},
		{name: "missing request identity", mutate: func(r *graphexec.PlanStoryRequest) { r.RequestID = "" }, invalid: true},
		{name: "duplicate upstream", mutate: func(r *graphexec.PlanStoryRequest) { r.Upstream = []string{"same", "same"} }, invalid: true},
		{name: "oversize reference", mutate: func(r *graphexec.PlanStoryRequest) { r.Candidate = strings.Repeat("x", 257) }, invalid: true},
		{name: "update without expected revision", mutate: func(r *graphexec.PlanStoryRequest) { r.Operation = "update" }, invalid: true},
		{name: "retirement without expected revision", mutate: func(r *graphexec.PlanStoryRequest) { r.Operation = "retire" }, invalid: true},
		{name: "trailing document", suffix: " {}", invalid: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := base
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			raw, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			_, err = graphexec.ParsePlanStoryRequest(append(raw, tt.suffix...))
			if (err != nil) != tt.invalid {
				t.Fatalf("invalid=%v, got %v", tt.invalid, err)
			}
		})
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"parent":362`, `"issue":363`, `"owner":1`, `"credentials":"secret"`} {
		t.Run("foreign authority "+field, func(t *testing.T) {
			data := append([]byte("{"+field+","), raw[1:]...)
			if _, err := graphexec.ParsePlanStoryRequest(data); err == nil {
				t.Fatal("agent authority accepted")
			}
		})
	}
	if _, err := graphexec.ParsePlanStoryRequest([]byte(strings.Repeat(" ", 128*1024+1))); err == nil {
		t.Fatal("oversize input accepted")
	}
}

func TestPlanStoryRequest_CreateRefusalsNameTheirField(t *testing.T) {
	create := graphexec.PlanStoryRequest{RequestID: "request-1", Operation: "create", Stage: "technical", Candidate: "candidate-1", SharedRevision: "shared-1", ExpectedRevision: strings.Repeat("a", 64), ChangedSections: []string{"conversation"}}
	faults := create.Faults()
	got := map[string]string{}
	for _, f := range faults {
		got[f.Field] = f.Message
	}
	for field, advice := range map[string]string{"changed_sections": "remove changed_sections", "expected_revision": "remove expected_revision", "stage": "use operation update"} {
		if !strings.Contains(got[field], advice) || strings.Contains(got[field], "initial story candidate") {
			t.Errorf("%s message %q lacks %q", field, got[field], advice)
		}
	}
	if len(faults) != 3 {
		t.Fatalf("want three separate faults, got %v", faults)
	}
	update := graphexec.PlanStoryRequest{RequestID: "request-1", Operation: "update", Stage: "story", Candidate: "candidate-1", SharedRevision: "shared-1", ExpectedRevision: strings.Repeat("a", 64), ChangedSections: []string{"conversation"}}
	if faults := update.Faults(); len(faults) != 0 {
		t.Fatalf("update with a section list refused: %v", faults)
	}
	if err := create.Validate(); err == nil || !strings.Contains(err.Error(), "changed_sections") {
		t.Fatalf("Validate hides the faults: %v", err)
	}
}
