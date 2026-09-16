package submission_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestFamilyProfile_ModeMatrix(t *testing.T) {
	installed := []string{"feedback_preflight", "issues_list", "issues_get", "issues_comment", "issues_create_batch", "evidence_submit", "activity_update", "activity_get", "receipts_get"}
	for _, mode := range []string{"research", "implement", "validate", "plan", "spike"} {
		for _, gate := range []bool{false, true} {
			name := mode + "/gate off"
			if gate {
				name = mode + "/gate on"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p, err := submission.NewFamilyToolProfile(mode, gate, installed)
				if err != nil {
					t.Fatal(err)
				}
				catalog := map[string]submission.ToolSpec{}
				for _, spec := range p.List() {
					if _, duplicate := catalog[spec.Name]; duplicate {
						t.Fatalf("duplicate %s", spec.Name)
					}
					if !json.Valid(spec.InputSchema) || spec.Description == "" {
						t.Fatalf("invalid advertised spec: %+v", spec)
					}
					catalog[spec.Name] = spec
				}
				for _, tool := range installed {
					want := mode != "plan" && mode != "spike"
					if tool == "issues_create_batch" {
						want = mode == "research" && gate
					}
					if tool == "evidence_submit" {
						want = mode == "implement" || mode == "validate"
					}
					_, discovered := catalog[tool]
					if p.Allows(tool) != want || discovered != want {
						t.Errorf("%s: allowed=%v discovered=%v want=%v", tool, p.Allows(tool), discovered, want)
					}
				}
				for _, tool := range []string{"communication_preflight", "communication_submit", "communication_receipt"} {
					if !p.Allows(tool) {
						t.Errorf("compatibility lost: %s", tool)
					}
				}
				for _, tool := range []string{"shell", "approve", "grant", "issues_delete", "capture_execution"} {
					if p.Allows(tool) {
						t.Errorf("unknown authority granted: %s", tool)
					}
				}
			})
		}
	}
}

func TestFamilyProfile_RegistrationAndImmutability(t *testing.T) {
	t.Parallel()
	for _, names := range [][]string{{"unknown"}, {"issues_list", "issues_list"}, {"communication_submit"}} {
		if _, err := submission.NewFamilyToolProfile("research", true, names); err == nil {
			t.Fatalf("invalid registration accepted: %v", names)
		}
	}
	installed := []string{"issues_list", "feedback_preflight"}
	p, err := submission.NewFamilyToolProfile("research", false, installed)
	if err != nil {
		t.Fatal(err)
	}
	before := p.List()
	installed[0] = "issues_create_batch"
	returned := p.List()
	returned[len(returned)-1].Name = "shell"
	returned[len(returned)-1].InputSchema[0] = '!'
	if !reflect.DeepEqual(p.List(), before) || p.Allows("issues_create_batch") {
		t.Fatal("caller mutated immutable catalog")
	}
	for _, name := range []string{"feedback_preflight", "receipts_get"} {
		profile, err := submission.NewFamilyToolProfile("research", false, []string{name})
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			AdditionalProperties *bool    `json:"additionalProperties"`
			Required             []string `json:"required"`
		}
		for _, spec := range profile.List() {
			if spec.Name != name {
				continue
			}
			if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			if schema.AdditionalProperties == nil || *schema.AdditionalProperties || len(schema.Required) == 0 {
				t.Fatalf("unbounded schema for %s: %s", name, spec.InputSchema)
			}
		}
	}
}
