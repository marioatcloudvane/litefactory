package turn

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
)

// C7: a POST that GitHub refused with a rate limit is recorded as not sent, so
// the next due recovery posts it exactly once; any other failure stays possibly
// sent and is never reposted blind.
func TestReportDeliveryRateLimitRefusalIsRecordedAsNotSent(t *testing.T) {
	for name, status := range map[string]int{"rate limited": http.StatusTooManyRequests, "other failure": http.StatusServiceUnavailable} {
		t.Run(name, func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				writeInWorktree(t, req, "health.go", "package api\n")
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			result := f.execute(f.request())
			forge.failWith(status)
			_, err := reporter.ReportTurn(t.Context(), result)
			var delivery *DeliveryError
			if !errors.As(err, &delivery) {
				t.Fatalf("failed post: %v", err)
			}
			if rateLimited := errors.Is(err, github.ErrRateLimited); rateLimited != (status == http.StatusTooManyRequests) {
				t.Fatalf("rate-limit classification %v for status %d: %v", rateLimited, status, err)
			}
			saved, err := f.db.GetReportDelivery(t.Context(), delivery.Key)
			if err != nil {
				t.Fatal(err)
			}
			if status != http.StatusTooManyRequests {
				if !saved.PossiblySent || delivery.State != "uncertain" {
					t.Fatalf("ordinary failure cleared the sent mark: %+v state=%s", saved, delivery.State)
				}
				return
			}
			if saved.PossiblySent || saved.CommentID != 0 || saved.Completed || delivery.State != "pending" {
				t.Fatalf("rate-limited post not recorded as not sent: %+v state=%s", saved, delivery.State)
			}
			forge.recover()
			attempts := forge.attemptCount()
			if _, pending, err := reporter.ReportPending(t.Context(), f.session.ID, result.TriggerCommentID); err != nil || !pending {
				t.Fatalf("recovery: pending=%v err=%v", pending, err)
			}
			done, err := f.db.GetReportDelivery(t.Context(), delivery.Key)
			if err != nil {
				t.Fatal(err)
			}
			if !done.Completed || done.CommentID == 0 || forge.attemptCount() != attempts+1 || len(forge.all()) != 1 {
				t.Fatalf("recovery did not post exactly once: %+v attempts=%d posts=%d", done, forge.attemptCount()-attempts, len(forge.all()))
			}
			if _, pending, err := reporter.ReportPending(t.Context(), f.session.ID, result.TriggerCommentID); err != nil || pending || len(forge.all()) != 1 {
				t.Fatalf("completed delivery reposted: pending=%v err=%v", pending, err)
			}
		})
	}
}
