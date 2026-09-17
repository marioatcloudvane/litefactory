package turn

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
)

func TestMCPStartupFinalizationBoundary(t *testing.T) {
	t.Parallel()
	_, _, opts := agentResultFixture(t, "implement")
	service, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	const secret = "private-startup-credential-259"
	const diagnosis = "Codex could not initialize required MCP servers."
	original := &agent.RunResult{
		Outcome: agent.OutcomeFailed, ExitCode: 7, Reason: "coding agent exited with status 7",
		Diagnostic: diagnosis,
		Stderr:     "Error: required MCP servers failed to initialize: litefactory: Mcp error: -32602: Invalid tool parameters " + secret,
	}
	got, err := (&preparedMCP{service: service}).finalize(t.Context(), &preparation{}, nil, original, nil)
	mustProgress(t, err)
	if got.Outcome != original.Outcome || got.ExitCode != original.ExitCode || got.Reason != original.Reason || got.Stderr != original.Stderr {
		t.Fatalf("process cause changed during finalization: %+v", got)
	}
	if !strings.HasPrefix(got.Diagnostic, diagnosis+"\nShared final result rejected:") || !strings.Contains(got.Diagnostic, "required current-attempt output is absent") {
		t.Fatalf("MCP cause must precede missing-output consequence: %q", got.Diagnostic)
	}
	if original.Diagnostic != diagnosis {
		t.Fatal("finalization mutated the original process evidence")
	}
	receipt, err := service.Receipt(t.Context(), opts.Attempt.RunID, "terminal-"+opts.Attempt.Attempt)
	mustProgress(t, err)
	if receipt.Receipt == nil || receipt.Receipt.InputState != "rejected" || receipt.Receipt.Latest.Engineering != "pending" || receipt.Receipt.Latest.Acceptance != "pending" {
		t.Fatalf("startup failure acquired workflow completion: %+v", receipt)
	}

	meta := harness.NewMeta(agent.Request{}, "", got)
	r := redact.New(redact.Options{Environ: []string{"SERVICE_TOKEN=" + secret}})
	retainRunDiagnostics(t.Context(), Options{MCP: &MCPRuntime{Redactor: r}}, got, &meta, slog.New(slog.DiscardHandler))
	path := filepath.Join(t.TempDir(), "run.meta.json")
	mustProgress(t, harness.WriteMeta(path, meta))
	data, err := os.ReadFile(path)
	mustProgress(t, err)
	if strings.Contains(string(data), secret) {
		t.Fatal("startup credential leaked into durable metadata")
	}
	var retained harness.Meta
	mustProgress(t, json.Unmarshal(data, &retained))
	if retained.Error == nil || *retained.Error != original.Reason || retained.ExitCode != 7 || retained.Diagnostic != got.Diagnostic || !strings.Contains(retained.Stderr, redact.Replacement) || !strings.Contains(retained.Stderr, "Invalid tool parameters") {
		t.Fatalf("durable metadata lost actionable sanitized evidence: %+v", retained)
	}
}
