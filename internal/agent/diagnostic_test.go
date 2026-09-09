package agent

import "testing"

func TestRunResultDiagnosticClone(t *testing.T) {
	t.Parallel()
	original := &RunResult{Outcome: OutcomeFailed, ExitCode: 7, Reason: "exit status 7", Diagnostic: "provider-neutral startup guidance"}
	clone := original.Clone()
	if clone.Diagnostic != original.Diagnostic || clone.Outcome != original.Outcome || clone.ExitCode != original.ExitCode || clone.Reason != original.Reason {
		t.Fatalf("clone lost result contract: %+v", clone)
	}
	clone.Diagnostic = "changed"
	if original.Diagnostic != "provider-neutral startup guidance" || original.Succeeded() || clone.Succeeded() {
		t.Fatal("diagnostic changes affected original or failure semantics")
	}
}
