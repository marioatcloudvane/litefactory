package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestModeTransitionManualEditsAndRemoval(t *testing.T) {
	f := newFixture(t)
	classifier := &retentionClassifier{}
	f.daemon = retentionDaemon(t, f, classifier)
	f.plantCursor()
	f.issueWithLabels("Manual selection", "Mode: Plan", "bug")
	f.settle()
	for i, tc := range []struct {
		label string
		want  agent.Mode
	}{{"Mode: Plan", agent.ModePlan}, {"Mode: Validate", agent.ModeValidate}, {"", agent.ModeResearch}} {
		labels := []string{"bug"}
		if tc.label != "" {
			labels = append(labels, tc.label)
		}
		f.issueWithLabels("Manual selection", labels...)
		f.forge.addComment(testIssue, int64(501+i), owner(), "continue")
		f.settle()
		calls := f.turns.calls()
		if len(calls) != i+1 || calls[i].Mode != tc.want {
			t.Fatalf("iteration %d calls=%+v logs=%s", i, calls, f.logs)
		}
	}
	if classifier.calls.Load() != 1 {
		t.Fatalf("classification calls=%d", classifier.calls.Load())
	}
	assertRetentionLabels(t, f, "bug", "Mode: Research")
}

func TestModeTransitionRejectsAmbiguousLabelsEvenWithCommand(t *testing.T) {
	for _, labels := range [][]string{{"Mode: Plan", "Mode: Spike"}, {"mode: Plan"}, {"Mode: Deploy"}} {
		t.Run(strings.Join(labels, "+"), func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Invalid mode", labels...)
			f.settle()
			f.forge.addComment(testIssue, 501, owner(), "/implement build this")
			f.settle()
			if len(f.turns.calls()) != 0 || len(f.runs()) != 0 {
				t.Fatal("invalid labels reached execution")
			}
			bodies, _, _ := f.forge.snapshot()
			if len(bodies) != 1 || !strings.Contains(bodies[0], "Mode:") || !strings.Contains(strings.ToLower(bodies[0]), "nothing was run") {
				t.Fatalf("validation reply=%v", bodies)
			}
			assertRetentionLabels(t, f, labels...)
		})
	}
}

func TestModeTransitionFailureAndRestart(t *testing.T) {
	for _, effect := range []string{"remove", "add"} {
		t.Run(effect, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Recover selection", "Mode: Plan", "bug")
			f.settle()
			fail := func(w http.ResponseWriter, _ *http.Request) bool {
				http.Error(w, "interrupted label effect", http.StatusServiceUnavailable)
				return true
			}
			if effect == "remove" {
				f.forge.onRemoveLabel = fail
			} else {
				f.forge.onAddLabels = fail
			}
			f.forge.addComment(testIssue, 501, owner(), "/implement build")
			f.settle()
			if len(f.turns.calls()) != 0 {
				t.Fatal("subprocess ran before label effects confirmed")
			}
			row, err := f.db.GetModeSelection(t.Context(), f.session().ID, 501)
			if err != nil || row.Mode != "implement" || row.PreviousLabel != "Mode: Plan" || row.LabelApplied {
				t.Fatalf("durable intent=%+v err=%v", row, err)
			}
			if effect == "remove" {
				assertRetentionLabels(t, f, "Mode: Plan", "bug")
			} else {
				assertRetentionLabels(t, f, "bug")
			}
			f.forge.onRemoveLabel = nil
			f.forge.onAddLabels = nil
			f.daemon = retentionDaemon(t, f, &retentionClassifier{})
			if _, err := f.daemon.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Mode != agent.ModeImplement || calls[0].ToolProfile != "implement" {
				t.Fatalf("recovered calls=%+v logs=%s", calls, f.logs)
			}
			assertRetentionLabels(t, f, "Mode: Implement", "bug")
			assertRetiredSelection(t, f, 501)
		})
	}
}

func TestModeTransitionConflictDuringRecoveryPreservesOwnerSelection(t *testing.T) {
	for _, ownerLabel := range []string{"Mode: Research", "Mode: Plan"} {
		t.Run(ownerLabel, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Conflicting edit", "Mode: Plan", "bug")
			f.settle()
			f.forge.onAddLabels = func(w http.ResponseWriter, _ *http.Request) bool {
				http.Error(w, "interrupted", http.StatusServiceUnavailable)
				return true
			}
			f.forge.addComment(testIssue, 501, owner(), "/implement build")
			f.settle()
			if len(f.turns.calls()) != 0 {
				t.Fatal("executed unfinished transition")
			}
			f.forge.onAddLabels = nil
			f.issueWithLabels("Conflicting edit", ownerLabel, "bug")
			f.daemon = retentionDaemon(t, f, &retentionClassifier{})
			if _, err := f.daemon.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.settle()
			if len(f.turns.calls()) != 0 {
				t.Fatal("executed conflicting transition")
			}
			assertRetentionLabels(t, f, ownerLabel, "bug")
			bodies, _, _ := f.forge.snapshot()
			if len(bodies) == 0 {
				t.Fatalf("conflict not visible: %s", f.logs)
			}
		})
	}
}

func TestModeTransitionCrashBeforeJournalAcknowledgement(t *testing.T) {
	for _, stage := range []string{"removed", "added"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			labels := []string{"bug"}
			if stage == "added" {
				labels = append(labels, "Mode: Implement")
			}
			f.issueWithLabels("Effect before crash", labels...)
			f.settle()
			_, err := f.db.GetOrCreateModeSelection(t.Context(), store.ModeSelection{SessionID: f.session().ID, TriggerCommentID: 501, Mode: "implement", Label: "Mode: Implement", PreviousLabel: "Mode: Plan"})
			if err != nil {
				t.Fatal(err)
			}
			f.forge.addComment(testIssue, 501, owner(), "/implement build")
			f.daemon = retentionDaemon(t, f, &retentionClassifier{})
			if _, err := f.daemon.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Mode != agent.ModeImplement {
				t.Fatalf("calls=%+v logs=%s", calls, f.logs)
			}
			assertRetentionLabels(t, f, "bug", "Mode: Implement")
		})
	}
}

func TestModeTransitionConfirmedRetryPinsModeAndPreservesLaterOwnerEdit(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Pinned execution", "Mode: Plan", "bug")
	f.settle()
	first := true
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if first {
			first = false
			return turn.Result{}, errors.New("interrupted before execution")
		}
		return f.turnResult(ctx, req, turn.DispositionRan, harness.OutcomeSucceeded)
	})
	f.forge.addComment(testIssue, 501, owner(), "/implement build")
	f.settle()
	row, err := f.db.GetModeSelection(t.Context(), f.session().ID, 501)
	if err != nil || !row.LabelApplied {
		t.Fatalf("confirmation=%+v err=%v", row, err)
	}
	f.issueWithLabels("Pinned execution", "Mode: Validate", "bug")
	f.daemon = f.newDaemon(daemon.Options{})
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 2 {
		t.Fatalf("calls=%+v logs=%s", calls, f.logs)
	}
	for _, req := range calls {
		if req.Mode != agent.ModeImplement || req.ToolProfile != "implement" || !equalStringSlices(req.Tools, config.DefaultToolProfiles().Permissions["implement"].Claude.Tools) {
			t.Fatalf("retry changed permissions=%+v", req)
		}
	}
	assertRetentionLabels(t, f, "Mode: Validate", "bug")
	f.forge.addComment(testIssue, 502, owner(), "continue")
	f.settle()
	calls = f.turns.calls()
	if len(calls) != 3 || calls[2].Mode != agent.ModeValidate {
		t.Fatalf("next trigger=%+v", calls)
	}
}

func TestModeTransitionReportRetryPreservesOwnerEdit(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Report edit", "Mode: Plan", "bug")
	f.settle()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, "interrupted report", http.StatusServiceUnavailable)
		return true
	}
	f.forge.addComment(testIssue, 501, owner(), "/implement build")
	f.settle()
	f.issueWithLabels("Report edit", "Mode: Research", "bug")
	f.forge.onPostComment = nil
	f.daemon = f.newDaemon(daemon.Options{})
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	assertRetentionLabels(t, f, "Mode: Research", "bug")
	assertRetiredSelection(t, f, 501)
	if f.watermark() != 501 {
		t.Fatalf("watermark=%d", f.watermark())
	}
}

func TestModeTransitionChildJournalSurvivesParentWatermarkAndRestart(t *testing.T) {
	f := managedRefinementFixture(t)
	parent := f.sessionOf(testIssue)
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Implement", "bug")})
	f.forge.onAddLabels = func(w http.ResponseWriter, _ *http.Request) bool {
		http.Error(w, "interrupted", http.StatusServiceUnavailable)
		return true
	}
	f.forge.addComment(200, 60, owner(), "refine acceptance criteria")
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatal("incomplete child transition executed")
	}
	row, err := f.db.GetModeSelection(t.Context(), parent.ID, 60)
	if err != nil || row.Mode != "plan" || row.LabelApplied {
		t.Fatalf("child intent=%+v err=%v logs=%s", row, err, f.logs)
	}
	if _, err := f.db.AdvanceWatermark(t.Context(), parent.ID, 100); err != nil {
		t.Fatal(err)
	}
	f.forge.onAddLabels = nil
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Research", "bug")})
	f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.GetModeSelection(t.Context(), parent.ID, 60); err != nil {
		t.Fatalf("restart retired unfinished child journal: %v", err)
	}
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatal("conflicting child transition executed after restart")
	}
	assertRetentionLabels(t, f, "Mode: Research", "bug")
	bodies, _, _ := f.forge.snapshot()
	if !hasModeRefusal(bodies) {
		t.Fatalf("child conflict invisible: reports=%v logs=%s", bodies, f.logs)
	}
}

func hasModeRefusal(bodies []string) bool {
	for _, body := range bodies {
		lower := strings.ToLower(body)
		if strings.Contains(lower, "mode") && strings.Contains(lower, "nothing was run") {
			return true
		}
	}
	return false
}
