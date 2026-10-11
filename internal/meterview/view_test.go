package meterview

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// meterViewFixture persists events in the order the collectors emit them:
// invocations are registered before usage is seen, so their status is stale.
type meterViewFixture struct {
	t   *testing.T
	db  *store.DB
	run store.Run
}

func newMeterViewFixture(t *testing.T) *meterViewFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "litefactory.db"))
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
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), "api"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "wt")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 674)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "claude-session-id", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(n int) string { return fmt.Sprintf("/logs/%04d.jsonl", n) }})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"root-model", "child-model"} {
		var rates []store.MeterPriceRate
		for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
			rates = append(rates, store.MeterPriceRate{CodingAgent: "claude", Model: model, Dimension: dimension, USDPerMillion: "1"})
		}
		if _, err := db.SetMeterPrices(ctx, "claude", model, rates); err != nil {
			t.Fatal(err)
		}
	}
	return &meterViewFixture{t: t, db: db, run: run}
}

func (f *meterViewFixture) record(attempt string, e agent.UsageEvent) {
	f.t.Helper()
	if err := f.db.RecordMeterEvent(f.t.Context(), attempt, "claude", e); err != nil {
		f.t.Fatal(err)
	}
}

func (f *meterViewFixture) invocation(attempt, id, parent string) {
	f.t.Helper()
	f.record(attempt, agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: attempt, ID: id, ParentID: parent, SourceID: "session", SourceInvocationID: id, Coverage: agent.UsageCoverage{Status: agent.CollectionUnknown, SourceFormat: "stream-json"}}})
}

func (f *meterViewFixture) usage(attempt, invocation, key, model string, input, output int64) {
	f.t.Helper()
	zero := int64(0)
	f.record(attempt, agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: attempt, SourceID: "session", EventKey: key, InvocationID: invocation, ActualModel: &model, ModelProvenance: "message", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &input, Output: &output, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial, SourceFormat: "stream-json"}}})
}

func (f *meterViewFixture) finish(attempt, outcome string, coverage agent.UsageCoverage) {
	f.t.Helper()
	f.record(attempt, agent.UsageEvent{Coverage: &coverage})
	if err := f.db.FinishMeterAttempt(f.t.Context(), attempt, outcome, string(coverage.Status), coverage.Version, coverage.SourceFormat, coverage.Diagnostics); err != nil {
		f.t.Fatal(err)
	}
}

func (f *meterViewFixture) text(details bool) string {
	f.t.Helper()
	view, err := Build(f.t.Context(), f.db, store.MeterFilter{}, details)
	if err != nil {
		f.t.Fatal(err)
	}
	var b strings.Builder
	if err := view.WriteText(&b); err != nil {
		f.t.Fatal(err)
	}
	return b.String()
}

func TestBuildCompletedRunReportsCompleteAndInterruptedRunIsNamed(t *testing.T) {
	f := newMeterViewFixture(t)
	ctx := t.Context()
	complete, err := f.db.StartMeterAttempt(ctx, f.run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	f.invocation(complete.ID, "root", "")
	f.usage(complete.ID, "root", "m1", "root-model", 10, 3)
	f.invocation(complete.ID, "tool:child", "root")
	f.usage(complete.ID, "tool:child", "m2", "child-model", 5, 2)
	f.usage(complete.ID, "root", "m3", "root-model", 4, 1)
	f.finish(complete.ID, "success", agent.UsageCoverage{Status: agent.CollectionComplete, SourceFormat: "stream-json"})

	out := f.text(false)
	for _, want := range []string{"Collection: complete;", "Complete local estimate USD: ", "input_uncached: 19; known subtotal: 19", "output: 6; known subtotal: 6"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "collection unknown") || strings.Contains(out, "total unknown") {
		t.Fatalf("completed run still unknown:\n%s", out)
	}

	interrupted, err := f.db.StartMeterAttempt(ctx, f.run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	f.invocation(interrupted.ID, "root", "")
	f.usage(interrupted.ID, "root", "m1", "root-model", 7, 2)
	f.finish(interrupted.ID, "cancelled", agent.UsageCoverage{Status: agent.CollectionPartial, SourceFormat: "stream-json", Diagnostics: []string{"collection_cancelled", "missing_terminal_usage"}})

	out = f.text(false)
	if strings.Contains(out, "Complete local estimate") || !strings.Contains(out, "Collection: partial;") || !strings.Contains(out, "total unknown") {
		t.Fatalf("interrupted run reported a total:\n%s", out)
	}
	out = f.text(true)
	var named bool
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "attempt "+interrupted.ID) && strings.Contains(line, fmt.Sprintf("run %d ", f.run.Ordinal)) && strings.Contains(line, "collection partial") && strings.Contains(line, "collection_cancelled;missing_terminal_usage") {
			named = true
		}
		if strings.Contains(line, "attempt "+complete.ID) && !strings.Contains(line, "collection complete") {
			t.Fatalf("complete attempt row degraded: %s", line)
		}
	}
	if !named {
		t.Fatalf("interrupted attempt not named:\n%s", out)
	}
}
