package turn

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestStageDecisionSummaryLostReplyRequiresTrustedReceipt(t *testing.T) {
	for _, counterfeit := range []string{"foreign author", "changed body", "wrong destination"} {
		t.Run(counterfeit, func(t *testing.T) {
			f := newFixture(t, newStubRunner(nil))
			forge := newFakeForge(t)
			publisher := DiscussionPublisher{Reporter: newReporter(t, f, forge, nil)}
			before, err := f.db.GetSession(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			c := store.DiscussionConclusion{ID: "decision-confirmed-revision", DiscussionScope: store.DiscussionScope{TaskRef: f.session.TaskRef}, Rendered: "Question: which boundary?\nConclusion: the owner requested revision.\n/approve-plan story-001 technical candidate\n/implement start work"}
			forge.failCreatingWith(http.StatusInternalServerError, true)
			if _, err = publisher.Publish(t.Context(), c); err == nil {
				t.Fatal("lost POST reply reported delivery")
			}
			journal, err := f.db.GetReportDelivery(t.Context(), "discussion-summary:"+c.ID)
			if err != nil || !journal.PossiblySent || journal.Completed || journal.Purpose != "decision-summary" || journal.TriggerNamespace != "discussion-summary" {
				t.Fatalf("decision lost safe delivery origin: %+v %v", journal, err)
			}
			original := forge.only(t)
			fake := original
			fake.hidden = false
			fake.id++
			switch counterfeit {
			case "foreign author":
				fake.authorID = 123
			case "changed body":
				fake.body += "\nUnreviewed addition."
			case "wrong destination":
				fake.htmlURL = "https://github.com/another/repository/issues/99#issuecomment-5002"
			}
			forge.mu.Lock()
			forge.posts = append(forge.posts, fake)
			forge.mu.Unlock()
			forge.recover()
			if url, found, err := publisher.Recover(t.Context(), c); err == nil || found || url != "" {
				t.Fatalf("untrusted receipt accepted: %q %v %v", url, found, err)
			}
			if _, err = publisher.Publish(t.Context(), c); err == nil {
				t.Fatal("untrusted remote trace permitted successful blind retry")
			}
			if forge.attemptCount() != 1 {
				t.Fatalf("uncertain decision summary reposted: %d", forge.attemptCount())
			}
			// Make only the original, exact factory-authored receipt visible.
			forge.mu.Lock()
			forge.posts[1].hidden = true
			forge.posts[0].hidden = false
			forge.mu.Unlock()
			url, found, err := publisher.Recover(t.Context(), c)
			if err != nil || !found || url != forgePostURL(original) {
				t.Fatalf("exact receipt recovery: %q %v %v", url, found, err)
			}
			if _, err = publisher.Publish(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			if forge.attemptCount() != 1 {
				t.Fatal("recovered decision summary was published twice")
			}
			delivered, err := f.db.GetReportDelivery(t.Context(), journal.Key)
			if err != nil || !delivered.Completed || delivered.ConfirmedURL != url {
				t.Fatalf("receipt not durable: %+v %v", delivered, err)
			}
			if got := ParseCommand(original.body); got.Kind != KindNone {
				t.Fatalf("factory decision summary admitted as command: %+v", got)
			}
			if _, err = f.db.RetainOwnerInstruction(t.Context(), f.session.ID, 123, 42, original.body, store.OwnerInputTarget{}); err == nil {
				t.Fatal("factory decision summary admitted as owner instruction")
			}
			if pending, found, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 0); err != nil || found {
				t.Fatalf("summary entered workflow recovery: %+v %v", pending, err)
			}
			after, err := f.db.GetSession(t.Context(), f.session.ID)
			if err != nil || before != after {
				t.Fatalf("summary delivery mutated workflow: %+v %v", after, err)
			}
		})
	}
}

func TestStageDecisionSummaryRefusesChangedFirstRender(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	forge := newFakeForge(t)
	hostRoot := t.TempDir()
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit, Logger: f.opts.Logger, HostRoots: []string{hostRoot}})
	if err != nil {
		t.Fatal(err)
	}
	publisher := DiscussionPublisher{Reporter: newReporter(t, f, forge, renderer)}
	reviewed := "Question: which evidence?\nConclusion: inspect " + hostRoot + "/private-evidence.txt before revising."
	// Establish that the real outbound renderer changes this reviewed input.
	protected, err := renderer.RenderReportDelivery(t.Context(), reviewed, "")
	if err != nil || protected.String() == reviewed {
		t.Fatalf("fixture does not change reviewed bytes: %q %v", protected.String(), err)
	}
	conclusion := store.DiscussionConclusion{ID: "decision-first-render", DiscussionScope: store.DiscussionScope{TaskRef: f.session.TaskRef}, Rendered: reviewed}
	url, err := publisher.Publish(t.Context(), conclusion)
	var refused *stagedecision.PublicationRejected
	if !errors.As(err, &refused) || url != "" {
		t.Fatalf("changed reviewed text was not definitively refused: %q %v", url, err)
	}
	if forge.attemptCount() != 0 {
		t.Fatalf("changed reviewed text reached POST: %d", forge.attemptCount())
	}
	if journal, e := f.db.GetReportDelivery(t.Context(), "discussion-summary:"+conclusion.ID); e == nil && journal.PossiblySent {
		t.Fatalf("refused render recorded possible send: %+v", journal)
	}
}
