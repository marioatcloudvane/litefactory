package agent_test

import (
	_ "embed"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

//go:embed mcp.go
var mcpLaunchSource string

func TestMCPLaunchSpec_ImmutableNeutralValue(t *testing.T) {
	t.Parallel()
	spec, err := agent.NewMCPLaunchSpec("/factory/bin/litefactory", "/private/attempt/descriptor.json", "existing-attempt")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mcp", "serve", "--descriptor", "/private/attempt/descriptor.json"}
	if spec.Executable() != "/factory/bin/litefactory" || spec.DescriptorPath() != want[3] || spec.AttemptReference() != "existing-attempt" || !reflect.DeepEqual(spec.Arguments(), want) {
		t.Fatal("wrong neutral launch values")
	}
	arguments := spec.Arguments()
	arguments[0] = "shell"
	arguments[3] = "/foreign/descriptor.json"
	cloned := spec.Clone()
	copyArguments := cloned.Arguments()
	copyArguments[1] = "create"
	if cloned == spec || !reflect.DeepEqual(spec.Arguments(), want) || !reflect.DeepEqual(cloned.Arguments(), want) {
		t.Fatal("argument or request copy aliases authority")
	}
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		request := agent.Request{Mode: mode, Permissions: agent.Permissions{Profile: mode, Payload: struct{ Native, UnrelatedServers []string }{[]string{"allowed-native-tool"}, []string{"allowed-unrelated-server"}}}}
		before, err := json.Marshal(request.Permissions)
		if err != nil {
			t.Fatal(err)
		}
		request.MCPLaunch = spec.Clone()
		after, err := json.Marshal(request.Permissions)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("%s permission payload changed", mode)
		}
		request.MCPLaunch = nil
		after, err = json.Marshal(request.Permissions)
		if err != nil || string(before) != string(after) {
			t.Fatalf("omission changed %s permissions: %v", mode, err)
		}
	}
	var absent *agent.MCPLaunchSpec
	if absent.Clone() != nil {
		t.Fatal("nil launch allocated registration")
	}
}

func TestMCPLaunchSpec_RejectsInvalidValues(t *testing.T) {
	cases := []struct{ name, exe, path, attempt string }{
		{"NUL executable", "/factory/litefactory\x00", "/private/descriptor.json", "a"},
		{"NUL descriptor", "/factory/litefactory", "/private/\x00/descriptor.json", "a"},
		{"relative executable", "litefactory", "/private/descriptor.json", "a"},
		{"unclean executable", "/factory/../litefactory", "/private/descriptor.json", "a"},
		{"relative descriptor", "/factory/litefactory", "descriptor.json", "a"},
		{"wrong descriptor filename", "/factory/litefactory", "/private/secret", "a"},
		{"unclean descriptor", "/factory/litefactory", "/private/../descriptor.json", "a"},
		{"absent attempt", "/factory/litefactory", "/private/descriptor.json", ""},
		{"control in attempt", "/factory/litefactory", "/private/descriptor.json", "attempt\nforeign"},
		{"oversize attempt", "/factory/litefactory", "/private/descriptor.json", strings.Repeat("a", 257)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if spec, err := agent.NewMCPLaunchSpec(tt.exe, tt.path, tt.attempt); err == nil || spec != nil {
				t.Fatalf("invalid registration accepted: %+v %v", spec, err)
			}
		})
	}
	var zero agent.MCPLaunchSpec
	if zero.Validate() == nil {
		t.Fatal("zero launch accepted")
	}
}

func TestMCPLaunchSpec_NoHostOrProcessSideEffects(t *testing.T) {
	t.Parallel()
	source, err := parser.ParseFile(token.NewFileSet(), "mcp.go", mcpLaunchSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range source.Imports {
		if strings.Contains(imp.Path.Value, "/internal/") || imp.Path.Value == `"os"` || imp.Path.Value == `"os/exec"` {
			t.Errorf("neutral value imports host/effect owner: %s", imp.Path.Value)
		}
	}
	ast.Inspect(source, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if ok && (selector.Sel.Name == "Permissions" || selector.Sel.Name == "StartProcess" || selector.Sel.Name == "CommandContext" || selector.Sel.Name == "WriteFile") {
			t.Errorf("neutral construction changes permission/process/config: %s", selector.Sel.Name)
		}
		return true
	})
}
