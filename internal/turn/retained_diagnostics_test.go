package turn

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
)

func TestRetainRunDiagnostics(t *testing.T) {
	const secret = "configured-private-value-259"
	for _, tc := range []struct {
		name          string
		outcome       agent.Outcome
		broken, empty bool
	}{
		{"configured secret", agent.OutcomeFailed, false, false},
		{"sanitizer failure", agent.OutcomeFailed, true, false},
		{"success omission", agent.OutcomeSucceeded, false, false},
		{"empty omission", agent.OutcomeFailed, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := redact.New(redact.Options{Environ: []string{"SERVICE_TOKEN=" + secret}})
			if tc.broken {
				r = &redact.Redactor{}
			}
			result := &agent.RunResult{Outcome: tc.outcome, Stderr: "MCP startup " + secret, Diagnostic: "Check MCP " + secret, Reason: "coding agent exited with status 7"}
			if tc.empty {
				result.Stderr = ""
				result.Diagnostic = ""
			}
			before := *result
			var meta harness.Meta
			retainRunDiagnostics(t.Context(), Options{MCP: &MCPRuntime{Redactor: r}}, result, &meta, slog.New(slog.DiscardHandler))
			if result.Stderr != before.Stderr || result.Diagnostic != before.Diagnostic || result.Reason != before.Reason {
				t.Fatal("raw process evidence mutated")
			}
			if tc.outcome == agent.OutcomeSucceeded || tc.empty {
				data, err := json.Marshal(meta)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(data, &fields); err != nil {
					t.Fatal(err)
				}
				if _, ok := fields["stderr"]; ok {
					t.Fatal("omitted stderr serialized")
				}
				if _, ok := fields["diagnostic"]; ok {
					t.Fatal("omitted diagnostic serialized")
				}
				return
			}
			for _, field := range []string{meta.Stderr, meta.Diagnostic} {
				if strings.Contains(field, secret) {
					t.Fatal("configured credential retained")
				}
				if tc.broken {
					if field != "[diagnostic omitted: sanitization failed]" {
						t.Fatalf("failure did not close: %q", field)
					}
				} else if !strings.Contains(field, "MCP") || !strings.Contains(field, redact.Replacement) {
					t.Fatalf("lost sanitized startup evidence: %q", field)
				}
			}
		})
	}
}

func TestRetainRunDiagnostics_Bounded(t *testing.T) {
	t.Parallel()
	result := &agent.RunResult{Outcome: agent.OutcomeFailed, Stderr: strings.Repeat("safe diagnostic line\n", 10000), Diagnostic: strings.Repeat("safe diagnostic line\n", 1000)}
	var meta harness.Meta
	retainRunDiagnostics(t.Context(), Options{MCP: &MCPRuntime{Redactor: redact.New(redact.Options{Environ: []string{}})}}, result, &meta, slog.New(slog.DiscardHandler))
	if len(meta.Stderr) == 0 || len(meta.Stderr) > 64<<10 || len(meta.Diagnostic) == 0 || len(meta.Diagnostic) > 4096 {
		t.Fatalf("unbounded metadata: stderr=%d diagnostic=%d", len(meta.Stderr), len(meta.Diagnostic))
	}
}
