package turn

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/testutil"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type acknowledgementForge struct {
	progressForge
	entered      chan struct{}
	release      chan struct{}
	loseResponse bool
}

func (f *acknowledgementForge) PostComment(ctx context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error) {
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return github.Comment{}, ctx.Err()
		}
		f.entered = nil
	}
	comment, err := f.progressForge.PostComment(ctx, repo, issue, body)
	if f.loseResponse {
		f.loseResponse = false
		return github.Comment{}, errors.New("ack response lost")
	}
	return comment, err
}

func TestStoryAcknowledgementRecoversAcrossIndependentReporters(t *testing.T) {
	f := newFixture(t, nil)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	const source = 401
	mustProgress(t, f.db.AdmitStoryRefinement(t.Context(), store.StoryRefinement{SessionID: f.session.ID, CommentID: source, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}))
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	forge := &acknowledgementForge{progressForge: progressForge{comments: map[int][]github.Comment{}}, entered: make(chan struct{}), release: make(chan struct{}), loseResponse: true}
	makeReporter := func(db *store.DB) *Reporter {
		t.Helper()
		r, err := NewReporter(ReporterOptions{DB: db, Forge: forge, Renderer: renderer, Logger: discardLogger(), BotUserID: 9, StoryLockDir: f.paths.StoryProgressLocksDir()})
		mustProgress(t, err)
		return r
	}
	a := makeReporter(f.db)
	other, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	defer other.Close()
	b := makeReporter(other)
	done := make(chan error, 1)
	go func() { done <- a.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source) }()
	select {
	case <-forge.entered:
	case <-time.After(testutil.Wait):
		t.Fatal("acknowledgement did not reach paused post")
	}
	if err := b.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); !errors.Is(err, store.ErrStoryProgressBusy) {
		t.Fatalf("concurrent acknowledgement escaped ownership: %v", err)
	}
	close(forge.release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("response loss not surfaced")
		}
	case <-time.After(testutil.Wait):
		t.Fatal("acknowledgement did not finish")
	}
	mustProgress(t, b.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source))
	mustProgress(t, a.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source))
	if len(forge.comments[200]) != 1 {
		t.Fatal("lost response duplicated the acknowledgement")
	}
	ack, err := f.db.StoryRefinementAcknowledgement(t.Context(), f.session.ID, source)
	mustProgress(t, err)
	pending, err := f.db.ListPendingStoryRefinements(t.Context(), f.session.ID)
	mustProgress(t, err)
	if ack != forge.comments[200][0].ID || len(pending) != 1 {
		t.Fatal("recovery lost acknowledgement identity or consumed the discussion")
	}
}
