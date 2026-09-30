package agent_test

import (
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestDoctorRepairRequestSeparatesAuthorityAndCatalog(t *testing.T) {
	dir := t.TempDir()
	spec, err := agent.NewMCPLaunchSpec(filepath.Join(dir, "factory"), filepath.Join(dir, "descriptor.json"), "attempt", agent.RepairToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.RepairRequest{Operation: "operation", Invocation: agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "approved scope", WorkingDirectory: dir, MCPLaunch: spec}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.Invocation.Validate() == nil {
		t.Fatal("writing tools accepted as diagnosis")
	}
	for _, operation := range []string{"", "\t", "operation\nforeign"} {
		bad := request
		bad.Operation = operation
		if bad.Validate() == nil {
			t.Fatalf("invalid operation %q", operation)
		}
	}
	for _, tools := range [][]string{{"communication_preflight", "communication_receipt", "communication_submit"}, append(agent.RepairToolNames(), "shell"), {"doctor_backup", "doctor_write", "doctor_verify"}} {
		bad := request
		bad.Invocation.MCPLaunch, err = agent.NewMCPLaunchSpec(spec.Executable(), spec.DescriptorPath(), "attempt", tools...)
		if err != nil {
			t.Fatal(err)
		}
		if bad.Validate() == nil {
			t.Fatalf("invalid repair catalog accepted: %v", tools)
		}
	}
	names := agent.RepairToolNames()
	names[0] = "shell"
	if err := request.Validate(); err != nil {
		t.Fatalf("caller mutated authority catalog: %v", err)
	}
}
