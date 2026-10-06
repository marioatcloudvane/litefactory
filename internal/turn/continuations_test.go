package turn

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestInterruptionDurableBeforeAnnouncementAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name              string
		enabled, reliable bool
	}{
		{"enabled/schedules", true, true}, {"disabled/manual", false, true}, {"missing reset/manual", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				writeInWorktree(t, req, "partial.txt", "unfinished work\n")
				res := succeeded(req, f.paths.Home())
				res.Outcome = agent.OutcomeInterrupted
				res.ResultText = ""
				res.ResumeToken = "newly-discovered"
				res.Interruption = &agent.Interruption{Precision: agent.ResetExact}
				if tc.reliable {
					res.Interruption.ResetBound = res.FinishedAt.Add(time.Hour)
					res.Interruption.DueAt = res.Interruption.ResetBound.Add(time.Minute)
				}
				return res, nil
			})
			f = newFixture(t, runner)
			f.opts.AutoResumeOnSessionLimit = tc.enabled
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			result := f.execute(f.request())
			run := f.runs()[0]
			if run.SemanticOutcome != "interrupted" || run.Succeeded() || run.CommitSHA == "" {
				t.Fatalf("interrupted run = %+v", run)
			}
			bytes, err := os.ReadFile(filepath.Join(f.worktreeDir(), "partial.txt"))
			if err != nil || string(bytes) != "unfinished work\n" {
				t.Fatalf("partial work: %q %v", bytes, err)
			}
			segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
			if err != nil || segment.ResumeToken != "newly-discovered" {
				t.Fatalf("resume token: %+v %v", segment, err)
			}
			actions, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.enabled && tc.reliable {
				want = 1
			}
			if len(actions) != want {
				t.Fatalf("actions before post: %+v", actions)
			}
			if want == 0 && !strings.Contains(strings.ToLower(run.RecoveryReason), "manual") {
				t.Fatalf("missing manual explanation: %q", run.RecoveryReason)
			}
			forge.failWith(http.StatusInternalServerError)
			if _, err = reporter.ReportTurn(t.Context(), result); err == nil {
				t.Fatal("post failure ignored")
			}
			reports, err := f.db.ListPendingInterruptionReports(t.Context())
			if err != nil || len(reports) != 1 {
				t.Fatalf("failed post discovery = %+v %v", reports, err)
			}
			if want == 1 {
				saved, err := f.db.GetContinuation(t.Context(), actions[0].ID)
				if err != nil || saved.AnnouncementCommentID != 0 {
					t.Fatalf("failed post advanced announcement: %+v %v", saved, err)
				}
			}
			forge.recover()
			retry := f.execute(f.request())
			if retry.Disposition != DispositionPrepareFailed || runner.callCount() != 1 {
				t.Fatalf("pending delivery did not prevent execution: disposition=%v calls=%d", retry.Disposition, runner.callCount())
			}
			if _, pending, err := reporter.ReportPending(t.Context(), f.session.ID, run.TriggerCommentID); err == nil || !pending {
				t.Fatalf("empty lookup resolved uncertain interruption: pending=%v err=%v", pending, err)
			}
			if forge.attemptCount() != 1 || watermark(t, f) != 0 {
				t.Fatal("uncertain interruption reposted or consumed source")
			}
			intent, found, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, run.TriggerCommentID)
			if err != nil || !found {
				t.Fatalf("retained interruption intent: %v %v", found, err)
			}
			// Model the report having reached the forge before its response was
			// lost. Only exact retained bytes from the expected numeric bot and
			// destination authorize recovery; the prior empty lookup did not.
			forge.mu.Lock()
			forge.posts = append(forge.posts, forgePost{path: "/repos/acme/acme-api/issues/123/comments", body: intent.Body, id: 5001, authorID: 77})
			forge.mu.Unlock()
			posted, pending, err := newReporter(t, f, forge, nil).ReportPending(t.Context(), f.session.ID, run.TriggerCommentID)
			if err != nil || !pending {
				t.Fatalf("recover interruption: pending=%v err=%v", pending, err)
			}
			if posted.Run.SemanticOutcome != "interrupted" || posted.Run.Succeeded() || posted.Run.Status != store.RunReported {
				t.Fatalf("reported semantics: %+v", posted.Run)
			}
			post := forge.only(t)
			if !strings.Contains(post.body, "interrupted by") || !strings.Contains(post.body, "not complete") {
				t.Fatalf("untruthful report: %s", post.body)
			}
			if want == 1 {
				command := "/schedule -to " + actions[0].DueAt.UTC().Format("01/02/2006 15:04:05 UTC")
				if !strings.Contains(post.body, command) {
					t.Fatalf("missing absolute persisted command %q: %s", command, post.body)
				}
				saved, err := f.db.GetContinuation(t.Context(), actions[0].ID)
				if err != nil || saved.AnnouncementCommentID != post.id {
					t.Fatalf("confirmed announcement not recorded: %+v %v", saved, err)
				}
			}
			if _, err = reporter.ReportTurn(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			if runner.callCount() != 1 || len(forge.all()) != 1 || forge.attemptCount() != 1 {
				t.Fatalf("replay duplicated execution/report: runs=%d comments=%d", runner.callCount(), len(forge.all()))
			}
			actions, err = f.db.ListPendingContinuations(t.Context(), f.session.ID)
			if err != nil || len(actions) != want {
				t.Fatalf("replay duplicated action: %+v %v", actions, err)
			}
		})
	}
}

func TestInterruptionRetainsHandleOnResumedTurn(t *testing.T) {
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		if req.Path != harness.PathResume {
			t.Fatalf("request path=%v, want resume", req.Path)
		}
		res := succeeded(req, f.paths.Home())
		res.Outcome = agent.OutcomeInterrupted
		res.ResumeToken = "replacement-handle"
		res.Interruption = &agent.Interruption{}
		return res, nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, "implement", store.RunSucceeded)
	f.execute(f.request())
	runs := f.runs()
	latest := runs[len(runs)-1]
	segment, err := f.db.GetSegment(t.Context(), latest.SegmentID)
	if err != nil || segment.ResumeToken != "replacement-handle" || latest.SemanticOutcome != "interrupted" {
		t.Fatalf("resumed interruption handle lost: %+v %+v %v", latest, segment, err)
	}
}
