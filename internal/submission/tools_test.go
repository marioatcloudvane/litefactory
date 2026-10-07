package submission_test

import (
	_ "embed"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

//go:embed tools.go
var toolSource string

func TestToolProfile_ModeCatalog(t *testing.T) {
	for _, mode := range []string{"research", "spike", "plan", "implement", "validate"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			profile, err := submission.NewToolProfile(mode)
			if err != nil {
				t.Fatal(err)
			}
			got := profile.List()
			want := []string{"communication_preflight", "communication_receipt", "communication_submit"}
			names := make([]string, 0, len(got))
			for _, tool := range got {
				names = append(names, tool.Name)
				if !profile.Allows(tool.Name) || tool.Description == "" || !json.Valid(tool.InputSchema) {
					t.Fatalf("unusable advertised tool: %+v", tool)
				}
				expectedEffect := "read"
				if tool.Name == "communication_submit" {
					expectedEffect = "input"
				}
				if tool.Effect != expectedEffect {
					t.Fatalf("%s effect=%q want %q", tool.Name, tool.Effect, expectedEffect)
				}
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("catalog=%v want=%v", names, want)
			}
			for _, name := range []string{"", "communication_status", "shell", "run_tests", "capture_execution", "create_issue", "create_subissue", "waive", "accept", "dispatch", "change_mode", "communication_submit_create", "tools/call"} {
				if profile.Allows(name) {
					t.Errorf("forbidden direct operation permitted: %s", name)
				}
			}
			// Client-side catalog changes cannot modify the immutable server policy.
			got[0].Name = "shell"
			got[0].Effect = "creation"
			got[0].InputSchema[0] = 'x'
			fresh := profile.List()
			if fresh[0].Name != want[0] || !json.Valid(fresh[0].InputSchema) || profile.Allows("shell") {
				t.Fatal("caller changed server catalog")
			}
		})
	}
}

func TestToolProfile_UnassignedMode(t *testing.T) {
	for _, mode := range []string{"", "Research", "unknown", "validate,implement", " shell "} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			p, err := submission.NewToolProfile(mode)
			if err == nil || len(p.List()) != 0 || p.Allows("communication_submit") {
				t.Fatalf("unassigned mode obtained authority: %q", mode)
			}
		})
	}
	var zero submission.ToolProfile
	if len(zero.List()) != 0 || zero.Allows("communication_receipt") {
		t.Fatal("zero profile grants authority")
	}
}

func TestDecodeToolCall_PreservesContribution(t *testing.T) {
	t.Parallel()
	contribution := "{\n  \"result\": {\"comment\":\"spaces  and \\u0061\", \"number\":1e2}\n}"
	input := []byte(`{"name":"communication_submit","arguments":` + contribution + `}`)
	q, err := submission.DecodeToolCall(input)
	if err != nil || q.Name != "communication_submit" || string(q.Arguments) != contribution {
		t.Fatalf("bytes changed: %q error=%v", q.Arguments, err)
	}
}

func TestDecodeToolCall_RejectsAmbiguousEnvelope(t *testing.T) {
	cases := []struct{ name, input string }{
		{"missing name", `{"arguments":{}}`},
		{"missing arguments", `{"name":"communication_submit"}`},
		{"duplicate name", `{"name":"communication_receipt","name":"communication_submit","arguments":{}}`},
		{"duplicate selector", `{"name":"communication_submit","arguments":{"task":"own","task":"foreign"}}`},
		{"authority outside contribution", `{"name":"communication_submit","arguments":{},"mode":"research"}`},
		{"numeric name", `{"name":42,"arguments":{}}`},
		{"trailing document", `{"name":"communication_submit","arguments":{}} {}`},
		{"excessive depth", `{"name":"communication_submit","arguments":` + strings.Repeat(`[`, 34) + `0` + strings.Repeat(`]`, 34) + `}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := submission.DecodeToolCall([]byte(tt.input)); err == nil {
				t.Fatal("ambiguous or foreign envelope accepted")
			}
		})
	}
}

func TestToolProfile_AdditivePermissionBoundary(t *testing.T) {
	t.Parallel()
	// A compile-time embedded source snapshot makes this check independent of
	// runtime disk. Host imports or global request mutations require new scope.
	source, err := parser.ParseFile(token.NewFileSet(), "tools.go", toolSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range source.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(path, "/internal/") || path == "os/exec" {
			t.Errorf("catalog crossed host/application boundary via %s", path)
		}
	}
	ast.Inspect(source, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && slices.Contains([]string{"Permissions", "Run", "StartProcess", "CommandContext", "Setenv"}, selector.Sel.Name) {
			t.Errorf("catalog manipulates external tool/process policy: %s", selector.Sel.Name)
		}
		return true
	})
}
