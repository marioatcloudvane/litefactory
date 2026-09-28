package turn

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// AC-006 (story-002): facts render one per line, in fixed stage order
// (analysis, repair, checks, delivery, overflow last), keeping each stage's
// own recorded order stable.
func TestBugFactLines_StageOrder(t *testing.T) {
	t.Parallel()
	facts := []store.BugFact{
		{Stage: store.BugFactStageDelivery, Reference: "d1", Text: "delivery fact"},
		{Stage: store.BugFactStageOverflow, Reference: "overflow", Text: "3 further facts were dropped"},
		{Stage: store.BugFactStageAnalysis, Reference: "a1", Text: "first analysis fact"},
		{Stage: store.BugFactStageChecks, Reference: "c1", Text: "checks fact"},
		{Stage: store.BugFactStageAnalysis, Reference: "a2", Text: "second analysis fact"},
		{Stage: store.BugFactStageRepair, Reference: "r1", Text: "repair fact"},
	}
	got := bugFactLines(facts)
	want := []string{
		"[analysis] first analysis fact",
		"[analysis] second analysis fact",
		"[repair] repair fact",
		"[checks] checks fact",
		"[delivery] delivery fact",
		"[overflow] 3 further facts were dropped",
	}
	if len(got) != len(want) {
		t.Fatalf("bugFactLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBugFactLines_Empty(t *testing.T) {
	t.Parallel()
	if got := bugFactLines(nil); len(got) != 0 {
		t.Fatalf("empty facts produced lines: %v", got)
	}
}

// bugFactLines must never mutate the caller's slice order (it copies before
// sorting).
func TestBugFactLines_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	facts := []store.BugFact{
		{Stage: store.BugFactStageDelivery, Text: "d"},
		{Stage: store.BugFactStageAnalysis, Text: "a"},
	}
	original := append([]store.BugFact(nil), facts...)
	_ = bugFactLines(facts)
	for i := range facts {
		if facts[i] != original[i] {
			t.Fatalf("input slice was mutated: %v vs %v", facts, original)
		}
	}
}

// AC-009 (story-003): the delivery review's one-line self-check evidence is
// computed only from factory-recorded runs, never the fixer's own report.
func TestBugSelfCheckLine(t *testing.T) {
	t.Parallel()
	t.Run("no runs/plain no-self-check line", func(t *testing.T) {
		t.Parallel()
		got := bugSelfCheckLine(nil)
		if got != "The fixer did not run its own checks." {
			t.Fatalf("bugSelfCheckLine(nil) = %q", got)
		}
	})
	t.Run("one run singular wording", func(t *testing.T) {
		t.Parallel()
		runs := []store.BugSelfCheckSummary{{
			RunID: "selfcheck:repair-1:1", Ordinal: 1,
			Phases: []store.BugSelfCheckPhaseSummary{{Phase: "regression", Outcome: "passed"}, {Phase: "focused", Outcome: "passed"}},
		}}
		got := bugSelfCheckLine(runs)
		want := "The fixer ran its own checks 1 time; last result: regression passed, focused passed."
		if got != want {
			t.Fatalf("bugSelfCheckLine = %q, want %q", got, want)
		}
	})
	t.Run("multiple runs plural wording uses the last run's phases", func(t *testing.T) {
		t.Parallel()
		runs := []store.BugSelfCheckSummary{
			{RunID: "selfcheck:repair-1:1", Ordinal: 1, Phases: []store.BugSelfCheckPhaseSummary{{Phase: "regression", Outcome: "failed"}}},
			{RunID: "selfcheck:repair-1:2", Ordinal: 2, Phases: []store.BugSelfCheckPhaseSummary{{Phase: "regression", Outcome: "passed"}, {Phase: "focused", Outcome: "failed"}}},
		}
		got := bugSelfCheckLine(runs)
		want := "The fixer ran its own checks 2 times; last result: regression passed, focused failed."
		if got != want {
			t.Fatalf("bugSelfCheckLine = %q, want %q", got, want)
		}
	})
}

// AC-011 (story-003): the output shown to the fixer is redacted and capped at
// the last BugSelfCheckOutputCap bytes per failing command; passed commands
// contribute no failing-command evidence at all.
func TestBugSelfCheckPhaseViews_CapsAndFiltersFailures(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("x", BugSelfCheckOutputCap+500) + "TAIL"
	exit := 1
	phases := []store.BugCheckPhaseRecord{
		{
			Binding: store.BugCheckBinding{Phase: "regression"}, Outcome: "failed",
			Commands: []store.BugCheckCommandRecord{
				{Observation: store.BugCheckObservation{Outcome: "passed", Executable: "go build ./..."}},
				{Observation: store.BugCheckObservation{Outcome: "failed", Executable: "go test ./...", Exit: &exit, Output: longOutput}},
			},
		},
		{
			Binding: store.BugCheckBinding{Phase: "focused"}, Outcome: "cancelled",
			Commands: []store.BugCheckCommandRecord{
				{Observation: store.BugCheckObservation{Outcome: "cancelled", Executable: "go vet ./...", Output: "short"}},
			},
		},
	}
	got := bugSelfCheckPhaseViews(phases)
	if len(got) != 2 {
		t.Fatalf("want 2 phase views, got %d: %+v", len(got), got)
	}
	regression := got[0]
	if regression.Name != "regression" || regression.Outcome != "failed" {
		t.Fatalf("regression view = %+v", regression)
	}
	if len(regression.FailingCommands) != 1 {
		t.Fatalf("passed command leaked into failing commands: %+v", regression.FailingCommands)
	}
	fc := regression.FailingCommands[0]
	if fc.ArgvSummary != "go test ./..." || fc.Exit == nil || *fc.Exit != 1 {
		t.Fatalf("failing command evidence wrong: %+v", fc)
	}
	if len(fc.OutputTail) != BugSelfCheckOutputCap+len("…") {
		t.Fatalf("output tail not capped: got %d bytes", len(fc.OutputTail))
	}
	if !strings.HasSuffix(fc.OutputTail, "TAIL") || !strings.HasPrefix(fc.OutputTail, "…") {
		t.Fatalf("output tail lost its end or cap marker: %q", fc.OutputTail[:20]+"...")
	}
	focused := got[1]
	if focused.Name != "focused" || focused.Outcome != "cancelled" {
		t.Fatalf("focused view = %+v", focused)
	}
	if len(focused.FailingCommands) != 1 || focused.FailingCommands[0].OutputTail != "short" {
		t.Fatalf("uncapped short output changed: %+v", focused.FailingCommands)
	}
}

func TestBugSelfCheckPhaseViews_Empty(t *testing.T) {
	t.Parallel()
	if got := bugSelfCheckPhaseViews(nil); len(got) != 0 {
		t.Fatalf("empty phases produced views: %v", got)
	}
}

// AC-003 (story-003): bug_self_check_start's reply shape carries exactly one
// of run_id (started/pending), unavailable or limit_reached; unused fields are
// omitted rather than sent as zero values.
func TestBugSelfCheckStartResponse_ShapeOmitsUnusedFields(t *testing.T) {
	t.Parallel()
	t.Run("started/pending", func(t *testing.T) {
		t.Parallel()
		raw, err := json.Marshal(bugSelfCheckStartResponse{RunID: "selfcheck:repair-1:1", Ordinal: 1, Remaining: 4})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"run_id", "ordinal", "remaining"} {
			if _, ok := decoded[field]; !ok {
				t.Errorf("started response missing %q: %s", field, raw)
			}
		}
		for _, field := range []string{"unavailable", "limit_reached"} {
			if _, ok := decoded[field]; ok {
				t.Errorf("started response leaked unused field %q: %s", field, raw)
			}
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		t.Parallel()
		raw, err := json.Marshal(bugSelfCheckStartResponse{Unavailable: "self-check sandbox is not configured for this attempt"})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["unavailable"] != "self-check sandbox is not configured for this attempt" {
			t.Errorf("unavailable reason missing/wrong: %s", raw)
		}
		for _, field := range []string{"run_id", "ordinal", "remaining", "limit_reached"} {
			if _, ok := decoded[field]; ok {
				t.Errorf("unavailable response leaked unused field %q: %s", field, raw)
			}
		}
	})
	t.Run("limit_reached", func(t *testing.T) {
		t.Parallel()
		count := 5
		raw, err := json.Marshal(bugSelfCheckStartResponse{LimitReached: &count})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["limit_reached"] != float64(5) {
			t.Errorf("limit_reached count missing/wrong: %s", raw)
		}
		for _, field := range []string{"run_id", "ordinal", "remaining", "unavailable"} {
			if _, ok := decoded[field]; ok {
				t.Errorf("limit_reached response leaked unused field %q: %s", field, raw)
			}
		}
	})
	t.Run("unavailableResponse helper marshals to the unavailable shape", func(t *testing.T) {
		t.Parallel()
		raw, err := unavailableResponse(errors.New("sandbox unavailable: recipe missing"))
		if err != nil {
			t.Fatal(err)
		}
		var decoded bugSelfCheckStartResponse
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Unavailable != "sandbox unavailable: recipe missing" || decoded.RunID != "" || decoded.LimitReached != nil {
			t.Fatalf("unavailableResponse shape wrong: %+v", decoded)
		}
	})
}

// AC-003, AC-011 (story-003): bug_self_check_status's reply shape names each
// phase's outcome and, for a failing command, its argv summary, exit code and
// redacted output tail.
func TestBugSelfCheckStatusResponse_Shape(t *testing.T) {
	t.Parallel()
	exit := 1
	resp := bugSelfCheckStatusResponse{
		State: "done", Remaining: 3,
		Phases: []bugSelfCheckPhaseView{
			{Name: "regression", Outcome: "passed"},
			{Name: "focused", Outcome: "failed", FailingCommands: []bugSelfCheckFailingCommand{{ArgvSummary: "go test ./...", Exit: &exit, OutputTail: "FAIL"}}},
		},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["state"] != "done" {
		t.Fatalf("state missing/wrong: %s", raw)
	}
	phases, ok := decoded["phases"].([]any)
	if !ok || len(phases) != 2 {
		t.Fatalf("phases missing/wrong shape: %s", raw)
	}
	regression, ok := phases[0].(map[string]any)
	if !ok || regression["name"] != "regression" || regression["outcome"] != "passed" {
		t.Fatalf("regression phase shape wrong: %s", raw)
	}
	if _, ok := regression["failing_commands"]; ok {
		t.Errorf("passed phase leaked a failing_commands field: %s", raw)
	}
	focused, ok := phases[1].(map[string]any)
	if !ok || focused["name"] != "focused" || focused["outcome"] != "failed" {
		t.Fatalf("focused phase shape wrong: %s", raw)
	}
	failing, ok := focused["failing_commands"].([]any)
	if !ok || len(failing) != 1 {
		t.Fatalf("failing command evidence missing: %s", raw)
	}
	cmd, ok := failing[0].(map[string]any)
	if !ok || cmd["argv_summary"] != "go test ./..." || cmd["exit"] != float64(1) || cmd["output_tail"] != "FAIL" {
		t.Fatalf("failing command shape wrong: %+v", cmd)
	}
}

// Sanity: BugSelfCheckWait and BugSelfCheckOutputCap are bounded as documented
// (30s status wait ceiling under every provider's MCP tool timeout; 8 KiB
// per-command output cap).
func TestBugSelfCheckConstants(t *testing.T) {
	t.Parallel()
	if BugSelfCheckWait != 30*time.Second {
		t.Fatalf("BugSelfCheckWait = %v, want 30s", BugSelfCheckWait)
	}
	if BugSelfCheckOutputCap != 8<<10 {
		t.Fatalf("BugSelfCheckOutputCap = %d, want 8KiB", BugSelfCheckOutputCap)
	}
}
