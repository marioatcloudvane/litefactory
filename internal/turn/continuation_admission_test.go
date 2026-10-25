package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
)

func TestAutomaticResumeNeverColdFallsBack(t *testing.T) {
	for _, outcome := range []string{"invalid session", "quota refusal", "success"} {
		t.Run(outcome, func(t *testing.T) {
			var f *fixture
			calls := 0
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				calls++
				res := succeeded(req, f.paths.Home())
				if calls == 1 {
					res.Outcome = agent.OutcomeInterrupted
					res.Interruption = &agent.Interruption{Precision: agent.ResetExact, ResetBound: res.FinishedAt.Add(time.Hour), DueAt: res.FinishedAt.Add(time.Hour + time.Minute)}
					return res, nil
				}
				if req.Path != harness.PathResume {
					t.Fatalf("automatic path=%v", req.Path)
				}
				switch outcome {
				case "invalid session":
					return nil, agent.ErrSessionNotResumable
				case "quota refusal":
					res.Outcome = agent.OutcomeInterrupted
					res.Interruption = &agent.Interruption{}
				}
				return res, nil
			})
			f = newFixture(t, runner)
			f.opts.AutoResumeOnSessionLimit = true
			f.execute(f.request())
			pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending=%+v err=%v", pending, err)
			}
			c, ok, err := f.db.ReserveContinuation(t.Context(), pending[0].ID, pending[0].DueAt)
			if err != nil || !ok {
				t.Fatalf("reserve=%v %v", ok, err)
			}
			req := f.request()
			req.Continuation = &c
			req.Trigger = Trigger{CommentID: c.TriggerID(), Body: c.Context.Request, Author: req.Trigger.Author}
			req.Command = Command{Kind: KindFreeText, Text: c.Context.Request}
			result := f.execute(req)
			if calls != 2 {
				t.Fatalf("automatic invocation fell back: calls=%d", calls)
			}
			segments, err := f.db.ListSegments(t.Context(), f.session.ID)
			if err != nil || len(segments) != 1 {
				t.Fatalf("automatic recovery changed segment: %+v %v", segments, err)
			}
			if outcome != "success" && result.Run.Succeeded() {
				t.Fatalf("failure reported success: %+v", result.Run)
			}
		})
	}
}

func TestAutomaticCancellationWinsBeforeAllocation(t *testing.T) {
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		res := succeeded(req, f.paths.Home())
		res.Outcome = agent.OutcomeInterrupted
		res.Interruption = &agent.Interruption{Precision: agent.ResetExact, ResetBound: res.FinishedAt.Add(time.Hour), DueAt: res.FinishedAt.Add(time.Hour + time.Minute)}
		return res, nil
	})
	f = newFixture(t, runner)
	f.opts.AutoResumeOnSessionLimit = true
	f.execute(f.request())
	pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v %v", pending, err)
	}
	c, ok, err := f.db.ReserveContinuation(t.Context(), pending[0].ID, pending[0].DueAt)
	if err != nil || !ok {
		t.Fatalf("reserve=%v %v", ok, err)
	}
	if err = f.db.CancelContinuation(t.Context(), c.ID, "owner superseded", c.DueAt); err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.Continuation = &c
	req.Trigger.CommentID = c.TriggerID()
	req.Trigger.Body = c.Context.Request
	req.Command = Command{Kind: KindFreeText, Text: c.Context.Request}
	result, err := Execute(t.Context(), f.opts, req)
	if err == nil && result.Run.ID != 0 {
		t.Fatalf("cancelled recovery allocated run: %+v", result)
	}
	if runner.callCount() != 1 || len(f.runs()) != 1 {
		t.Fatalf("cancelled action invoked or allocated: calls=%d runs=%d", runner.callCount(), len(f.runs()))
	}
	saved, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || saved.Status != "cancelled" || saved.AllocatedRunID != 0 {
		t.Fatalf("cancelled state=%+v %v", saved, err)
	}
}

func TestAutomaticManagedResumePreservesFrozenScope(t *testing.T) {
	for _, changedGeneration := range []bool{false, true} {
		t.Run(map[bool]string{false: "frozen scope and completed dependency", true: "generation changed refuses invocation"}[changedGeneration], func(t *testing.T) {
			var f *fixture
			var first agent.ApprovedBundle
			calls := 0
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				bundle := approvedPromptBundle(t, req.Prompt)
				if calls == 1 {
					first = bundle
					event := progressEvent(t, f, bundle.RunID, req.WorktreePath)
					writeProgressEvent(t, req.WorktreePath, event)
					mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
				} else {
					if req.Path != agent.PathResume || bundle.Generation != first.Generation || len(bundle.Stories) != 2 || bundle.Stories[1].Key != "US-2" || bundle.Stories[1].Body != first.Stories[1].Body || bundle.Stories[1].Revision != first.Stories[1].Revision || bundle.Stories[0].State != "completed" {
						t.Fatalf("automatic frozen scope lost: %+v path=%v", bundle, req.Path)
					}
					authorized, err := f.db.StoryProgressAuthorized(t.Context(), bundle.RunID, "US-1")
					mustProgress(t, err)
					if authorized {
						t.Fatal("completed dependency authorized for new verification")
					}
					if len(bundle.CompletedDependencies) != 1 || bundle.CompletedDependencies[0].Key != "US-1" {
						t.Fatalf("completed scope not read-only dependency: %+v", bundle)
					}
				}
				res := succeeded(req, f.paths.Home())
				res.ResumeToken = "automatic-frozen-session"
				if calls == 1 {
					res.Outcome = agent.OutcomeInterrupted
					res.Interruption = &agent.Interruption{Precision: agent.ResetExact, ResetBound: res.FinishedAt.Add(time.Hour), DueAt: res.FinishedAt.Add(time.Hour + time.Minute)}
				}
				return res, nil
			})
			f = newFixture(t, runner)
			f.opts.AutoResumeOnSessionLimit = true
			forge, _ := setupProgressScope(t, f)
			req := f.request()
			req.Mode = agent.ModeImplement
			req.Command = ParseCommand("/implement original frozen request")
			req.Trigger.Body = "/implement original frozen request"
			f.execute(req)
			pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending=%+v %v", pending, err)
			}
			if pending[0].Context.ApprovedGeneration != 1 {
				t.Fatalf("generation not persisted: %+v", pending[0])
			}
			if changedGeneration {
				mustProgress(t, f.db.ReplanManagedFeature(t.Context(), f.session.TaskRef, 1))
			} else {
				child := forge.children[201]
				child.Body = "live edits must never become execution instructions"
				forge.children[201] = child
			}
			c, ok, err := f.db.ReserveContinuation(t.Context(), pending[0].ID, pending[0].DueAt)
			if err != nil || !ok {
				t.Fatalf("reserve=%v %v", ok, err)
			}
			req.Continuation = &c
			req.Trigger.CommentID = c.TriggerID()
			req.Trigger.Body = c.Context.Request
			req.Command = Command{Kind: KindFreeText, Text: c.Context.Request}
			result, executeErr := Execute(t.Context(), f.opts, req)
			if changedGeneration {
				if executeErr == nil || !strings.Contains(executeErr.Error(), "approved story generation changed") {
					t.Fatalf("changed identity was not refused: %v", executeErr)
				}
				if calls != 1 || len(f.runs()) != 1 || result.Run.ID != 0 || result.Harness != nil {
					t.Fatalf("changed generation invoked: calls=%d result=%+v", calls, result)
				}
			} else if executeErr != nil || calls != 2 || !result.Succeeded() {
				t.Fatalf("frozen automatic resume failed: calls=%d result=%+v", calls, result)
			}
		})
	}
}
