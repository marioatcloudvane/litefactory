package turn

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Shell fixtures exercise the production transport and sink; they do not
// certify any provider version or real descendant collection capability.
func TestMeterSyntheticTransportPersistsAndReports(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			var f *fixture
			f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				return succeeded(req, f.paths.Home()), nil
			}))
			initial := f.execute(f.request())
			if !initial.Succeeded() {
				t.Fatal(initial)
			}
			root := t.TempDir()
			binary := filepath.Join(root, provider)
			stream := `{"type":"assistant","message":{"id":"message-1","model":"observed-model","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","message":{"id":"message-1","model":"observed-model","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"result","subtype":"success","is_error":false,"result":"done"}`
			if provider == "codex" {
				stream = `{"type":"thread.started","thread_id":"root-thread"}
{"type":"turn.completed","usage":{"input_tokens":20,"cached_input_tokens":5,"output_tokens":4}}`
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\ncat >/dev/null\ncat <<'FIXTURE'\n"+stream+"\nFIXTURE\n"), 0700); err != nil {
				t.Fatal(err)
			}
			logger := slog.New(slog.DiscardHandler)
			environ := []string{"PATH=/usr/bin:/bin", "HOME=" + root}
			var runner agent.Runner
			var err error
			if provider == "claude" {
				runner, err = harness.New(harness.Options{Paths: f.paths, Claude: config.Claude{Binary: binary, Tools: []string{"Read"}, AllowedTools: []string{"Read"}, SettingsPath: filepath.Join(root, "settings.json"), TimeoutSeconds: 10}, Model: config.DefaultClaudeModel, Environ: environ, Logger: logger})
			} else {
				runner, err = codex.New(codex.Options{Paths: f.paths, Codex: config.Codex{Binary: binary, TimeoutSeconds: 10}, Model: "configured-only", Environ: environ, Logger: logger})
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.db.SetMeterPrices(t.Context(), provider, "observed-model", []store.MeterPriceRate{{CodingAgent: provider, Model: "observed-model", Dimension: "input_uncached", USDPerMillion: "1"}, {CodingAgent: provider, Model: "observed-model", Dimension: "output", USDPerMillion: "1"}, {CodingAgent: provider, Model: "observed-model", Dimension: "cache_read", USDPerMillion: "1"}, {CodingAgent: provider, Model: "observed-model", Dimension: "cache_write", USDPerMillion: "1"}})
			if err != nil {
				t.Fatal(err)
			}
			request := agent.Request{Ref: naming.Ref{Forge: "github", Owner: "acme", Repo: "acme-api", Issue: 73}, RunOrdinal: initial.Run.Ordinal, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e"}, Mode: "spike", WorktreePath: root, Prompt: "synthetic transport"}
			if provider == "codex" {
				request.Permissions = agent.Permissions{Profile: "spike", Payload: codex.Sandbox{Mode: "read-only"}}
			}
			result, err := runMetered(t.Context(), &preparation{meterDB: f.db, run: initial.Run, segment: store.Segment{Agent: provider}, runner: runner, request: request}, logger)
			if err != nil || result == nil || !result.Succeeded() {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			snapshot, err := f.db.NewMeterSnapshot(t.Context(), store.MeterFilter{RunID: initial.Run.ID}, store.MeterSnapshotOptions{})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := snapshot.Next(t.Context(), 256)
			if err != nil {
				t.Fatal(err)
			}
			if err = snapshot.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			measured := 0
			for _, row := range rows {
				if row.CollectionStatus == "complete" || row.EstimatedUSD != nil {
					t.Fatal("uncertified consumption marked complete")
				}
				if row.MeasurementID == 0 {
					continue
				}
				measured++
				if provider == "codex" {
					t.Fatal("inclusive unproven summary became additive")
				}
				if row.Tokens.InputUncached == nil || *row.Tokens.InputUncached != 10 || row.PricedSubtotalUSD == nil || *row.PricedSubtotalUSD != "0.000013" {
					t.Fatalf("lost or replayed measurement: %+v", row)
				}
			}
			if provider == "claude" && measured != 1 {
				t.Fatalf("measurement count=%d", measured)
			}
			summary, err := (&Reporter{db: f.db}).meterRunSummary(t.Context(), initial.Run.ID)
			if err != nil || summary == nil {
				t.Fatalf("summary=%+v error=%v", summary, err)
			}
		})
	}
}
