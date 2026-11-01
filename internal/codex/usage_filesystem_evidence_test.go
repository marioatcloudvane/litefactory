package codex

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// Native collection stays with the provider; this check retains factory
// attribution, persisted coverage and reporting at their existing boundary.
func TestUsageNativeAdapterPersistsFinalCoverage(t *testing.T) {
	home := t.TempDir()
	db, attempt := filesystemEvidenceMeter(t)
	capture := agent.UsageCapture{AttemptID: attempt, Sink: func(ctx context.Context, e agent.UsageEvent) error {
		return db.RecordMeterEvent(ctx, attempt, "codex", e)
	}}
	c := newUsageCaptureCollector(t.Context(), &capture)
	c.prepareRollouts([]string{"HOME=" + home})
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "new"))
	input := `{"type":"thread.started","thread_id":"root"}
{"type":"turn.completed","usage":{"input_tokens":20,"cached_input_tokens":8,"output_tokens":3}}
`
	consumeUsage(strings.NewReader(input), io.Discard, c)
	coverage := c.finish()
	if coverage.Status != agent.CollectionComplete {
		t.Fatalf("coverage %+v", coverage)
	}
	if err := db.FinishMeterAttempt(t.Context(), attempt, "success", string(coverage.Status), coverage.Version, coverage.SourceFormat, coverage.Diagnostics); err != nil {
		t.Fatal(err)
	}
	view, err := meterview.Build(t.Context(), db, store.MeterFilter{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Details) != 1 || view.Summary.CollectionStatus != string(coverage.Status) || view.Summary.KnownTokens.InputUncached == nil || *view.Summary.KnownTokens.InputUncached != 12 {
		t.Fatalf("persisted coverage %+v", view)
	}
}
