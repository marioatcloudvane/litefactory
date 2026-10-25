package loganalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// fakeTracker is an in-memory issue tracker; it never touches a network.
type fakeTracker struct {
	name    string
	refuse  string
	created []string
	bodies  []string
}

func (f *fakeTracker) Name() string { return f.name }
func (f *fakeTracker) Search(_ context.Context, text string, _ int) ([]IssueHit, error) {
	return []IssueHit{{Reference: f.name + "#7", Title: "Earlier report: " + text, State: "closed", Labels: []string{"bug"}}}, nil
}
func (f *fakeTracker) FileBug(_ context.Context, title, body redact.Body, marker string, _ time.Time) (IssueHit, error) {
	if f.refuse != "" {
		return IssueHit{}, NotFiled(f.refuse)
	}
	for i, existing := range f.bodies {
		if hasMarkerLine(existing, marker) {
			return IssueHit{Reference: fmt.Sprintf("%s#%d", f.name, i+1)}, nil
		}
	}
	f.created = append(f.created, title.String())
	f.bodies = append(f.bodies, body.String())
	return IssueHit{Reference: fmt.Sprintf("%s#%d", f.name, len(f.bodies)), URL: "https://example.test/" + title.String()}, nil
}

func (f *outcomeFixture) withTeam(owned bool) {
	f.h.scope.Repositories[0].Team, f.h.scope.Repositories[0].TeamOwned = "go-development@1.0.0", owned
}

func TestFindingValidationOnlyAllowsFilingWhereSomeoneCanFixIt(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	id := f.incident(t)
	finding := func(key, kind string, file bool) Finding {
		v := findingFor(key, kind, id)
		v.File = file
		return v
	}
	for name, findings := range map[string][]Finding{
		"unknown kind":                    {finding("a", "one_off", false)},
		"duplicate key":                   {finding("a", FindingProjectSetup, false), finding("a", FindingFactoryDefect, false)},
		"multi-line title":                {func() Finding { v := finding("a", FindingProjectSetup, false); v.Title = "a\nb"; return v }()},
		"missing cause":                   {func() Finding { v := finding("a", FindingProjectSetup, false); v.Cause = " "; return v }()},
		"filing a covered finding":        {func() Finding { v := finding("a", FindingFactoryDefect, true); v.Existing = "owner/repo#12"; return v }()},
		"filing a user-owned team's role": {finding("a", FindingRolePrompt, true)},
		"filing three":                    {finding("a", FindingProjectSetup, true), finding("b", FindingFactoryDefect, true), finding("c", FindingFactoryDefect, true)},
		"unknown repository":              {func() Finding { v := finding("a", FindingProjectSetup, false); v.Repository = "other/repo"; return v }()},
	} {
		f.withTeam(true)
		report := reportFor(logevidence.Citation{})
		report.Findings = findings
		if _, err := f.h.validate(t.Context(), report); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	f.withTeam(false)
	report := reportFor(logevidence.Citation{})
	covered := finding("c", FindingFactoryDefect, false)
	covered.Existing = "owner/repo#12"
	report.Findings = []Finding{finding("a", FindingProjectSetup, true), finding("b", FindingRolePrompt, true), covered, finding("d", FindingFactoryDefect, false)}
	if _, err := f.h.validate(t.Context(), report); err != nil {
		t.Fatalf("two filed findings, a catalogue role prompt and a covered one refused: %v", err)
	}
	f.withTeam(true)
	report.Findings[1].File = false
	if _, err := f.h.validate(t.Context(), report); err != nil {
		t.Fatalf("an unfiled suggestion for a user-owned team refused: %v", err)
	}
}

// accept submits a report through the analyst's own result tool.
func (f *outcomeFixture) accept(t *testing.T, findings ...Finding) {
	t.Helper()
	report := reportFor(logevidence.Citation{})
	report.Findings = findings
	reply, err := f.h.communication(t.Context(), "submit", "communication_submit", raw(contribution{RequestID: "accepted", Report: report}))
	if err != nil || strings.Contains(string(reply), `"valid":false`) {
		t.Fatalf("report refused: %s %v", reply, err)
	}
}

func TestFilingRoutesEachFindingToWhoCanFixIt(t *testing.T) {
	f := newOutcomeFixture(t, true, false, upstream("github:maintainer/litefactory"), ownedTeam)
	id := f.incident(t)
	project, maintainer := &fakeTracker{name: "github:fixture/service"}, &fakeTracker{name: "github:maintainer/litefactory"}
	s := f.h.service
	s.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: project}
	s.Upstream = maintainer
	setup, defect, suggestion := findingFor("make-test", FindingProjectSetup, id), findingFor("finalizer", FindingFactoryDefect, id), findingFor("story-worker", FindingRolePrompt, id)
	setup.File, defect.File = true, true
	f.accept(t, setup, defect, suggestion)

	if err := s.FileFindings(t.Context(), f.h.a.ID); err != nil {
		t.Fatal(err)
	}
	if len(project.created) != 1 || project.created[0] != setup.Title || len(maintainer.created) != 1 || maintainer.created[0] != defect.Title {
		t.Fatalf("project=%v upstream=%v", project.created, maintainer.created)
	}
	if !strings.Contains(maintainer.bodies[0], Marker(f.h.a.ID, "finalizer")) || !strings.Contains(maintainer.bodies[0], defect.Cause) || !strings.Contains(maintainer.bodies[0], id) {
		t.Fatalf("upstream body lacks marker, cause or incident:\n%s", maintainer.bodies[0])
	}
	// A retry, or a restart after a crash mid-filing, files nothing twice.
	if _, err := s.DB.SaveLogAnalysisFiling(t.Context(), store.LogAnalysisFiling{AnalysisID: f.h.a.ID, FindingKey: "make-test", Destination: "x", State: store.FilingStarted, Title: "t", Body: "b"}); err == nil {
		t.Fatal("a filed record was reopened")
	}
	if err := s.FileFindings(t.Context(), f.h.a.ID); err != nil {
		t.Fatal(err)
	}
	if len(project.created) != 1 || len(maintainer.created) != 1 {
		t.Fatalf("retry filed again: project=%v upstream=%v", project.created, maintainer.created)
	}
	filings, err := s.DB.ListLogAnalysisFilings(t.Context(), f.h.a.ID)
	if err != nil || len(filings) != 2 || filings[0].State != store.FilingFiled || filings[1].State != store.FilingFiled {
		t.Fatalf("filings=%+v %v", filings, err)
	}
	if Route(f.h.scope, suggestion) != "team" {
		t.Fatal("a user-owned team's role prompt is not a suggested team change")
	}
}

func upstream(name string) func(*Scope) { return func(s *Scope) { s.Upstream = name } }
func ownedTeam(s *Scope) {
	s.Repositories[0].Team, s.Repositories[0].TeamOwned = "go-development@1.0.0", true
}
func dryRun(s *Scope) { s.Request.DryRun = true }

func TestUpstreamReportWithoutDestinationAndDryRunFileNothing(t *testing.T) {
	for _, dry := range []bool{false, true} {
		options := []func(*Scope){}
		if dry {
			options = append(options, dryRun)
		}
		f := newOutcomeFixture(t, true, false, options...)
		id := f.incident(t)
		project := &fakeTracker{name: "github:fixture/service"}
		s := f.h.service
		s.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: project}
		s.Upstream = &fakeTracker{name: "github:maintainer/litefactory"}
		setup, defect := findingFor("make-test", FindingProjectSetup, id), findingFor("finalizer", FindingFactoryDefect, id)
		setup.File, defect.File = true, true
		f.accept(t, setup, defect)
		if err := s.FileFindings(t.Context(), f.h.a.ID); err != nil {
			t.Fatal(err)
		}
		filings, err := s.DB.ListLogAnalysisFilings(t.Context(), f.h.a.ID)
		if err != nil || len(filings) != 2 {
			t.Fatalf("filings=%+v %v", filings, err)
		}
		byKey := map[string]store.LogAnalysisFiling{filings[0].FindingKey: filings[0], filings[1].FindingKey: filings[1]}
		upstream := byKey["finalizer"]
		if !strings.Contains(upstream.Reason, "no upstream tracker is configured") || !strings.Contains(upstream.Body, defect.Suggestion) {
			t.Fatalf("upstream report not shown as ready-to-file text: %+v", upstream)
		}
		if dry {
			if len(project.created) != 0 || byKey["make-test"].State != store.FilingWouldFile || upstream.State != store.FilingWouldFile {
				t.Fatalf("dry run filed: %v %+v", project.created, filings)
			}
			continue
		}
		if len(project.created) != 1 || upstream.State != store.FilingNotFiled {
			t.Fatalf("project=%v upstream=%+v", project.created, upstream)
		}
	}
}

func TestDestinationThatCannotFileRecordsWhy(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	id := f.incident(t)
	f.h.service.Projects = map[int64]Tracker{f.h.scope.Repositories[0].ID: &fakeTracker{name: "linear:team/project", refuse: "litefactory cannot yet label a new Linear issue as a Bug"}}
	setup := findingFor("make-test", FindingProjectSetup, id)
	setup.File = true
	f.accept(t, setup)
	if err := f.h.service.FileFindings(t.Context(), f.h.a.ID); err != nil {
		t.Fatal(err)
	}
	filings, err := f.h.service.DB.ListLogAnalysisFilings(t.Context(), f.h.a.ID)
	if err != nil || len(filings) != 1 || filings[0].State != store.FilingNotFiled || !strings.Contains(filings[0].Reason, "Linear") {
		t.Fatalf("filings=%+v %v", filings, err)
	}
}

func TestGitHubTrackerFindsItsMarkerInsteadOfFilingTwice(t *testing.T) {
	var mu sync.Mutex
	issues := []map[string]any{}
	labelled := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/maintainer/litefactory/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/maintainer/litefactory/issues":
			var in map[string]any
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &in)
			n := len(issues) + 1
			issue := map[string]any{"id": 100 + n, "number": n, "title": in["title"], "body": in["body"], "state": "open", "html_url": fmt.Sprintf("https://github.test/maintainer/litefactory/issues/%d", n)}
			issues = append(issues, issue)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(issue)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/labels"):
			labelled++
			_, _ = w.Write([]byte(`[{"name":"bug"}]`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("test-token"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	tracker := GitHubTracker{Client: client, Repo: github.Repo{Owner: "maintainer", Name: "litefactory"}}
	r := redact.New(redact.Options{Environ: []string{}})
	title, _ := r.Apply(t.Context(), "Finalizer ignores the check result", 256)
	body, _ := r.Apply(t.Context(), "Cause.\n\n"+Marker("A1", "finalizer")+"\n", 4096)
	first, err := tracker.FileBug(t.Context(), title, body, Marker("A1", "finalizer"), time.Now())
	if err != nil || first.Reference != "maintainer/litefactory#1" {
		t.Fatalf("first=%+v %v", first, err)
	}
	again, err := tracker.FileBug(t.Context(), title, body, Marker("A1", "finalizer"), time.Now())
	if err != nil || again.Reference != first.Reference || len(issues) != 1 || labelled != 2 {
		t.Fatalf("again=%+v issues=%d labelled=%d %v", again, len(issues), labelled, err)
	}
}

func TestIssueAndRepositoryToolsAreBoundedReadOnlyChecks(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	repo := gittest.New(t)
	repo.WriteFile("Makefile", "test:\n\tgo test ./...\n")
	head := repo.Commit("Make: run the whole suite")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s := f.h.service
	s.Git = binary
	registered, err := s.DB.UpsertRepo(t.Context(), store.Repo{Path: repo.Dir, Forge: store.ForgeGitHub, Owner: "fixture", Name: "checked", RemoteURL: "git@github.com:fixture/checked.git", BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.Projects = map[int64]Tracker{registered.ID: &fakeTracker{name: "github:fixture/checked"}}
	pinned, err := s.pinRepositories(t.Context(), []int64{registered.ID})
	if err != nil || len(pinned) != 1 || pinned[0].Tickets != "github:fixture/checked" {
		t.Fatalf("pinned=%+v %v", pinned, err)
	}
	// The pin is the remote base head; this fixture's commit is local only.
	pinned[0].Commit = head
	f.h.scope.Repositories = pinned
	call := func(name string, args any) map[string]any {
		t.Helper()
		b, err := f.h.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: fmt.Sprintf("%s-%d", name, time.Now().UnixNano()), Payload: raw(map[string]any{"name": name, "arguments": args})})
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err = json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := call("repo_log", map[string]any{"query": "suite"}); !strings.Contains(fmt.Sprint(out["text"]), "Make: run the whole suite") {
		t.Fatalf("repo_log=%v", out)
	}
	if out := call("repo_grep", map[string]any{"pattern": "go test"}); !strings.Contains(fmt.Sprint(out["text"]), "Makefile:2:") {
		t.Fatalf("repo_grep=%v", out)
	}
	if out := call("repo_read", map[string]any{"path": "Makefile"}); !strings.Contains(fmt.Sprint(out["text"]), "1\ttest:") {
		t.Fatalf("repo_read=%v", out)
	}
	if out := call("repo_read", map[string]any{"path": "missing.txt"}); out["status"] != "unavailable" {
		t.Fatalf("missing file=%v", out)
	}
	if out := call("issues_search", map[string]any{"query": "flaky suite"}); out["status"] != "ok" || !strings.Contains(fmt.Sprint(out["issues"]), "Earlier report: flaky suite") {
		t.Fatalf("issues_search=%v", out)
	}
	if out := call("issues_search", map[string]any{"query": "x", "where": "upstream"}); out["status"] != "unavailable" {
		t.Fatalf("upstream without configuration=%v", out)
	}
	view, err := s.Inspect(t.Context(), f.h.a.ID, 0, 100)
	if err != nil || view.Read.IssueSearches != 2 || view.Read.RepositoryReads != 4 {
		t.Fatalf("reading=%+v %v", view.Read, err)
	}
}
