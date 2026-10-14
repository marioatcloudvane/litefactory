package codex

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
)

// All envelopes, counters, file names and scheduling in this file are synthetic.
// Real isolated filesystem I/O establishes offline behavior only, not authentic
// current-provider hierarchy compatibility.
func filesystemEvidenceWrite(t *testing.T, home, name, content string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-"+name+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func filesystemEvidenceEnvelope(id, parent, response string) string {
	return fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"parent_thread_id\":%q,\"cli_version\":\"synthetic\"}}\n", id, parent) +
		`{"type":"turn_context","payload":{"model":"gpt-6-astra","turn_id":"turn"}}` + "\n" + filesystemEvidenceResponse(id, response) +
		`{"type":"event_msg","payload":{"type":"turn_complete"}}` + "\n"
}
func filesystemEvidenceResponse(id, response string) string {
	return fmt.Sprintf("{\"timestamp\":\"2026-09-09T18:20:00Z\",\"type\":\"token_usage_record\",\"payload\":{\"thread_id\":%q,\"turn_id\":\"turn\",\"response_id\":%q,\"usage\":{\"input_tokens\":20,\"cached_input_tokens\":8,\"output_tokens\":3}}}\n", id, response)
}
func filesystemEvidenceCollector(t *testing.T, home string) (*usageCollector, *[]agent.UsageObservation) {
	t.Helper()
	var observations []agent.UsageObservation
	c := newUsageCollector(t.Context(), agent.Request{Mode: "implement", Usage: &agent.UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Observation != nil {
			observations = append(observations, e.Observation.Clone())
		}
		return nil
	}}})
	c.prepareRollouts([]string{"HOME=" + home})
	c.thread = "root"
	c.invocation("root", "")
	t.Cleanup(func() {
		if c.rollouts != nil && c.rollouts.root != nil {
			_ = c.rollouts.root.Close()
		}
	})
	return c, &observations
}

func TestFilesystemEvidenceImplementUnchangedAncestor(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old-root"))
	filesystemEvidenceWrite(t, home, "ancestor", filesystemEvidenceEnvelope("ancestor", "root", "old-ancestor"))
	c, got := filesystemEvidenceCollector(t, home)
	filesystemEvidenceWrite(t, home, "grandchild", filesystemEvidenceEnvelope("grandchild", "ancestor", "new-grandchild"))
	filesystemEvidenceWrite(t, home, "parallel", filesystemEvidenceEnvelope("parallel", "root", "new-parallel"))
	filesystemEvidenceWrite(t, home, "unrelated", filesystemEvidenceEnvelope("unrelated", "foreign-root", "foreign"))
	c.collectRollouts(true)
	c.collectRollouts(true)
	if len(*got) != 2 {
		t.Fatalf("observations=%+v, want only two new descendants", *got)
	}
	for _, o := range *got {
		if o.InvocationID != "grandchild" && o.InvocationID != "parallel" {
			t.Fatalf("historical/foreign usage charged: %+v", o)
		}
		if o.ActualModel == nil || *o.ActualModel != "gpt-6-astra" || *o.Tokens.InputUncached != 12 {
			t.Fatalf("wrong attribution: %+v", o)
		}
	}
	if c.parents["grandchild"] != "ancestor" || c.parents["ancestor"] != "root" || c.parents["parallel"] != "root" {
		t.Fatalf("parents=%v", c.parents)
	}
}

func TestFilesystemEvidenceDelayedFinalDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old"))
		c, got := filesystemEvidenceCollector(t, home)
		c.collectRollouts(true)
		// The delayed writer does not access the collector; finalization must discover
		// a new rollout using its bounded drain, without another stdout event.
		path := filepath.Join(home, ".codex", "sessions", "rollout-delayed.jsonl")
		done := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			done <- os.WriteFile(path, []byte(filesystemEvidenceEnvelope("delayed", "root", "new")), 0600)
		}()
		started := time.Now()
		coverage := c.finish()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("drain unbounded: %v", elapsed)
		}
		if len(*got) != 1 || (*got)[0].InvocationID != "delayed" {
			t.Fatalf("delayed descendant missing: %+v", *got)
		}
		if slices.Contains(coverage.Diagnostics, "collection_cancelled") {
			t.Fatalf("ordinary final drain mislabeled as request cancellation: %v", coverage.Diagnostics)
		}
		if coverage.Status == agent.CollectionComplete {
			t.Fatal("synthetic hierarchy certified complete")
		}
	})
}

func TestFilesystemEvidenceScanBudget(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old"))
	c, got := filesystemEvidenceCollector(t, home)
	// Set the accumulated byte budget at its real limit, then exercise actual
	// filesystem discovery; no giant fixture is necessary to cross the bound.
	c.rollouts.bytes = rolloutBytes
	c.collectRollouts(true)
	if len(*got) != 0 || !slices.Contains(c.coverage.Diagnostics, "usage_source_scan_incomplete") {
		t.Fatalf("observations=%v diagnostics=%v", *got, c.coverage.Diagnostics)
	}
}

func TestFilesystemEvidenceHistoricalRevalidation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"missing", "replaced", "same-size-edit", "symlink"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "root"))
			content := filesystemEvidenceEnvelope("child", "root", "child")
			path := filesystemEvidenceWrite(t, home, "child", content)
			from, _ := time.Parse(time.RFC3339, "2026-09-09T18:00:00Z")
			h, err := CollectHistoricalUsage(t.Context(), home, "attempt", "root", HistoricalWindow{From: from, To: from.Add(time.Hour)})
			if err != nil || len(h.Observations) != 1 {
				t.Fatalf("initial collection: %+v, %v", h, err)
			}
			switch change {
			case "missing":
				err = os.Remove(path)
			case "replaced":
				replacement := filesystemEvidenceWrite(t, home, "replacement", content)
				err = os.Rename(replacement, path)
			case "same-size-edit":
				err = os.WriteFile(path, []byte(strings.Replace(content, `"output_tokens":3`, `"output_tokens":4`, 1)), 0600)
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside")
				if e := os.WriteFile(target, []byte(content), 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				err = os.Symlink(target, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Revalidate(t.Context()); err == nil {
				t.Fatal("changed source passed revalidation")
			}
		})
	}
}

func TestFilesystemEvidenceMalformedUnsupported(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content, diag string }{
		{"malformed-metadata", "{\n", "unsupported_rollout_metadata"},
		{"unsupported-metadata", `{"type":"future_metadata","payload":{}}` + "\n", "unsupported_rollout_metadata"},
		{"malformed-usage", `{"type":"session_meta","payload":{"id":"child","parent_thread_id":"root"}}` + "\n{\n", "malformed_usage_event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old"))
			filesystemEvidenceWrite(t, home, "child", tc.content)
			from, _ := time.Parse(time.RFC3339, "2026-09-09T18:00:00Z")
			h, err := CollectHistoricalUsage(t.Context(), home, "attempt", "root", HistoricalWindow{From: from, To: from.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if len(h.Observations) != 0 || !slices.Contains(h.Diagnostics, tc.diag) || !slices.Contains(h.Diagnostics, "historical_descendant_usage_missing") {
				t.Fatalf("unexpected evidence: %+v", h)
			}
		})
	}
}

func TestFilesystemEvidenceResumedAppendAndPartialLine(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old-root"))
	path := filesystemEvidenceWrite(t, home, "child", filesystemEvidenceEnvelope("child", "root", "old-child"))
	c, got := filesystemEvidenceCollector(t, home)
	appendBytes := func(value string) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := f.WriteString(value)
		closeErr := f.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	response := filesystemEvidenceResponse("child", "new-child")
	appendBytes(strings.TrimSuffix(response, "\n"))
	c.collectRollouts(true)
	if len(*got) != 0 {
		t.Fatalf("partial/historical row charged: %+v", *got)
	}
	appendBytes("\n")
	c.collectRollouts(true)
	appendBytes(response)
	c.collectRollouts(true)
	if len(*got) != 1 || (*got)[0].EventKey != "response:new-child" {
		t.Fatalf("resume/replay attribution: %+v", *got)
	}
}

func TestFilesystemEvidenceCancelledDrain(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old"))
	c, _ := filesystemEvidenceCollector(t, home)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c.ctx = ctx
	coverage := c.finish()
	if !slices.Contains(coverage.Diagnostics, "collection_cancelled") {
		t.Fatalf("real cancellation lost: %v", coverage.Diagnostics)
	}
}

func TestFilesystemEvidenceCompletedRunWithSubagentIsComplete(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	c, got := filesystemEvidenceCollector(t, home)
	rollout := func(id, parent, model, response string, input, cached, output int) string {
		return fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"parent_thread_id\":%q,\"cli_version\":\"synthetic\"}}\n", id, parent) +
			fmt.Sprintf("{\"type\":\"turn_context\",\"payload\":{\"model\":%q,\"turn_id\":\"turn\"}}\n", model) +
			fmt.Sprintf("{\"type\":\"token_usage_record\",\"payload\":{\"thread_id\":%q,\"turn_id\":\"turn\",\"response_id\":%q,\"usage\":{\"input_tokens\":%d,\"cached_input_tokens\":%d,\"output_tokens\":%d}}}\n", id, response, input, cached, output) +
			`{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n"
	}
	filesystemEvidenceWrite(t, home, "root", rollout("root", "", "gpt-6-astra", "root-response", 20, 8, 3))
	filesystemEvidenceWrite(t, home, "child", rollout("child", "root", "gpt-6-mini", "child-response", 10, 4, 2))
	exec := `{"type":"thread.started","thread_id":"root"}
{"type":"item.completed","item":{"type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"root","receiver_thread_ids":["child"]}}
{"type":"turn.completed","usage":{"input_tokens":30,"cached_input_tokens":12,"output_tokens":5}}
`
	if state := consumeUsage(strings.NewReader(exec), io.Discard, c); !state.completed {
		t.Fatalf("state=%+v", state)
	}
	coverage := c.finish()
	if coverage.Status != agent.CollectionComplete || len(coverage.Diagnostics) != 0 {
		t.Fatalf("coverage %+v", coverage)
	}
	models := map[string]string{}
	var input, read, output int64
	for _, o := range *got {
		if o.Scope != agent.UsageExclusive {
			continue
		}
		if o.ActualModel == nil {
			t.Fatalf("unattributed exclusive usage: %+v", o)
		}
		models[o.InvocationID] = *o.ActualModel
		input += *o.Tokens.InputUncached
		read += *o.Tokens.CacheRead
		output += *o.Tokens.Output
	}
	if models["root"] != "gpt-6-astra" || models["child"] != "gpt-6-mini" || input != 18 || read != 12 || output != 5 {
		t.Fatalf("models=%v input=%d read=%d output=%d", models, input, read, output)
	}
}

func TestFilesystemEvidenceFinalDiscoverySettlesOnlyTransientGap(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		t.Run(fmt.Sprint(permanent), func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			c, got := filesystemEvidenceCollector(t, home)
			db, attempt := filesystemEvidenceMeter(t)
			priorSink := c.capture.Sink
			c.capture.AttemptID = attempt
			c.capture.Sink = func(ctx context.Context, e agent.UsageEvent) error {
				if err := priorSink(ctx, e); err != nil {
					return err
				}
				return db.RecordMeterEvent(ctx, attempt, "codex", e)
			}
			c.emit(agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: attempt, ID: "root", SourceID: "root", SourceInvocationID: "root", Coverage: agent.UsageCoverage{Status: agent.CollectionUnknown}}})
			filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "new"))
			original := c.ctx
			cancelled, cancel := context.WithCancel(original)
			cancel()
			c.ctx = cancelled
			c.collectRollouts(false)
			if !slices.Contains(c.coverage.Diagnostics, "usage_source_scan_incomplete") {
				t.Fatal("interim failure not observed")
			}
			c.ctx = original
			if permanent {
				c.diagnostic("usage_source_inventory_incomplete")
			}
			c.terminal = true
			coverage := c.finish()
			if slices.Contains(coverage.Diagnostics, "usage_source_scan_incomplete") || slices.Contains(coverage.Diagnostics, "usage_source_discovery_cancelled") {
				t.Fatalf("transient gap retained: %+v", coverage)
			}
			if (coverage.Status == agent.CollectionComplete) == permanent {
				t.Fatalf("coverage %+v", coverage)
			}
			if err := db.FinishMeterAttempt(t.Context(), attempt, "success", string(coverage.Status), coverage.Version, coverage.SourceFormat, coverage.Diagnostics); err != nil {
				t.Fatal(err)
			}
			view, err := meterview.Build(t.Context(), db, store.MeterFilter{}, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Details) != 1 || view.Summary.CollectionStatus != string(coverage.Status) || view.Summary.KnownTokens.InputUncached == nil || *view.Summary.KnownTokens.InputUncached != 12 || slices.Contains(view.Summary.Diagnostics, "usage_source_scan_incomplete") {
				t.Fatalf("persisted coverage: %+v", view)
			}
			if len(*got) != 1 || *(*got)[0].Tokens.InputUncached != 12 {
				t.Fatalf("replay: %+v", *got)
			}
		})
	}
}

func filesystemEvidenceMeter(t *testing.T) (*store.DB, string) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "meter.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 614)
	if err != nil {
		t.Fatal(err)
	}
	seg, err := db.GetOrCreateSegment(ctx, session.ID, 1, "root", store.AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: store.ModeSpike, Agent: store.AgentCodex, TriggerCommentID: 1, LogPath: func(n int) string { return fmt.Sprintf("/logs/%d", n) }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	return db, a.ID
}
