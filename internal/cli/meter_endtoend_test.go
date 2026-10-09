package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/meter"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// These subprocesses are shell fixtures, not provider executions. They prove
// callback/outcome/export plumbing, not version compatibility or child forwarding.
// The turn package separately exercises the production observation bridge.
func TestMeterEndToEndSyntheticProviderCoverage(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newLogsHome(t)
			if _, err := config.WriteDefault(config.NewPaths(f.home)); err != nil {
				t.Fatal(err)
			}
			repo := f.repo("acme", "api")
			session := f.session(repo, 73)
			run := f.allocateRun(session, 1, time.Time{})
			attempt, err := f.db.StartMeterAttempt(t.Context(), run.ID, provider)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			binary := filepath.Join(root, provider)
			stream := syntheticMeterStream(provider)
			if err := os.WriteFile(binary, []byte("#!/bin/sh\ncat >/dev/null\ncat <<'FIXTURE'\n"+stream+"\nFIXTURE\n"), 0700); err != nil {
				t.Fatal(err)
			}
			paths := config.NewPaths(f.home)
			environ := []string{"PATH=/usr/bin:/bin", "HOME=" + root}
			var runner agent.Runner
			if provider == "claude" {
				runner, err = harness.New(harness.Options{Paths: paths, Claude: config.Claude{Binary: binary, Tools: []string{"Read"}, AllowedTools: []string{"Read"}, SettingsPath: filepath.Join(f.home, "settings.json"), TimeoutSeconds: 10}, Model: config.DefaultClaudeModel, Environ: environ, Logger: slog.New(slog.DiscardHandler)})
			} else {
				runner, err = codex.New(codex.Options{Paths: paths, Codex: config.Codex{Binary: binary, TimeoutSeconds: 10}, Model: "configured-only", Environ: environ, Logger: slog.New(slog.DiscardHandler)})
			}
			if err != nil {
				t.Fatal(err)
			}
			var observations []agent.UsageObservation
			req := agent.Request{Ref: naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 73}, RunOrdinal: 1, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e"}, Mode: "spike", WorktreePath: root, Prompt: "private prompt canary", Usage: &agent.UsageCapture{AttemptID: attempt.ID, Sink: func(ctx context.Context, e agent.UsageEvent) error {
				if e.Observation != nil {
					observations = append(observations, e.Observation.Clone())
				}
				if e.Invocation != nil {
					v := e.Invocation
					return f.db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attempt.ID, ID: v.ID, ParentID: v.ParentID, SourceID: v.SourceID, SourceInvocationID: v.SourceInvocationID, Status: string(v.Coverage.Status), Diagnostics: v.Coverage.Diagnostics})
				}
				return nil
			}}}
			if provider == "codex" {
				req.Permissions = agent.Permissions{Profile: "spike", Payload: codex.Sandbox{Mode: "read-only"}}
			}
			result, err := runner.Run(t.Context(), req)
			if err != nil || result == nil || !result.Succeeded() {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.Usage == nil || result.Usage.Status == agent.CollectionComplete {
				t.Fatalf("unsupported certification: %+v", result.Usage)
			}
			if len(observations) == 0 {
				t.Fatal("no source observations")
			}
			if provider == "codex" {
				for _, o := range observations {
					if o.Scope == agent.UsageExclusive || o.ActualModel != nil {
						t.Fatalf("invented attribution: %+v", o)
					}
				}
			}
			if err := f.db.FinishMeterAttempt(t.Context(), attempt.ID, "success", string(result.Usage.Status), result.Usage.Version, result.Usage.SourceFormat, result.Usage.Diagnostics); err != nil {
				t.Fatal(err)
			}
			code, out, diagnostics := f.run("meter", "export", "github:acme/api", "--coding_agent", provider, "--issue", "73", "--mode", "spike", "--time_from", "2000-01-01T00:00:00Z", "--time_to", "2100-01-01T01:00:00+01:00")
			if code != 0 {
				t.Fatal(diagnostics)
			}
			rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
			if err != nil || len(rows) < 3 {
				t.Fatalf("missing root/child: %v %v", rows, err)
			}
			for _, row := range rows[1:] {
				if row[25] != "coverage" || row[12] != "" || row[20] != "" || row[23] == "complete" {
					t.Fatalf("fabricated usage: %v", row)
				}
			}
			if strings.Contains(out, "private prompt") || strings.Contains(out, "configured-only") {
				t.Fatal("private/configuration text leaked")
			}
			code, out, diagnostics = f.run("meter", "acme/api", "73", "--json", "--details")
			if code != 0 {
				t.Fatal(diagnostics)
			}
			var response struct {
				Summary struct {
					Estimate *string `json:"estimated_usd"`
					Status   string  `json:"collection_status"`
				}
				Details []store.MeterRow
			}
			if err := json.Unmarshal([]byte(out), &response); err != nil {
				t.Fatal(err)
			}
			if response.Summary.Estimate != nil || response.Summary.Status == "complete" || len(response.Details) != len(rows)-1 {
				t.Fatalf("summary/export mismatch: %+v", response)
			}
		})
	}
}

func syntheticMeterStream(provider string) string {
	if provider == "claude" {
		return `{"type":"assistant","message":{"id":"root-message","model":"actual-root","content":[{"type":"tool_use","name":"Agent","id":"child"}],"usage":{"input_tokens":10,"output_tokens":3}}}
{"type":"assistant","parent_tool_use_id":"child","message":{"id":"child-message","model":"actual-child","usage":{"input_tokens":5,"output_tokens":2}}}
{"type":"result","subtype":"success","is_error":false,"result":"done","modelUsage":{"actual-root":{"inputTokens":15,"outputTokens":5}}}`
	}
	return `{"type":"thread.started","thread_id":"root-thread"}
{"type":"item.completed","item":{"id":"spawn","type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"root-thread","receiver_thread_ids":["child-thread"],"agents_states":{}}}
{"type":"turn.completed","usage":{"input_tokens":20,"cached_input_tokens":5,"output_tokens":4}}`
}

// Covers every export filter separately and as an AND expression with two
// registered projects sharing the same issue number. No clock sleeps are used.
func TestMeterEndToEndProjectAndFilterMatrix(t *testing.T) {
	f, _, _ := meterTestHome(t)
	other := f.repo("other", "api")
	session := f.session(other, 73)
	run := f.allocateRun(session, 9, time.Time{})
	if _, err := f.db.StartMeterAttempt(t.Context(), run.ID, "claude"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		flags []string
		count int
	}{
		{nil, 1}, {[]string{"--coding_agent", "codex"}, 1}, {[]string{"--coding_agent", "claude"}, 0},
		{[]string{"--issue", "73"}, 1}, {[]string{"--issue", "74"}, 0},
		{[]string{"--mode", "spike"}, 1}, {[]string{"--mode", "plan"}, 0},
		{[]string{"--time_from", "2000-01-01T00:00:00Z"}, 1}, {[]string{"--time_from", "2100-01-01T00:00:00Z"}, 0},
		{[]string{"--time_to", "2100-01-01T00:00:00Z"}, 1}, {[]string{"--time_to", "2000-01-01T00:00:00Z"}, 0},
		{[]string{"--coding_agent", "codex", "--issue", "73", "--mode", "spike", "--time_from", "2000-01-01T01:00:00+01:00", "--time_to", "2100-01-01T00:00:00Z"}, 1},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			args := append([]string{"meter", "export", "github:acme/api"}, tc.flags...)
			code, out, diagnostics := f.run(args...)
			if code != 0 {
				t.Fatal(diagnostics)
			}
			rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
			if err != nil || len(rows) != tc.count+1 {
				t.Fatalf("rows=%v err=%v", rows, err)
			}
			for _, row := range rows[1:] {
				if row[0] != "github:acme/api" || row[6] != "codex" {
					t.Fatalf("cross-project leakage: %v", row)
				}
			}
		})
	}
}

func TestMeterEndToEndFrozenPricingDuringSlowExport(t *testing.T) {
	f, repo, baseline := meterTestHome(t)
	for _, rate := range []string{"2"} {
		code, _, errOut := f.run("meter", "prices", "set", "actual", "--coding_agent", "codex", "--input", rate, "--output", rate, "--cache_read", "0", "--cache_write", "0")
		if code != 0 {
			t.Fatal(errOut)
		}
	}
	attempt, err := f.db.StartMeterAttempt(t.Context(), baseline.RunID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.PutMeterInvocation(t.Context(), store.MeterInvocation{AttemptID: attempt.ID, ID: "priced-root", SourceID: "synthetic", SourceInvocationID: "priced-root", Status: "partial"}); err != nil {
		t.Fatal(err)
	}
	add := func(ctx context.Context, key string, quantity int64) error {
		model := "actual"
		zero := int64(0)
		tokens := store.MeterTokens{InputUncached: &quantity, Output: &zero, CacheRead: &zero, CacheWrite: &zero}
		o := store.MeterObservation{AttemptID: attempt.ID, SourceID: "synthetic", EventKey: key, InvocationID: "priced-root", Semantics: "incremental/exclusive", CounterEpoch: "fresh", CounterKey: "input", ModelKey: `["actual"]`, ActualModel: &model, ModelProvenance: "synthetic_actual", Digest: strings.Repeat(key, 64), Status: "partial", Tokens: tokens}
		return f.db.ApplyMeterObservation(ctx, o, func(state store.MeterLedgerState) (store.MeterMutation, error) {
			if state.Measurement != nil {
				quantity += *state.Measurement.InputUncached
			}
			tokens.InputUncached = &quantity
			rates := []meter.PriceRate{}
			for _, r := range state.Catalog.Rates {
				if r.CodingAgent == "codex" && r.Model == model {
					rates = append(rates, meter.PriceRate{Dimension: r.Dimension, USDPerMillion: r.USDPerMillion})
				}
			}
			v, e := meter.Value(agent.TokenUsage(tokens), rates)
			if e != nil {
				return store.MeterMutation{}, e
			}
			frozen := store.MeterValuation{EstimatedUSD: v.EstimatedUSD, PricedSubtotalUSD: v.PricedSubtotalUSD, Status: v.Status}
			for _, d := range v.Dimensions {
				frozen.Dimensions = append(frozen.Dimensions, store.MeterValuationDimension{Dimension: d.Dimension, Quantity: d.Quantity, USDPerMillion: d.USDPerMillion, AmountUSD: d.AmountUSD, Status: d.Status})
			}
			return store.MeterMutation{Measurement: &tokens, Valuation: &frozen}, nil
		})
	}
	if err := add(t.Context(), "a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	writer := &meterBufferedGate{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- writeMeter(t.Context(), writer, f.db, store.MeterFilter{RepoID: repo.ID}, meterOptions{}, true)
	}()
	select {
	case <-writer.entered:
	case <-time.After(testutil.Wait):
		t.Fatal("snapshot not ready")
	}
	ctx, cancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cancel()
	// The header write is blocked, but catalog and measurement mutations must
	// complete through the same single SQLite connection.
	_, editErr := f.db.SetMeterPrices(ctx, "codex", "actual", []store.MeterPriceRate{{CodingAgent: "codex", Model: "actual", Dimension: "input_uncached", USDPerMillion: "9"}, {CodingAgent: "codex", Model: "actual", Dimension: "output", USDPerMillion: "9"}})
	var addErr error
	if editErr == nil {
		addErr = add(ctx, "b", 1_000_000)
	}
	close(writer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if editErr != nil || addErr != nil {
		t.Fatalf("SQLite retained during output: edit=%v add=%v", editErr, addErr)
	}
	assertPriced := func(out, wantQuantity, wantAmount string) {
		t.Helper()
		rows, e := csv.NewReader(strings.NewReader(out)).ReadAll()
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, row := range rows[1:] {
			if row[3] == attempt.ID {
				found = true
				if row[12] != wantQuantity || row[21] != wantAmount || row[20] != "" {
					t.Fatalf("mixed snapshot or full partial estimate: %v", row)
				}
			}
		}
		if !found {
			t.Fatal("missing priced row")
		}
	}
	assertPriced(writer.String(), "1000000", "2")
	code, out, errOut := f.run("meter", "export", "github:acme/api")
	if code != 0 {
		t.Fatal(errOut)
	}
	assertPriced(out, "2000000", "4")
	code, out, errOut = f.run("meter", "acme/api", "73", "--json")
	if code != 0 {
		t.Fatal(errOut)
	}
	var response struct {
		Summary struct {
			Subtotal *string `json:"priced_subtotal_usd"`
			Estimate *string `json:"estimated_usd"`
		}
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err)
	}
	if response.Summary.Subtotal == nil || *response.Summary.Subtotal != "4.000000000000" || response.Summary.Estimate != nil {
		t.Fatalf("report/export mismatch: %s", out)
	}
}

type meterBufferedGate struct {
	strings.Builder
	entered, release chan struct{}
	first            bool
}

func (w *meterBufferedGate) Write(p []byte) (int, error) {
	if !w.first {
		w.first = true
		close(w.entered)
		<-w.release
	}
	return w.Builder.Write(p)
}

func TestMeterEndToEndCancelledSlowExportCleanup(t *testing.T) {
	f, repo, a := meterTestHome(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := &meterBufferedGate{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- writeMeter(ctx, writer, f.db, store.MeterFilter{RepoID: repo.ID}, meterOptions{}, true)
	}()
	select {
	case <-writer.entered:
	case <-time.After(testutil.Wait):
		t.Fatal("waiting for output")
	}
	cancel()
	close(writer.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation reported as success: %v", err)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("cancelled export did not finish")
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cleanupCancel()
	if _, err := f.db.StartMeterAttempt(cleanupCtx, a.RunID, "claude"); err != nil {
		t.Fatalf("database unavailable after cancelled snapshot: %v", err)
	}
	code, out, errOut := f.run("meter", "export", "github:acme/api")
	if code != 0 || !strings.HasPrefix(out, meterCSVHeaderLine) {
		t.Fatalf("later export failed %d %s", code, errOut)
	}
}
