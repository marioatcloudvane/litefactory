package turn

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// C7: a POST that GitHub refused with a rate limit is recorded as not sent, so
// the next due recovery posts it exactly once; any other failure stays possibly
// sent until a complete lookup on a later recovery settles it.
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

// failedRunReport reports one run whose POST the forge answers with a 500,
// after prepare has set the forge's failure mode, and returns the retained
// possibly-sent delivery.
func failedRunReport(t *testing.T, prepare func(*fakeForge)) (*fixture, *fakeForge, *Reporter, int64, store.ReportDelivery) {
	t.Helper()
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	result := f.execute(f.request())
	prepare(forge)
	if _, err := reporter.ReportTurn(t.Context(), result); err == nil {
		t.Fatal("ReportTurn = nil error, want the forge failure")
	}
	forge.recover()
	delivery, found, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, result.TriggerCommentID)
	if err != nil || !found || !delivery.PossiblySent {
		t.Fatalf("failed post not possibly sent: found=%v err=%v %+v", found, err, delivery)
	}
	return f, forge, reporter, result.TriggerCommentID, delivery
}

// C2: a comment the forge created behind a 500 is found by the lookup and
// recorded as the receipt; nothing is reposted.
func TestReportDeliveryVisibleReceiptAfterFailureIsNotReposted(t *testing.T) {
	f, forge, reporter, trigger, delivery := failedRunReport(t, func(forge *fakeForge) { forge.failCreatingWith(http.StatusInternalServerError, false) })
	if _, pending, err := reporter.ReportPending(t.Context(), f.session.ID, trigger); err != nil || !pending {
		t.Fatalf("recovery: pending=%v err=%v", pending, err)
	}
	post := forge.only(t)
	done, err := f.db.GetReportDelivery(t.Context(), delivery.Key)
	if err != nil || !done.Completed || done.CommentID != post.id || forge.attemptCount() != 1 {
		t.Fatalf("receipt not recorded without repost: %+v err=%v attempts=%d", done, err, forge.attemptCount())
	}
	if watermark(t, f) != trigger {
		t.Fatal("receipt did not consume the source")
	}
}

// C5: at-least-once delivery. A comment created behind a 500 but hidden from
// the later lookup is reposted once; both copies carry the same bytes and
// marker, and recovery then settles without a further POST or error.
func TestReportDeliveryHiddenReceiptIsRepostedOnce(t *testing.T) {
	f, forge, reporter, trigger, delivery := failedRunReport(t, func(forge *fakeForge) { forge.failCreatingWith(http.StatusInternalServerError, true) })
	if _, pending, err := reporter.ReportPending(t.Context(), f.session.ID, trigger); err != nil || !pending {
		t.Fatalf("recovery: pending=%v err=%v", pending, err)
	}
	if forge.attemptCount() != 2 {
		t.Fatalf("attempts = %d, want 2", forge.attemptCount())
	}
	forge.unhide()
	posts := forge.all()
	if len(posts) != 2 {
		t.Fatalf("comments = %d, want 2", len(posts))
	}
	for _, post := range posts {
		if post.body != delivery.Body || !strings.Contains(post.body, delivery.Marker) {
			t.Fatal("a copy differs from the retained body or lacks its marker")
		}
	}
	done, err := f.db.GetReportDelivery(t.Context(), delivery.Key)
	if err != nil || !done.Completed || done.CommentID != posts[1].id {
		t.Fatalf("delivery not completed with the repost: %+v %v", done, err)
	}
	if _, pending, err := reporter.ReportPending(t.Context(), f.session.ID, trigger); err != nil || pending || forge.attemptCount() != 2 {
		t.Fatalf("settled delivery looped: pending=%v err=%v attempts=%d", pending, err, forge.attemptCount())
	}
}

// C6: with the real renderer, a marker-bearing comment that cannot be trusted
// keeps the delivery uncertain and never authorizes a repost; a genuine
// absence reposts exactly once.
func TestReportDeliveryUntrustedMarkerCommentBlocksRepost(t *testing.T) {
	for _, name := range []string{"wrong author", "wrong destination", "conflicting body", "genuine absence"} {
		t.Run(name, func(t *testing.T) {
			f, forge, reporter, trigger, delivery := failedRunReport(t, func(forge *fakeForge) { forge.failWith(http.StatusInternalServerError) })
			forge.mu.Lock()
			path := "/repos/" + f.ref().Owner + "/" + f.ref().Repo + "/issues/" + strconv.Itoa(f.ref().Issue) + "/comments"
			planted := forgePost{path: path, body: delivery.Body, id: 4000}
			switch name {
			case "wrong author":
				planted.authorID = 123
			case "wrong destination":
				planted.htmlURL = "https://github.com/other/repo/issues/1#issuecomment-4000"
			case "conflicting body":
				planted.body = "changed content\n" + delivery.Body
			}
			if name != "genuine absence" {
				forge.posts = append(forge.posts, planted)
			}
			forge.mu.Unlock()
			_, _, err := reporter.ReportPending(t.Context(), f.session.ID, trigger)
			if name == "genuine absence" {
				if err != nil || forge.attemptCount() != 2 || len(forge.all()) != 1 {
					t.Fatalf("absence did not repost once: err=%v attempts=%d", err, forge.attemptCount())
				}
				return
			}
			if !errors.Is(err, ErrReportReceiptUncertain) || errors.Is(err, ErrReportReceiptAbsent) {
				t.Fatalf("untrusted marker comment classified as %v", err)
			}
			saved, readErr := f.db.GetReportDelivery(t.Context(), delivery.Key)
			if readErr != nil || !saved.PossiblySent || saved.Completed || forge.attemptCount() != 1 || watermark(t, f) != 0 {
				t.Fatalf("untrusted comment authorized a repost: %+v err=%v attempts=%d", saved, readErr, forge.attemptCount())
			}
		})
	}
}
