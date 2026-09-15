package agent_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestMCPLaunchSpec_CatalogValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		tools   []string
		invalid bool
	}{
		{"installed catalog/preserves exact order", []string{"receipts_get", "communication_preflight", "evidence_submit"}, false},
		{"omitted catalog/preserves neutral legacy value", nil, false},
		{"duplicate/rejected", []string{"receipts_get", "receipts_get"}, true},
		{"empty name/rejected", []string{""}, true},
		{"wildcard/rejected", []string{"mcp__litefactory__*"}, true},
		{"permission grammar/rejected", []string{"Bash(*)"}, true},
		{"qualified provider name/rejected", []string{"mcp.litefactory.receipts_get"}, true},
		{"newline/rejected", []string{"receipts_get\nevidence_submit"}, true},
		{"nul/rejected", []string{"receipts_get\x00"}, true},
		{"non ASCII/rejected", []string{"réceipts_get"}, true},
		{"oversized name/rejected", []string{strings.Repeat("a", 129)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec, err := agent.NewMCPLaunchSpec("/factory/litefactory", "/private/descriptor.json", "attempt", tc.tools...)
			if tc.invalid {
				if err == nil || spec != nil {
					t.Fatalf("invalid catalog accepted: spec=%v error=%v", spec, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := spec.PermittedTools(); !reflect.DeepEqual(got, tc.tools) {
				t.Fatalf("catalog = %v, want %v", got, tc.tools)
			}
		})
	}
}

func TestMCPLaunchSpec_CatalogOwnership(t *testing.T) {
	t.Parallel()
	input := []string{"communication_preflight", "communication_submit", "communication_receipt"}
	want := append([]string(nil), input...)
	spec, err := agent.NewMCPLaunchSpec("/factory/litefactory", "/private/descriptor.json", "attempt", input...)
	if err != nil {
		t.Fatal(err)
	}
	cloned := spec.Clone()
	input[0] = "evidence_submit"
	spec.PermittedTools()[1] = "issues_comment"
	cloned.PermittedTools()[2] = "issues_create"
	for _, value := range []*agent.MCPLaunchSpec{spec, cloned} {
		if got := value.PermittedTools(); !reflect.DeepEqual(got, want) {
			t.Fatalf("caller mutation changed installed catalog: %v, want %v", got, want)
		}
		if err := value.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMCPLaunchSpec_StructuredSyntaxPaths(t *testing.T) {
	t.Parallel()
	executable := "/factory/quoted \" München $(command)\\bin/litefactory"
	descriptor := "/private/quoted \" München $(command)\\dir/descriptor.json"
	spec, err := agent.NewMCPLaunchSpec(executable, descriptor, "attempt", "receipts_get")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Executable() != executable || spec.DescriptorPath() != descriptor || !reflect.DeepEqual(spec.Arguments(), []string{"mcp", "serve", "--descriptor", descriptor}) {
		t.Fatal("structured path values changed")
	}
}
