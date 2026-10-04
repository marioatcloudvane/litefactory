package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type deliveryCommentReader struct {
	Poster
	comments    []github.Comment
	err         error
	destination naming.Ref
}

func (r deliveryCommentReader) ListCommentsAfter(_ context.Context, repo github.Repo, issue int, after int64) ([]github.Comment, error) {
	if repo.Owner != r.destination.Owner || repo.Name != r.destination.Repo || issue != r.destination.Issue || after != 0 {
		return nil, errors.New("lookup did not cover the complete actual destination")
	}
	return r.comments, r.err
}

func TestReportDeliveryIdentitySeparatesLogicalMessages(t *testing.T) {
	root := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 42}
	child := root
	child.Issue = 43
	cases := []struct {
		name              string
		task, destination naming.Ref
		trigger, run      int64
		purpose           string
	}{
		{"owner runless", root, root, 7, 0, "run-report"},
		{"different trigger", root, root, 8, 0, "run-report"},
		{"different purpose", root, root, 7, 0, "reply:refused"},
		{"allocated run", root, root, 7, 1, "run-report"},
		{"different run", root, root, 7, 2, "run-report"},
		{"redirected child", root, child, 7, 0, "run-report"},
		{"child execution", child, child, 7, 0, "run-report"},
		{"issue body", root, root, 0, 0, "run-report"},
		{"schedule", root, root, (store.Schedule{ID: 7}).TriggerID(), 0, "run-report"},
		{"automatic", root, root, (store.Continuation{ID: 7}).TriggerID(), 0, "run-report"},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reporter{botUserID: 99}
			first := r.deliveryIntent(tc.task, tc.destination, 1, tc.trigger, tc.run, tc.purpose)
			second := r.deliveryIntent(tc.task, tc.destination, 1, tc.trigger, tc.run, tc.purpose)
			if first.Key != second.Key || first.Marker != second.Marker {
				t.Fatal("same logical delivery changed identity")
			}
			if previous, exists := seen[first.Key]; exists {
				t.Fatalf("identity collided with %s", previous)
			}
			seen[first.Key] = tc.name
			if first.Version != 1 || first.Task != tc.task.String() || first.Destination != tc.destination.String() || first.TriggerID != tc.trigger || first.RunID != tc.run || first.Purpose != tc.purpose || first.BotUserID != 99 {
				t.Fatalf("lost identity dimension: %+v", first)
			}
		})
	}
}

func TestReportDeliveryRecoveryRequiresTrustedExactEvidence(t *testing.T) {
	destination := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 42}
	intent := (&Reporter{botUserID: 99}).deliveryIntent(destination, destination, 1, 7, 9, "run-report")
	intent.Body = "retained redacted result\n" + intent.Marker
	exact := func(id int64) github.Comment {
		return github.Comment{ID: id, Body: intent.Body, Author: github.User{ID: 99}, HTMLURL: fmt.Sprintf("https://github.com/acme/api/issues/42#issuecomment-%d", id)}
	}
	changed := func(change func(*github.Comment)) github.Comment { c := exact(11); change(&c); return c }
	cases := []struct {
		name     string
		comments []github.Comment
		err      error
		want     int64
	}{
		{name: "equivalent duplicates choose lowest numeric", comments: []github.Comment{exact(90), exact(12), exact(43)}, want: 12},
		{name: "single trusted match", comments: []github.Comment{exact(11)}, want: 11},
		{name: "wrong numeric author", comments: []github.Comment{changed(func(c *github.Comment) { c.Author.ID = 100 })}},
		{name: "wrong destination", comments: []github.Comment{changed(func(c *github.Comment) { c.HTMLURL = "https://github.com/acme/api/issues/43#issuecomment-11" })}},
		{name: "wrong repository", comments: []github.Comment{changed(func(c *github.Comment) { c.HTMLURL = "https://github.com/acme/other/issues/42#issuecomment-11" })}},
		{name: "wrong host", comments: []github.Comment{changed(func(c *github.Comment) { c.HTMLURL = "https://evil.example/acme/api/issues/42#issuecomment-11" })}},
		{name: "missing destination", comments: []github.Comment{changed(func(c *github.Comment) { c.HTMLURL = "" })}},
		{name: "mismatched receipt URL", comments: []github.Comment{changed(func(c *github.Comment) { c.HTMLURL = "https://github.com/acme/api/issues/42#issuecomment-12" })}},
		{name: "body drift", comments: []github.Comment{changed(func(c *github.Comment) { c.Body = "changed\n" + intent.Marker })}},
		{name: "conflict alongside exact match", comments: []github.Comment{exact(10), changed(func(c *github.Comment) { c.Body = "changed\n" + intent.Marker })}},
		{name: "unrelated marker", comments: []github.Comment{changed(func(c *github.Comment) { c.Body = "unrelated result" })}},
		{name: "empty successful lookup"},
		{name: "failed partial lookup", comments: []github.Comment{exact(11)}, err: errors.New("pagination interrupted")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reporter{forge: deliveryCommentReader{comments: tc.comments, err: tc.err, destination: destination}}
			id, err := r.recoverDelivery(t.Context(), intent, destination)
			if id != tc.want || (err != nil) != (tc.want == 0) {
				t.Fatalf("receipt=%d err=%v want=%d", id, err, tc.want)
			}
		})
	}
}

func TestReportDeliveryLegacyRunlessAmbiguity(t *testing.T) {
	destination := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 42}
	intent := (&Reporter{botUserID: 99}).deliveryIntent(destination, destination, 1, 7, 0, "run-report")
	for _, tc := range []struct {
		name, body string
		author     int64
		want       bool
	}{
		{"legacy reply", report.ReplyMarker(destination) + "\nold reply", 99, true},
		{"legacy runless status", report.StatusMarker(destination, 1) + "\nold refusal", 99, true},
		{"modern delivery", report.ReplyMarker(destination) + "\nreply\n" + intent.Marker, 99, false},
		{"owner copied marker", report.ReplyMarker(destination) + "\nold reply", 123, false},
		{"unrelated content", "a conversation", 99, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comment := github.Comment{ID: 11, Author: github.User{ID: tc.author}, Body: tc.body, HTMLURL: "https://github.com/acme/api/issues/42#issuecomment-11"}
			r := &Reporter{forge: deliveryCommentReader{comments: []github.Comment{comment}, destination: destination}}
			got, err := r.legacyRunlessConflict(t.Context(), intent, destination)
			if err != nil || got != tc.want {
				t.Fatalf("legacy ambiguous=%v err=%v want=%v", got, err, tc.want)
			}
		})
	}
}

func TestGraphDispatchStopNoticeUsesDurableDelivery(t *testing.T) {
	t.Parallel()
	f := newFixture(t, newStubRunner(nil))
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	dispatch := store.GraphDispatch{ID: 466, SessionID: f.session.ID, Node: "shape", Status: "cancelled", CancellationReason: "startup refused"}
	first, err := reporter.ReportGraphDispatchStop(t.Context(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reporter.ReportGraphDispatchStop(t.Context(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if first.CommentID != second.CommentID || len(forge.all()) != 1 {
		t.Fatalf("duplicate notice: %+v %+v", first, second)
	}
	body := forge.only(t).body
	if !strings.Contains(body, "Work stopped at shape: startup refused") || !strings.Contains(body, "Reply on this open issue to resume") {
		t.Fatalf("notice: %s", body)
	}
	if watermark(t, f) != 0 {
		t.Fatal("private dispatch consumed an owner input")
	}
	dispatch.Status = "pending"
	if _, err = reporter.ReportGraphDispatchStop(t.Context(), dispatch); err == nil {
		t.Fatal("pending dispatch announced a stop")
	}
}

type lostDispatchReceipt struct{ Poster }

func (p lostDispatchReceipt) PostComment(ctx context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error) {
	if _, err := p.Poster.PostComment(ctx, repo, issue, body); err != nil {
		return github.Comment{}, err
	}
	return github.Comment{}, errors.New("receipt lost after post")
}

func TestGraphDispatchStopNoticeRecoversLostReceipt(t *testing.T) {
	t.Parallel()
	f := newFixture(t, newStubRunner(nil))
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	original := reporter.forge
	reporter.forge = lostDispatchReceipt{original}
	dispatch := store.GraphDispatch{ID: 467, SessionID: f.session.ID, Node: "shape", Status: "cancelled", CancellationReason: "startup refused"}
	if _, err := reporter.ReportGraphDispatchStop(t.Context(), dispatch); err == nil {
		t.Fatal("lost receipt claimed success")
	}
	reporter.forge = original
	_, pending, err := reporter.ReportPending(t.Context(), f.session.ID, dispatch.TriggerID())
	if err != nil || !pending {
		t.Fatalf("recover: pending=%v err=%v", pending, err)
	}
	if len(forge.all()) != 1 || forge.attemptCount() != 1 {
		t.Fatal("recovery duplicated the stop notice")
	}
}
