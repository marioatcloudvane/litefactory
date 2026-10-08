package submission_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestFamilyProfile_ModeMatrix(t *testing.T) {
	installed := []string{"feedback_preflight", "issues_list", "issues_get", "issues_comment", "issues_create_prepare", "evidence_submit", "activity_update", "activity_get", "receipts_get"}
	for _, mode := range []string{"research", "implement", "validate", "plan", "spike"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			p, err := submission.NewFamilyToolProfile(mode, installed)
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
				// Drafting needs no grant.
				if tool == "issues_create_prepare" {
					want = mode == "research"
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

func TestFamilyProfile_RegistrationAndImmutability(t *testing.T) {
	t.Parallel()
	for _, names := range [][]string{{"issues_create_batch"}, {"unknown"}, {"issues_list", "issues_list"}, {"communication_submit"}} {
		if _, err := submission.NewFamilyToolProfile("research", names); err == nil {
			t.Fatalf("invalid registration accepted: %v", names)
		}
	}
	installed := []string{"issues_list", "feedback_preflight"}
	p, err := submission.NewFamilyToolProfile("research", installed)
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
		profile, err := submission.NewFamilyToolProfile("research", []string{name})
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
	installed := []string{"feedback_preflight", "issues_list", "issues_get", "issues_comment", "issues_create_prepare", "evidence_submit", "activity_update", "activity_get", "receipts_get"}
	for _, mode := range []string{"research", "spike", "plan", "implement", "validate"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			p, err := submission.NewFamilyToolProfile(mode, installed)
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

// TestDecodeToolCallAcceptsCompliantHostCalls pins the two envelope shapes that
// kept both installed hosts off the MCP path entirely. Every tool -- including
// ones whose input schema was always complete, such as receipts_get -- answered
// -32602 "Invalid tool parameters", because decodeStrict's DisallowUnknownFields
// rejected MCP's _meta member, which hosts attach a progress token to. An
// omitted arguments member stays refused: that is a deliberate rule about
// submitting no contribution, pinned by TestDecodeToolCall_RejectsAmbiguousEnvelope.
func TestDecodeToolCallAcceptsCompliantHostCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		params    string
		wantArgs  string
		wantError bool
	}{
		{"plain call", `{"name":"receipts_get","arguments":{"request_id":"x"}}`, `{"request_id":"x"}`, false},
		{"host progress token", `{"name":"receipts_get","arguments":{"request_id":"x"},"_meta":{"progressToken":1}}`, `{"request_id":"x"}`, false},
		{"empty arguments", `{"name":"activity_get","arguments":{}}`, `{}`, false},
		{"progress token with empty arguments", `{"name":"activity_get","arguments":{},"_meta":{"progressToken":"t"}}`, `{}`, false},
		{"omitted arguments still refused", `{"name":"communication_submit"}`, "", true},
		{"missing name", `{"arguments":{}}`, "", true},
		{"unknown member still refused", `{"name":"activity_get","arguments":{},"authorization":"x"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			call, err := submission.DecodeToolCall([]byte(tt.params))
			if tt.wantError {
				if err == nil {
					t.Fatal("expected refusal")
				}
				return
			}
			if err != nil {
				t.Fatalf("compliant host call refused: %v", err)
			}
			if string(call.Arguments) != tt.wantArgs {
				t.Fatalf("arguments = %s, want %s", call.Arguments, tt.wantArgs)
			}
		})
	}
}
