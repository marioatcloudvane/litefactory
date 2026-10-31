package loganalysis

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type workflowReceiptTracker struct {
	creates, reads int
	hit            IssueHit
	found          bool
	createError    error
}

func (t *workflowReceiptTracker) Name() string { return "github:fixture/service" }
func (t *workflowReceiptTracker) Search(context.Context, string, int) ([]IssueHit, error) {
	return nil, nil
}
func (t *workflowReceiptTracker) FileBug(context.Context, redact.Body, redact.Body, string, time.Time) (IssueHit, error) {
	t.creates++
	return t.hit, t.createError
}
func (t *workflowReceiptTracker) FindFiled(context.Context, string, time.Time) (IssueHit, bool, error) {
	t.reads++
	return t.hit, t.found, nil
}

func TestWorkflowFilingReconcilesPositiveEvidenceWithoutRecreating(t *testing.T) {
	for _, labels := range []bool{false, true} {
		t.Run(fmt.Sprint(labels), func(t *testing.T) {
			f := newOutcomeFixture(t, true, false)
			incident := f.incident(t)
			finding := findingFor("setup", FindingProjectSetup, incident)
			finding.File = true
			f.accept(t, finding)
			tracker := &workflowReceiptTracker{createError: errors.New("response lost after create")}
			s := f.h.service
			s.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: tracker}
			ctx := WithWorkflowDeadline(t.Context())
			if err := s.FileFindings(ctx, f.h.a.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.FileFindings(ctx, f.h.a.ID); err != nil {
				t.Fatal(err)
			}
			if tracker.creates != 1 {
				t.Fatalf("repeated ambiguous create %d", tracker.creates)
			}
			if err := s.ReconcileWorkflowFilings(ctx, f.h.a.ID); err != nil {
				t.Fatal(err)
			}
			filings, err := s.DB.ListLogAnalysisFilings(ctx, f.h.a.ID)
			if err != nil || len(filings) != 1 || filings[0].State != store.FilingFailed {
				t.Fatalf("absence incorrectly settled: %+v %v", filings, err)
			}
			tracker.found = true
			tracker.hit = IssueHit{Reference: "fixture/service#77", URL: "https://example.test/issues/77"}
			if labels {
				tracker.hit.Labels = []string{"bug"}
			}
			if err = s.ReconcileWorkflowFilings(ctx, f.h.a.ID); err != nil {
				t.Fatal(err)
			}
			filings, err = s.DB.ListLogAnalysisFilings(ctx, f.h.a.ID)
			if err != nil || filings[0].Reference != tracker.hit.Reference || filings[0].URL != tracker.hit.URL {
				t.Fatalf("positive link lost: %+v %v", filings, err)
			}
			want := store.FilingFailed
			if labels {
				want = store.FilingFiled
			}
			if filings[0].State != want {
				t.Fatalf("label uncertainty lost: %+v", filings[0])
			}
			if err = s.FileFindings(ctx, f.h.a.ID); err != nil {
				t.Fatal(err)
			}
			if tracker.creates != 1 {
				t.Fatalf("recovery created another issue: %d", tracker.creates)
			}
		})
	}
}
func TestWorkflowFilingRejectsEffectsAfterAuthorityRevocation(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	incident := f.incident(t)
	finding := findingFor("setup", FindingProjectSetup, incident)
	finding.File = true
	f.accept(t, finding)
	tracker := &workflowReceiptTracker{}
	s := f.h.service
	s.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: tracker}
	ctx := WithWorkflowDeadline(store.WithOwnerActionEffect(t.Context(), "owner-action:missing", time.Now))
	if err := s.FileFindings(ctx, f.h.a.ID); err == nil {
		t.Fatal("unclaimed workflow effect accepted")
	}
	if tracker.creates != 0 {
		t.Fatal("tracker called without authority")
	}
}
func TestWorkflowFilingNeverRepeatsStartedClaim(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	incident := f.incident(t)
	finding := findingFor("setup", FindingProjectSetup, incident)
	finding.File = true
	f.accept(t, finding)
	tracker := &workflowReceiptTracker{}
	s := f.h.service
	s.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: tracker}
	if _, err := s.DB.SaveLogAnalysisFiling(t.Context(), store.LogAnalysisFiling{AnalysisID: f.h.a.ID, FindingKey: finding.Key, Destination: tracker.Name(), State: store.FilingStarted, Title: "title", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FileFindings(WithWorkflowDeadline(t.Context()), f.h.a.ID); err != nil {
		t.Fatal(err)
	}
	if tracker.creates != 0 {
		t.Fatal("started claim repeated")
	}
}
func TestGitHubWorkflowReceiptLookupUsesOnlyReadsAndExactMarker(t *testing.T) {
	marker := Marker("analysis", "finding")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("recovery mutation: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"number":7,"title":"near match","body":%q,"labels":[{"name":"bug"}]},{"number":8,"title":"exact","body":%q,"labels":[{"name":"bug"}],"html_url":"https://example.test/8"}]`, marker+" extra", marker)
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("test-token"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	tracker := GitHubTracker{Client: client, Repo: github.Repo{Owner: "fixture", Name: "service"}}
	hit, found, err := tracker.FindFiled(t.Context(), marker, time.Now())
	if err != nil || !found || hit.Reference != "fixture/service#8" || requests != 1 {
		t.Fatalf("receipt: %+v %v %v requests=%d", hit, found, err, requests)
	}
}

func TestWorkflowFilingLegacyEntryPointRespectsPersistedAmbiguousClaim(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	incident := f.incident(t)
	finding := findingFor("setup", FindingProjectSetup, incident)
	finding.File = true
	f.accept(t, finding)
	s := f.h.service
	db := s.DB
	ctx := t.Context()
	at := time.Now().UTC()
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: "github", Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	p := store.OwnerAction{ID: "workflow-filing", PolicyVersion: 1, Principal: store.OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "channel", DiscordOwnerID: "owner"}, RequestKey: "workflow-filing", RequestDigest: "digest", Facts: `{}`, Payload: `{}`, Digest: "digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:workflow-filing"}
	if err = db.CreateOwnerAction(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReserveWorkflowRequest(ctx, store.WorkflowRequest{OwnerID: "42", OwnerGeneration: owner.Generation, Key: p.RequestKey, Digest: "digest", ProposalID: p.ID, JobID: "workflow-filing-job", Snapshot: `{}`, Inputs: `{}`, Kind: "start", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MutateOwnerAction(ctx, p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	original, err := db.GetLogAnalysis(ctx, f.h.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := db.CreateLogAnalysis(ctx, store.LogAnalysis{ID: "workflow-analysis", StartKey: p.ExecutionID, RequestDigest: "digest", Scope: original.Scope, Limits: original.Limits, GrantDigest: original.GrantDigest})
	if err != nil {
		t.Fatal(err)
	}
	report, err := db.GetLogAnalysisReport(ctx, f.h.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinalizeLogAnalysis(ctx, store.LogAnalysisFinalization{AnalysisID: analysis.ID, Generation: analysis.Generation, RequestID: "saved", RequestDigest: "digest", Report: report, State: "completed", Disposition: "no_warranted_change"}); err != nil {
		t.Fatal(err)
	}
	tracker := &workflowReceiptTracker{}
	s.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: tracker}
	if _, err = db.SaveLogAnalysisFiling(ctx, store.LogAnalysisFiling{AnalysisID: analysis.ID, FindingKey: finding.Key, Destination: tracker.Name(), State: store.FilingFailed, Title: "title", Body: "body", Reason: "response lost"}); err != nil {
		t.Fatal(err)
	}
	if err = s.FileFindings(ctx, analysis.ID); err != nil {
		t.Fatal(err)
	}
	if tracker.creates != 0 {
		t.Fatal("legacy explicit filing entry point repeated durable workflow ambiguous create")
	}
}

func TestWorkflowReceiptCannotSwitchSavedDestination(t *testing.T) {
	f := newOutcomeFixture(t, true, false, upstream("github:old/destination"))
	incident := f.incident(t)
	finding := findingFor("factory", FindingFactoryDefect, incident)
	finding.File = true
	f.accept(t, finding)
	tracker := &workflowReceiptTracker{found: true, hit: IssueHit{Reference: "fixture/service#77", Labels: []string{"bug"}}}
	s := f.h.service
	s.Upstream = tracker
	if _, e := s.DB.SaveLogAnalysisFiling(t.Context(), store.LogAnalysisFiling{AnalysisID: f.h.a.ID, FindingKey: finding.Key, Destination: "github:old/destination", State: store.FilingStarted, Title: "title", Body: "body"}); e != nil {
		t.Fatal(e)
	}
	if e := s.ReconcileWorkflowFilings(t.Context(), f.h.a.ID); e != nil {
		t.Fatal(e)
	}
	if tracker.reads != 0 || tracker.creates != 0 {
		t.Fatal("recovery used a different configured destination")
	}
	filings, e := s.DB.ListLogAnalysisFilings(t.Context(), f.h.a.ID)
	if e != nil || filings[0].State != store.FilingStarted {
		t.Fatalf("changed destination settled receipt: %+v %v", filings, e)
	}
}
