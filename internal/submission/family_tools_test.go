package submission_test

import (
	"encoding/json"
	"fmt"
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

// TestToolSchemasAreUsableInEveryMode guards the defect that shipped the MCP
// path unusable: communication_preflight, communication_submit and seven family
// tools advertised a bare {"type":"object"} with no properties. The transport,
// discovery and server validation all worked, so every offline test passed --
// but a host has nothing to validate an argument against, so it rejected each
// call before it was sent, and agents silently fell back to the CLI helper.
//
// A schema is usable here when it parses, is an object, and declares a
// properties member. activity_get declares an empty one because it genuinely
// takes no arguments; a missing member is what leaves the model guessing.
func TestToolSchemasAreUsableInEveryMode(t *testing.T) {
	t.Parallel()
	installed := []string{"feedback_preflight", "issues_list", "issues_get", "issues_comment", "issues_create_batch", "evidence_submit", "activity_update", "activity_get", "receipts_get"}
	for _, mode := range []string{"research", "spike", "plan", "implement", "validate"} {
		for _, creation := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/creation=%t", mode, creation), func(t *testing.T) {
				t.Parallel()
				p, err := submission.NewFamilyToolProfile(mode, creation, installed)
				if err != nil {
					t.Fatal(err)
				}
				tools := p.List()
				if len(tools) == 0 {
					t.Fatalf("%s exposes no tools", mode)
				}
				for _, tool := range tools {
					var schema struct {
						Type       string                     `json:"type"`
						Properties map[string]json.RawMessage `json:"properties"`
					}
					if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
						t.Errorf("%s: input schema does not parse: %v", tool.Name, err)
						continue
					}
					if schema.Type != "object" {
						t.Errorf("%s: input schema type = %q, want object", tool.Name, schema.Type)
					}
					if schema.Properties == nil {
						t.Errorf("%s: input schema declares no properties, so a host cannot validate any call against it", tool.Name)
					}
				}
			})
		}
	}
}
