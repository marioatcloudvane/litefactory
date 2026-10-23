package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type analysisPost struct{ channel, key, text string }

type analysisFixture struct {
	adapter *ConversationStandards
	repo    store.Repo
	starts  chan loganalysis.Request
	finish  chan error
	posts   chan analysisPost
	wg      *sync.WaitGroup
}

func newAnalysisFixture(t *testing.T) *analysisFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	f := &analysisFixture{repo: repo, starts: make(chan loganalysis.Request, 4), finish: make(chan error), posts: make(chan analysisPost, 4), wg: &sync.WaitGroup{}}
	analyses := &ConversationAnalyses{DB: db,
		Start: func(_ context.Context, q loganalysis.Request, _ bool) (store.LogAnalysis, error) {
			f.starts <- q
			if err := <-f.finish; err != nil {
				return store.LogAnalysis{}, err
			}
			return store.LogAnalysis{ID: "analysis-1", State: "succeeded"}, nil
		},
		Report: func(_ context.Context, id string) (string, error) { return "report of " + id, nil },
	}
	analyses.serve(ctx, f.wg, func(_ context.Context, channel, key, text string) error {
		f.posts <- analysisPost{channel, key, text}
		return nil
	})
	t.Cleanup(f.wg.Wait)
	t.Cleanup(func() { close(f.finish) })
	f.adapter = &ConversationStandards{DB: db, Analyses: analyses, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	return f
}

func (f *analysisFixture) request(message string) messaging.StandardsRequest {
	now := time.Now().UTC().Truncate(time.Second)
	return messaging.StandardsRequest{Operation: "analysis.start", Arguments: map[string]string{"repository_id": strconv.FormatInt(f.repo.ID, 10), "project": f.repo.Slug(), "after": now.Add(-48 * time.Hour).Format(time.RFC3339), "before": now.Format(time.RFC3339)}, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: message, OperationKey: "discord:3:nl-exec:" + message, At: now}
}

func TestConfirmedAnalysisRunsInTheBackgroundOncePerProject(t *testing.T) {
	f := newAnalysisFixture(t)
	first := f.request("first")
	if _, err := f.adapter.Execute(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	q := <-f.starts
	if len(q.RepositoryIDs) != 1 || q.RepositoryIDs[0] != f.repo.ID || q.StartKey != first.OperationKey || q.After.Location() != time.UTC || q.Before.Sub(q.After) != 48*time.Hour {
		t.Fatalf("analysis scope not resolved from the confirmed action: %+v", q)
	}
	// The confirmed action returned while the analysis is still running.
	if _, err := f.adapter.Execute(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	second, err := f.adapter.Execute(t.Context(), f.request("second"))
	if err != nil || second.Text == "" {
		t.Fatalf("second start not refused plainly: %+v %v", second, err)
	}
	select {
	case q = <-f.starts:
		t.Fatalf("a second analysis of the same project started: %+v", q)
	default:
	}
	f.finish <- nil
	post := <-f.posts
	if post.channel != "3" || post.key != "log-analysis:analysis-1" || post.text != "report of analysis-1" {
		t.Fatalf("result not posted to the conversation: %+v", post)
	}
	f.wg.Wait()
	select {
	case extra := <-f.posts:
		t.Fatalf("result posted twice: %+v", extra)
	default:
	}
	if _, err = f.adapter.Execute(t.Context(), f.request("third")); err != nil {
		t.Fatal(err)
	}
	<-f.starts
	f.finish <- nil
	<-f.posts
}

func TestFailedAnalysisPostsItsReason(t *testing.T) {
	f := newAnalysisFixture(t)
	if _, err := f.adapter.Execute(t.Context(), f.request("first")); err != nil {
		t.Fatal(err)
	}
	<-f.starts
	f.finish <- errors.New("provider unavailable")
	post := <-f.posts
	if post.channel != "3" || !strings.Contains(post.text, "provider unavailable") {
		t.Fatalf("failure reason not posted: %+v", post)
	}
}

func TestAnalysisStartRefusesAnInvalidWindowBeforeStarting(t *testing.T) {
	f := newAnalysisFixture(t)
	r := f.request("first")
	r.Arguments["before"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err := f.adapter.Execute(t.Context(), r); err == nil {
		t.Fatal("a window ending in the future was accepted")
	}
	select {
	case q := <-f.starts:
		t.Fatalf("invalid request started: %+v", q)
	default:
	}
}

func TestConversationAnalysisReportShowsSummaryProposalsAndFindings(t *testing.T) {
	report := loganalysis.Report{
		Summary:   "Tests fail on a missing fixture.",
		Proposals: []loganalysis.Proposal{{Operation: standards.Operation{Kind: "add", Standard: &standards.Standard{ID: "std-1", CategoryID: "testing", Rule: "MUST keep fixtures in testdata."}}, Justification: loganalysis.Justification{WhyItPrevents: "fixtures stay with the tests"}}},
		Findings: []loganalysis.Finding{
			{Key: "f1", Kind: loganalysis.FindingProjectSetup, Target: "make test", Title: "tests need a database", File: true},
			{Key: "f2", Kind: loganalysis.FindingFactoryDefect, Target: "runner", Title: "retries lose the session"},
		},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	view := loganalysis.Inspection{Invocation: store.LogAnalysis{ID: "analysis-9", State: "succeeded"}, Report: raw, Proposals: []store.LogAnalysisProposalLink{{CandidateKey: "proposal-1", ProposalID: "prop-1", Version: 1}}, Filings: []store.LogAnalysisFiling{{FindingKey: "f1", State: store.FilingFiled, Reference: "acme/app#12", URL: "https://example.test/12"}}}
	text := renderConversationAnalysis(view, []standards.Category{{ID: "testing", Name: "Testing"}})
	for _, want := range []string{"analysis-9", report.Summary, "MUST keep fixtures in testdata.", "prop-1", "tests need a database", "acme/app#12", "retries lose the session"} {
		if !strings.Contains(text, want) {
			t.Errorf("report misses %q", want)
		}
	}
}
