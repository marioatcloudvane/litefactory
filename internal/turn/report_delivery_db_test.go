package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestReportDeliveryRefusalSurvivesReceiptLossAndReadinessChange(t *testing.T) {
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("original refused command launched an agent")
		return nil, errors.New("unexpected launch")
	})
	f := newFixture(t, runner)
	if err := f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	request := f.request()
	request.Mode = "implement"
	request.Command = ParseCommand("/implement work")
	refused := f.execute(request)
	if !strings.Contains(refused.Reason, "No agent launched") {
		t.Fatalf("not a readiness refusal: %+v", refused)
	}
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	forge.afterPost = func() {
		if err := f.db.Close(); err != nil {
			t.Error(err)
		}
	}
	posted, err := reporter.ReportTurn(t.Context(), refused)
	if err == nil || posted.CommentID == 0 {
		t.Fatalf("receipt failure not exposed: %+v %v", posted, err)
	}
	original := forge.only(t).body
	// ReportTurn has joined the HTTP handler and no worker owns the store.
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	*f.db = *reopened
	forge.afterPost = nil
	if watermark(t, f) != 0 {
		t.Fatal("failed receipt consumed refusal")
	}
	if err := f.db.SetManagedFeatureReady(t.Context(), f.session.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	retry := f.execute(request)
	if runner.callCount() != 0 || len(f.runs()) != 0 {
		t.Fatal("changed readiness launched refused source")
	}
	// A renderer that always fails proves pending recovery does not rerender
	// mutable task state or regenerate the now-obsolete refusal explanation.
	reconstructed := newReporter(t, f, forge, stubRenderer{err: errors.New("must not rerender retained content")})
	if _, err := reconstructed.ReportTurn(t.Context(), retry); err != nil {
		t.Fatal(err)
	}
	if forge.attemptCount() != 1 || forge.only(t).body != original || watermark(t, f) != request.Trigger.CommentID {
		t.Fatal("recovery changed/reposted content or failed to consume original source")
	}
	if _, pending, err := reconstructed.ReportPending(t.Context(), f.session.ID, request.Trigger.CommentID); err != nil || pending {
		t.Fatalf("completed refusal remains pending: %v %v", pending, err)
	}
	if runner.callCount() != 0 {
		t.Fatal("refusal recovery launched")
	}
}

func TestReportDeliveryHTTPRecoveryRejectsUntrustedReceipts(t *testing.T) {
	for _, name := range []string{"wrong author", "wrong destination", "conflicting body", "equivalent duplicates"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				return nil, errors.New("not executed")
			}))
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			request := ReplyRequest{Ref: f.ref(), SessionID: f.session.ID, CommentID: 22, Command: ParseCommand("/unknown command")}
			forge.afterPost = func() {
				if err := f.db.Close(); err != nil {
					t.Error(err)
				}
			}
			posted, err := reporter.ReportReply(t.Context(), request)
			if err == nil || posted.CommentID == 0 {
				t.Fatalf("expected accepted post with missing receipt: %+v %v", posted, err)
			}
			reopened, err := store.Open(t.Context(), f.paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			*f.db = *reopened
			forge.mu.Lock()
			forge.afterPost = nil
			switch name {
			case "wrong author":
				forge.posts[0].authorID = 123
			case "wrong destination":
				forge.posts[0].htmlURL = "https://github.com/other/repo/issues/1#issuecomment-5001"
			case "conflicting body":
				forge.posts[0].body = "changed content\n" + forge.posts[0].body
			case "equivalent duplicates":
				duplicate := forge.posts[0]
				duplicate.id += 10
				forge.posts = append([]forgePost{duplicate}, forge.posts...)
			}
			forge.mu.Unlock()
			recovered, err := newReporter(t, f, forge, stubRenderer{err: errors.New("must retain original bytes")}).ReportReply(t.Context(), request)
			if name == "equivalent duplicates" {
				if err != nil || recovered.CommentID != posted.CommentID || watermark(t, f) != 22 {
					t.Fatalf("equivalent receipt not selected: %+v %v", recovered, err)
				}
			} else if err == nil || watermark(t, f) != 0 {
				t.Fatalf("untrusted receipt consumed request: %+v %v", recovered, err)
			}
			if forge.attemptCount() != 1 {
				t.Fatal("recovery blindly reposted")
			}
		})
	}
}

func TestReportDeliveryRedirectedReplyPreservesParentWatermark(t *testing.T) {
	f := newFixture(t, nil)
	const source = 401
	if err := f.db.AdmitStoryRefinement(t.Context(), store.StoryRefinement{SessionID: f.session.ID, CommentID: source, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge(t)
	request := ReplyRequest{Ref: f.ref(), SessionID: f.session.ID, CommentID: source, Command: ParseCommand("/unknown command")}
	if _, err := newReporter(t, f, forge, nil).ReportReply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(forge.only(t).path, "/issues/200/comments") {
		t.Fatalf("reply destination=%s", forge.only(t).path)
	}
	sourceState, err := f.db.GetStoryRefinement(t.Context(), f.session.ID, source)
	if err != nil || !sourceState.Consumed || watermark(t, f) != 0 {
		t.Fatalf("redirected source accounting: %+v %v watermark=%d", sourceState, err, watermark(t, f))
	}
	if _, err := newReporter(t, f, forge, stubRenderer{err: errors.New("already consumed")}).ReportReply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if forge.attemptCount() != 1 {
		t.Fatal("redirected reply repeated")
	}
}
