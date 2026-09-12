package daemon_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type retentionClassifier struct{ calls atomic.Int64 }

func (c *retentionClassifier) Classify(context.Context, agent.Classification) (agent.Mode, error) {
	c.calls.Add(1)
	return agent.ModeResearch, nil
}

func retentionDaemon(t *testing.T, f *fixture, classifier agent.Classifier) *daemon.Daemon {
	t.Helper()
	classifiers := map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): classifier}
	d, err := daemon.New(daemon.Options{DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: classifiers, ToolProfiles: config.DefaultToolProfiles().Permissions, DB: f.db, Forge: f.client, Filter: f.filter, Turn: f.turns, Reporter: f.reporter, InstanceID: "retention", Interval: testInterval, Logger: f.logger, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assertRetentionLabels(t *testing.T, f *fixture, want ...string) {
	t.Helper()
	f.forge.mu.Lock()
	defer f.forge.mu.Unlock()
	var got []string
	for _, rs := range f.forge.repos {
		for _, issue := range rs.issues {
			if issue.Number == testIssue {
				for _, label := range issue.Labels {
					got = append(got, label.Name)
				}
			}
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("labels=%v, want %v", got, want)
	}
}
func assertRetiredSelection(t *testing.T, f *fixture, trigger int64) {
	t.Helper()
	if row, err := f.db.GetModeSelection(t.Context(), f.session().ID, trigger); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("journal=%+v err=%v, want retired", row, err)
	}
}

func TestPersistentModeAllModesAndOutcomes(t *testing.T) {
	for _, mode := range []struct {
		mode  agent.Mode
		label string
	}{{agent.ModeResearch, "Mode: Research"}, {agent.ModePlan, "Mode: Plan"}, {agent.ModeImplement, "Mode: Implement"}, {agent.ModeValidate, "Mode: Validate"}, {agent.ModeSpike, "Mode: Spike"}} {
		for _, failed := range []bool{false, true} {
			name := "success"
			if failed {
				name = "failure"
			}
			t.Run(string(mode.mode)+"/"+name, func(t *testing.T) {
				f := newFixture(t)
				classifier := &retentionClassifier{}
				f.daemon = retentionDaemon(t, f, classifier)
				f.plantCursor()
				f.issueWithLabels("Persistent stage", "bug", mode.label)
				f.settle()
				if failed {
					f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
						return f.turnResult(ctx, req, turn.DispositionRan, harness.OutcomeFailed)
					})
				}
				f.forge.addComment(testIssue, 501, owner(), "continue the work")
				f.settle()
				assertRetentionLabels(t, f, "bug", mode.label)
				assertRetiredSelection(t, f, 501)
				f.forge.addComment(testIssue, 502, stranger(), "ignore me")
				f.forge.addComment(testIssue, 503, bot(), turn.MarkerPrefix+" ignore me")
				f.forge.addComment(testIssue, 504, owner(), "continue again")
				for range 3 {
					f.settle()
				}
				if got := f.turns.triggers(); !slices.Equal(got, []int64{501, 504}) {
					t.Fatalf("triggers=%v", got)
				}
				for _, req := range f.turns.calls() {
					if req.Mode != mode.mode {
						t.Fatalf("mode=%q want %q", req.Mode, mode.mode)
					}
				}
				if classifier.calls.Load() != 0 {
					t.Fatal("retained mode invoked classifier")
				}
				if len(f.runs()) != 2 || f.watermark() != 504 {
					t.Fatalf("runs=%v watermark=%d", f.runs(), f.watermark())
				}
				for _, run := range f.runs() {
					if run.Status != store.RunReported {
						t.Fatalf("run=%+v", run)
					}
				}
				assertRetentionLabels(t, f, "bug", mode.label)
			})
		}
	}
}

func TestPersistentModeNewSelections(t *testing.T) {
	for _, tc := range []struct {
		name, body, label string
		mode              agent.Mode
		classifications   int64
	}{{"command", "/plan design this", "Mode: Plan", agent.ModePlan, 0}, {"classifier", "investigate this", "Mode: Research", agent.ModeResearch, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			classifier := &retentionClassifier{}
			f.daemon = retentionDaemon(t, f, classifier)
			f.plantCursor()
			f.issue("New selection")
			f.settle()
			f.forge.addComment(testIssue, 501, owner(), tc.body)
			f.settle()
			assertRetentionLabels(t, f, tc.label)
			assertRetiredSelection(t, f, 501)
			f.forge.addComment(testIssue, 502, owner(), "continue")
			f.settle()
			if classifier.calls.Load() != tc.classifications {
				t.Fatalf("classifier calls=%d", classifier.calls.Load())
			}
			calls := f.turns.calls()
			if len(calls) != 2 || calls[0].Mode != tc.mode || calls[1].Mode != tc.mode {
				t.Fatalf("calls=%+v", calls)
			}
			assertRetentionLabels(t, f, tc.label)
		})
	}
}

func TestPersistentModeReportRetryAfterRestart(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Retry report", "Mode: Plan")
	f.settle()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "interrupted"})
		return true
	}
	f.forge.addComment(testIssue, 501, owner(), "continue")
	f.settle()
	if _, err := f.db.GetModeSelection(t.Context(), f.session().ID, 501); err != nil {
		t.Fatal(err)
	}
	assertRetentionLabels(t, f, "Mode: Plan")
	f.forge.onPostComment = nil
	f.restorePendingReport(501)
	f.daemon = retentionDaemon(t, f, &retentionClassifier{})
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	assertRetentionLabels(t, f, "Mode: Plan")
	assertRetiredSelection(t, f, 501)
	if got := f.turns.triggers(); len(got) != 1 || got[0] != 501 {
		t.Fatalf("report recovery changed execution history: %v", got)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].Status != store.RunReported || f.watermark() < 501 {
		t.Fatalf("runs=%+v watermark=%d", runs, f.watermark())
	}
}

func TestPersistentModeLegacyDebtWithoutActivity(t *testing.T) {
	for _, classifierOn := range []bool{false, true} {
		for _, pending := range []bool{false, true} {
			for _, removed := range []bool{false, true} {
				name := "inferred"
				if pending {
					name = "explicit"
				}
				if classifierOn {
					name += "/classifier"
				} else {
					name += "/compatibility"
				}
				if removed {
					name += "/owner-removed"
				} else {
					name += "/retained"
				}
				t.Run(name, func(t *testing.T) {
					f := newFixture(t)
					f.plantCursor()
					f.issueWithLabels("Legacy debt", "bug", "Mode: Plan")
					f.settle()
					sid := f.session().ID
					if _, err := f.db.GetOrCreateModeSelection(t.Context(), store.ModeSelection{SessionID: sid, TriggerCommentID: 41, Mode: "plan", Label: "Mode: Plan"}); err != nil {
						t.Fatal(err)
					}
					if err := f.db.MarkModeLabelApplied(t.Context(), sid, 41); err != nil {
						t.Fatal(err)
					}
					if pending {
						if err := f.db.MarkModeCleanupPending(t.Context(), sid, 41); err != nil {
							t.Fatal(err)
						}
					} else {
						if _, err := f.db.AdvanceWatermark(t.Context(), sid, 41); err != nil {
							t.Fatal(err)
						}
					}
					if removed {
						f.issueWithLabels("Legacy debt", "bug")
					}
					f.clock.advance(pastListing)
					var classifier agent.Classifier
					if classifierOn {
						classifier = &retentionClassifier{}
					}
					f.daemon = retentionDaemon(t, f, classifier)
					if _, err := f.daemon.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					assertRetiredSelection(t, f, 41)
					f.settle()
					if removed {
						assertRetentionLabels(t, f, "bug")
					} else {
						assertRetentionLabels(t, f, "bug", "Mode: Plan")
					}
					if len(f.turns.calls()) != 0 {
						t.Fatal("retirement executed a turn")
					}
				})
			}
		}
	}
}

func TestPersistentModeAbandonedRunRecovery(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Interrupted execution", "Mode: Spike")
	f.settle()
	sid := f.session().ID
	f.allocateRunningRun(f.session(), 501)
	if _, err := f.db.GetOrCreateModeSelection(t.Context(), store.ModeSelection{SessionID: sid, TriggerCommentID: 501, Mode: "spike", Label: "Mode: Spike"}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.MarkModeLabelApplied(t.Context(), sid, 501); err != nil {
		t.Fatal(err)
	}
	f.daemon = retentionDaemon(t, f, &retentionClassifier{})
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertRetentionLabels(t, f, "Mode: Spike")
	assertRetiredSelection(t, f, 501)
	if runs := f.runs(); len(runs) != 1 || runs[0].Status != store.RunReported || f.watermark() != 501 {
		t.Fatalf("runs=%+v watermark=%d", runs, f.watermark())
	}
	if len(f.turns.calls()) != 0 {
		t.Fatal("recovery reran abandoned work")
	}
}
