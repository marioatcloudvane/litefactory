package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

func metaRequest(worktree string) Request {
	return Request{
		Ref:            testRef,
		RunOrdinal:     7,
		SegmentOrdinal: 1,
		Mode:           "spike",
		WorktreePath:   worktree,
		Prompt:         testPrompt,
	}
}

// S-3 lists the keys the document must carry. A missing key is not a cosmetic
// problem: NNNN.meta.json is what answers "what was this run" after $LF_HOME
// has outlived the database row.
func TestMetaCarriesEveryRequiredKey(t *testing.T) {
	meta := NewMeta(metaRequest("/w/task-123"), testPrompt, &RunResult{
		Outcome:    OutcomeSucceeded,
		ExitCode:   0,
		SessionID:  "1a17a10a-0000-5000-8000-000000000001",
		LogPath:    "/home/logs/runs/github/acme/acme-api/123/0007.jsonl",
		Model:      "claude-sonnet-4-6",
		Tools:      []string{"Read", "Edit"},
		ResultText: "DONE",
		CostUSD:    0.25,
		HasResult:  true,
		StartedAt:  time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 23, 10, 0, 30, 0, time.UTC),
		Duration:   30 * time.Second,
	})

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"run_ordinal", "task_ref", "session_id", "segment_ordinal", "claude_session_id",
		"mode", "model", "prompt", "worktree_path", "branch", "sha_before", "sha_after",
		"exit_code", "started_at", "finished_at", "duration_ms", "cost_usd",
		"files_touched", "result_comment_id", "error",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("S-3 key %q is missing from the metadata document", key)
		}
	}

	if meta.TaskRef != "github:acme/acme-api#123" {
		t.Errorf("task ref = %q", meta.TaskRef)
	}
	if meta.Prompt != testPrompt {
		t.Error("the prompt was not persisted verbatim (AC-3.11)")
	}
	if meta.StartedAt != "2026-08-23T10:00:00Z" {
		t.Errorf("started_at = %q, want RFC 3339", meta.StartedAt)
	}
	if meta.DurationMS != 30_000 {
		t.Errorf("duration_ms = %d", meta.DurationMS)
	}
	if meta.CostUSD == nil || *meta.CostUSD != 0.25 {
		t.Errorf("cost_usd = %v", meta.CostUSD)
	}
	if meta.Error != nil {
		t.Errorf("a successful run carries an error: %q", *meta.Error)
	}
}

// A run that never emitted a result event has an *unknown* cost, not a cost of
// zero. The difference matters to anyone adding them up.
func TestMetaLeavesUnknownCostNull(t *testing.T) {
	meta := NewMeta(metaRequest("/w"), testPrompt, &RunResult{
		Outcome:  OutcomeNoResult,
		Reason:   "claude exited 0 without emitting a terminal result event",
		ExitCode: 0,
	})
	if meta.CostUSD != nil {
		t.Errorf("cost_usd = %v, want null", *meta.CostUSD)
	}
	if meta.Error == nil {
		t.Fatal("the failure reason is not recorded (S-9)")
	}
	if *meta.Error != "claude exited 0 without emitting a terminal result event" {
		t.Errorf("error = %q", *meta.Error)
	}
	if meta.Outcome != "no_result" {
		t.Errorf("outcome = %q", meta.Outcome)
	}
}

// The whole point of the "best effort on abnormal exit" rule (S-3): a metadata
// document naming a timeout is worth far more than a missing one, so a nil
// result must still produce a usable document.
func TestNewMetaToleratesAMissingResult(t *testing.T) {
	meta := NewMeta(metaRequest("/w/task-123"), testPrompt, nil)

	if meta.RunOrdinal != 7 || meta.TaskRef != "github:acme/acme-api#123" {
		t.Errorf("the request's own fields were lost: %+v", meta)
	}
	if meta.Prompt != testPrompt {
		t.Error("the prompt was lost")
	}
	if meta.ExitCode != SignalExitCode {
		t.Errorf("exit code = %d, want %d", meta.ExitCode, SignalExitCode)
	}
	if _, err := json.Marshal(meta); err != nil {
		t.Fatalf("the document does not encode: %v", err)
	}
}

func TestWriteMetaRoundTrips(t *testing.T) {
	home := t.TempDir()
	path, err := MetaPath(home, testRef, 7)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := naming.RunMetaPath(home, testRef, 7); path != want {
		t.Fatalf("MetaPath = %q, want naming's %q", path, want)
	}

	meta := NewMeta(metaRequest("/w/task-123"), testPrompt, &RunResult{
		Outcome: OutcomeSucceeded, Model: "claude-sonnet-4-6", HasResult: true, CostUSD: 1.5,
	})
	// The fields internal/turn fills in.
	meta.SessionID = 42
	meta.Branch = "lf/123-add-a-widget"
	meta.SHABefore = "aaa"
	meta.SHAAfter = "bbb"
	meta.FilesTouched = []string{"internal/api/health.go"}
	commentID := int64(9001)
	meta.ResultCommentID = &commentID

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteMeta(path, meta); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("metadata mode = %o, want 600; it holds the full prompt", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back Meta
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("the document does not decode: %v", err)
	}
	if back.SessionID != 42 || back.Branch != "lf/123-add-a-widget" || back.SHAAfter != "bbb" {
		t.Errorf("round trip lost turn's fields: %+v", back)
	}
	if back.ResultCommentID == nil || *back.ResultCommentID != 9001 {
		t.Errorf("result_comment_id = %v", back.ResultCommentID)
	}
	if back.Prompt != testPrompt {
		t.Error("round trip lost the prompt")
	}

	// Writing again replaces cleanly, and leaves no temporary file behind.
	if err := WriteMeta(path, meta); err != nil {
		t.Fatalf("second WriteMeta: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the run directory holds %d entries, want just the metadata document", len(entries))
	}
}

func TestWriteMetaRejectsARelativePath(t *testing.T) {
	if err := WriteMeta("0001.meta.json", Meta{}); err == nil {
		t.Fatal("WriteMeta accepted a relative path")
	} else if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error %v does not unwrap to ErrInvalidRequest", err)
	}
}

// AC-SR-2.10, the document half. A resumed run's prompt is only the delta it
// sent; without this key there is nothing in the run directory to say that the
// rest of the conversation is in claude's transcript rather than missing.
func TestMetaRecordsWhichPathTheRunTook(t *testing.T) {
	for _, tc := range []struct {
		path RunPath
		want bool
	}{
		{PathCold, false},
		{"", false},
		{PathResume, true},
		{PathColdAfterResumeRefused, false},
	} {
		t.Run(tc.path.String(), func(t *testing.T) {
			req := metaRequest("/w/task-123")
			req.Path = tc.path

			meta := NewMeta(req, testPrompt, nil)
			if meta.Resumed != tc.want {
				t.Errorf("resumed = %t, want %t", meta.Resumed, tc.want)
			}

			data, err := json.Marshal(meta)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got, ok := decoded["resumed"]; !ok || got != tc.want {
				t.Errorf("the document's resumed key = %v (present %t), want %t", got, ok, tc.want)
			}
		})
	}
}
