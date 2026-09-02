package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// todoWriteResolved and todoWriteRequested are the measured finding the whole
// story exists to make visible ("Settled before planning" in the
// implementation plan): eight tools requested, seven resolved, TodoWrite
// silently dropped with no error raised anywhere. The resolved order is
// copied verbatim from a real init event.
var (
	todoWriteResolved  = []string{"Task", "Bash", "Edit", "Glob", "Grep", "Read", "Write"}
	todoWriteRequested = append(append([]string{}, todoWriteResolved...), "TodoWrite")
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
		"run_ordinal", "task_ref", "session_id", "segment_ordinal", "agent", "session_ref",
		"claude_session_id", "mode", "model", "prompt", "worktree_path", "branch", "sha_before",
		"sha_after", "exit_code", "started_at", "finished_at", "duration_ms", "cost_usd",
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

// TestMetaToolsRequestedAndToolProfileComeFromDifferentSources is AC-4.1: Meta
// carries ToolsRequested (what the request asked for), the existing resolved
// Tools (what the init event reported), and ToolProfile, and the three must
// not collapse into one another. Tools and ToolsRequested are fed disjoint
// lists here on purpose — if the implementation ever aliased one field to the
// other, this is the test that would catch it, where two lists that merely
// overlap would not.
func TestMetaToolsRequestedAndToolProfileComeFromDifferentSources(t *testing.T) {
	req := metaRequest("/w/task-123")
	req.Permissions = Permissions{
		Profile: "research",
		Payload: ToolSurface{Tools: []string{"Read", "Write"}, AllowedTools: []string{"Read"}},
	}

	meta := NewMeta(req, testPrompt, &RunResult{
		Outcome:  OutcomeSucceeded,
		ExitCode: 0,
		Tools:    []string{"Grep", "Bash"}, // deliberately disjoint from req.Tools
	})

	if !slices.Equal(meta.Tools, []string{"Grep", "Bash"}) {
		t.Errorf("Tools = %q, want the init event's resolved surface", meta.Tools)
	}
	if !slices.Equal(meta.ToolsRequested, []string{"Read", "Write"}) {
		t.Errorf("ToolsRequested = %q, want the request's own surface", meta.ToolsRequested)
	}
	if meta.ToolProfile != "research" {
		t.Errorf("ToolProfile = %q, want %q", meta.ToolProfile, "research")
	}
	if slices.Equal(meta.Tools, meta.ToolsRequested) {
		t.Fatal("Tools and ToolsRequested collapsed into the same slice")
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"tools", `["Grep","Bash"]`},
		{"tools_requested", `["Read","Write"]`},
		{"tool_profile", `"research"`},
	} {
		got, ok := decoded[tc.key]
		if !ok {
			t.Errorf("JSON key %q is missing", tc.key)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("JSON key %q = %s, want %s", tc.key, got, tc.want)
		}
	}
}

// TestMetaToolsRequestedSurvivesANilResult is AC-4.1's other half: the
// requested surface and its profile are known before the subprocess starts and
// must not depend on a result ever arriving. This is exactly the run whose
// requested surface is worth reading (E-1) — a run that died before the init
// event still records what it asked for.
func TestMetaToolsRequestedSurvivesANilResult(t *testing.T) {
	req := metaRequest("/w/task-123")
	req.Permissions = Permissions{
		Profile: "research",
		Payload: ToolSurface{Tools: []string{"Read", "Grep"}},
	}

	meta := NewMeta(req, testPrompt, nil)

	if !slices.Equal(meta.ToolsRequested, []string{"Read", "Grep"}) {
		t.Errorf("ToolsRequested = %q, want %q", meta.ToolsRequested, []string{"Read", "Grep"})
	}
	if meta.ToolProfile != "research" {
		t.Errorf("ToolProfile = %q, want %q", meta.ToolProfile, "research")
	}
	if len(meta.Tools) != 0 {
		t.Errorf("Tools = %q, want empty: no init event ever arrived", meta.Tools)
	}
}

// TestMetaTodoWriteWorkedExample is the named regression for the finding that
// motivates the whole story: TodoWrite was requested on every run for the life
// of the project and silently dropped, with no error raised. Eight requested,
// seven resolved (IMPLEMENTATION_PLAN.md, "Settled before planning").
func TestMetaTodoWriteWorkedExample(t *testing.T) {
	req := metaRequest("/w/task-123")
	req.Permissions = Permissions{
		Profile: "research",
		Payload: ToolSurface{Tools: todoWriteRequested},
	}

	meta := NewMeta(req, testPrompt, &RunResult{
		Outcome:  OutcomeSucceeded,
		ExitCode: 0,
		Tools:    todoWriteResolved,
	})

	if len(meta.ToolsRequested) != 8 {
		t.Fatalf("requested %d tools, want 8: %q", len(meta.ToolsRequested), meta.ToolsRequested)
	}
	if len(meta.Tools) != 7 {
		t.Fatalf("resolved %d tools, want 7: %q", len(meta.Tools), meta.Tools)
	}
	if !slices.Contains(meta.ToolsRequested, "TodoWrite") {
		t.Error("TodoWrite is missing from the requested surface")
	}
	if slices.Contains(meta.Tools, "TodoWrite") {
		t.Error("TodoWrite resolved, contradicting the measured finding that it is silently dropped")
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
