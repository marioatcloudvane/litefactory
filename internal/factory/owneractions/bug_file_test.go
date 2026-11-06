package owneractions

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type fakeBugLabels struct {
	mu      sync.Mutex
	missing map[string]bool
}

func (l *fakeBugLabels) RepoLabelNodeID(_ context.Context, repo github.Repo, name string) (string, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if name != BugLabel || l.missing[repo.String()] {
		return "", false, nil
	}
	return "LA_" + repo.Name, true, nil
}

type bugFixture struct {
	brainstormFixture
	labels *fakeBugLabels
	bugs   BugFiles
	repoID int64
}

func newBugFixture(t *testing.T) bugFixture {
	t.Helper()
	base := newBrainstormFixture(t, "repository:actions")
	repo, err := base.db.GetRepoBySlug(t.Context(), store.ForgeGitHub, "acme", "api")
	if err != nil {
		t.Fatal(err)
	}
	base.exec(t, `INSERT INTO connector_grant_action_repositories(grant_id,repo_id,repository_ref) VALUES('grant',?,'github:acme/api')`, repo.ID)
	if _, err = base.db.UpsertRepo(t.Context(), store.Repo{Forge: "github", Owner: "acme", Name: "web", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	labels := &fakeBugLabels{missing: map[string]bool{}}
	protect := func(_ context.Context, s string) (string, error) {
		return strings.ReplaceAll(s, "SECRET", "[redacted]"), nil
	}
	f := bugFixture{brainstormFixture: base, labels: labels, repoID: repo.ID}
	f.bugs = BugFiles{DB: base.db, Labels: labels, Protect: protect, Availability: Availability{BugFile: true}}
	return f
}

func bugInvocation(repo, title, description string) owneraction.Invocation {
	return owneraction.Invocation{Command: BugFileCommand, Arguments: map[string]string{"repository": repo, "title": title, "description": description}}
}

var bugMarkerPattern = regexp.MustCompile(`^<!-- lf:bug-filing v=1 marker=[0-9a-f]{32} -->$`)

func frozenBug(t *testing.T, payload string) bugFrozen {
	t.Helper()
	var v bugFrozen
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestBugFileResolveFreezesContentAndMarker(t *testing.T) {
	f := newBugFixture(t)
	facts, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", "Crash on save", "Saving crashes."), f.at)
	if err != nil {
		t.Fatal(err)
	}
	v := frozenBug(t, facts.Payload)
	if !bugMarkerPattern.MatchString(v.Marker) || facts.TargetID != v.Marker {
		t.Fatalf("marker = %q", v.Marker)
	}
	if v.Title != "Crash on save" || v.Body != "Saving crashes.\n\nFiled for the owner from a chat client.\n\n"+v.Marker {
		t.Fatalf("frozen = %+v", v)
	}
	if facts.RepositoryID != f.repoID || facts.RepositoryRef != "github:acme/api" || v.RepositoryID != f.repoID {
		t.Fatalf("facts = %+v", facts)
	}
	again, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", "Crash on save", "Saving crashes."), f.at)
	if err != nil || frozenBug(t, again.Payload).Marker == v.Marker {
		t.Fatalf("a second resolve reused the marker: %v", err)
	}
	if err = f.bugs.Recheck(t.Context(), f.p, facts, f.at); err != nil {
		t.Fatal(err)
	}
	tampered := facts
	tampered.Payload = strings.Replace(facts.Payload, "Saving", "Loading", 1)
	if err = f.bugs.Recheck(t.Context(), f.p, tampered, f.at); !errors.Is(err, owneraction.ErrIntegrity) {
		t.Fatalf("tampered = %v", err)
	}
}

func TestBugFileResolveRefusals(t *testing.T) {
	f := newBugFixture(t)
	long := strings.Repeat("é", BugTitleLimit+1)
	cases := map[string]struct {
		in   owneraction.Invocation
		want string
	}{
		"blank title":      {bugInvocation("acme/api", "  ", "d"), ""},
		"blank repository": {bugInvocation(" ", "t", "d"), ""},
		"long title":       {bugInvocation("acme/api", long, "d"), "256"},
		"long description": {bugInvocation("acme/api", "t", strings.Repeat("a", BugDescriptionLimit+1)), ""},
		"unregistered":     {bugInvocation("acme/none", "t", "d"), "Unknown project"},
		"redacted title":   {bugInvocation("acme/api", "SECRET", "d"), "protection changed"},
		"redacted body":    {bugInvocation("acme/api", "t", "token SECRET"), "protection changed"},
	}
	for name, c := range cases {
		_, err := f.bugs.Resolve(t.Context(), f.p, c.in, f.at)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s = %v", name, err)
		}
	}
	if _, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", strings.Repeat("é", BugTitleLimit), "d"), f.at); err != nil {
		t.Fatalf("256-character title = %v", err)
	}
	if _, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", "t", strings.Repeat("a", BugDescriptionLimit)), f.at); err != nil {
		t.Fatalf("8192-byte description = %v", err)
	}
	if _, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/web", "t", "d"), f.at); !errors.Is(err, store.ErrConnectorScope) {
		t.Fatalf("outside grant = %v", err)
	}
	f.labels.missing["acme/api"] = true
	if _, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", "t", "d"), f.at); err == nil || !strings.Contains(err.Error(), "no label named bug") {
		t.Fatalf("missing label = %v", err)
	}
	f.labels.missing["acme/api"] = false
	ts := store.FormatTime(f.at)
	f.exec(t, `INSERT INTO project_connection_versions(id,repo_id,role,revision,provider,api_origin,destination,enabled,provenance,created_at) VALUES(900,?,'ticket',1,'linear','https://api.linear.app','team',1,'test',?)`, f.repoID, ts)
	f.exec(t, `INSERT INTO project_connections(repo_id,role,version_id) VALUES(?,'ticket',900)`, f.repoID)
	if _, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", "t", "d"), f.at); err == nil || err.Error() != "Filing a bug on Linear is not supported yet" {
		t.Fatalf("linear = %v", err)
	}
}

func TestBugFileRecheckRefusesStaleTarget(t *testing.T) {
	f := newBugFixture(t)
	facts, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("api", "t", "d"), f.at)
	if err != nil {
		t.Fatal(err)
	}
	f.labels.missing["acme/api"] = true
	if err = f.bugs.Recheck(t.Context(), f.p, facts, f.at); err == nil || !strings.Contains(err.Error(), "request a fresh proposal") {
		t.Fatalf("label removed = %v", err)
	}
}

func TestBugFileExecuteAdmitsOnceAndRecoverReports(t *testing.T) {
	f := newBugFixture(t)
	facts, err := f.bugs.Resolve(t.Context(), f.p, bugInvocation("acme/api", "t", "d"), f.at)
	if err != nil {
		t.Fatal(err)
	}
	v := frozenBug(t, facts.Payload)
	res, err := f.bugs.Execute(t.Context(), f.p, facts, "owner-action:p1", f.at)
	if err != nil || res.State != "executing" || res.Summary != "filing" {
		t.Fatalf("execute = %+v %v", res, err)
	}
	again, err := f.bugs.Execute(t.Context(), f.p, facts, "owner-action:p1", f.at)
	if err != nil || again != res {
		t.Fatalf("replay = %+v %v", again, err)
	}
	filing, err := f.db.BugFilingByExecution(t.Context(), "owner-action:p1")
	if err != nil || filing.Marker != v.Marker || filing.Body != v.Body || filing.Title != "t" || filing.OwnerActionID != "p1" || filing.RepositoryID != f.repoID {
		t.Fatalf("filing = %+v %v", filing, err)
	}
	filing.State, filing.IssueLink = store.BugFilingCreated, "https://github.com/acme/api/issues/7"
	if ok, err := f.db.SaveBugFiling(t.Context(), filing, store.BugFilingAdmitted, f.at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	rec, found, err := f.bugs.Recover(t.Context(), f.p, facts, "owner-action:p1", f.at)
	if err != nil || !found || rec.State != "executing" || rec.Summary != "filed but not yet labelled" || rec.Reference != filing.IssueLink {
		t.Fatalf("recover = %+v %v %v", rec, found, err)
	}
}

func TestBugFilingResultMapping(t *testing.T) {
	link := "https://github.com/acme/api/issues/7"
	for _, c := range []struct {
		state, reason, wantState, wantSummary string
	}{
		{store.BugFilingAdmitted, "", "executing", "filing"},
		{store.BugFilingCreating, "", "executing", "filing"},
		{store.BugFilingUncertain, "", "executing", "filing; checking whether the issue was created"},
		{store.BugFilingCreated, "", "executing", "filed but not yet labelled"},
		{store.BugFilingLabelled, "", "executing", "labelled but not yet started"},
		{store.BugFilingLabelled, "labelled; Bug run recorded but not started; reply run it again on the issue to continue", "executing", "labelled; Bug run recorded but not started; reply run it again on the issue to continue"},
		{store.BugFilingStartAdmitted, "", "executing", "labelled; Bug admitted, analysis not yet dispatched"},
		{store.BugFilingDispatched, "", "succeeded", "Bug analysis started"},
		{store.BugFilingNotCreated, "authority was revoked; nothing was filed", "failed", "authority was revoked; nothing was filed"},
		{store.BugFilingFailedUnconfirmed, "unconfirmed", "failed", "unconfirmed"},
		{store.BugFilingLabelFailed, "label failed", "failed", "label failed"},
		{store.BugFilingStartFailed, "start failed", "failed", "start failed"},
	} {
		got := BugFilingResult(store.BugFiling{State: c.state, Reason: c.reason, IssueLink: link})
		if got.State != c.wantState || got.Summary != c.wantSummary || got.Reference != link {
			t.Errorf("%s = %+v", c.state, got)
		}
	}
}

// failingExecute leaves the filing missing, as a crash between the execution
// claim and the admission would.
type failingExecute struct{ BugFiles }

func (d failingExecute) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, error) {
	return owneraction.Result{}, errors.New("stopped before admission")
}

type bugService struct {
	*owneraction.Service
	mu  sync.Mutex
	now time.Time
}

func (f bugFixture) service(domain owneraction.Domain) *bugService {
	s := &bugService{now: f.at}
	s.Service = owneraction.New(f.db, domain, func() time.Time { s.mu.Lock(); defer s.mu.Unlock(); return s.now })
	return s
}

func (s *bugService) advance(d time.Duration) { s.mu.Lock(); s.now = s.now.Add(d); s.mu.Unlock() }

func TestBugFileReplayIdentityThroughOwnerActions(t *testing.T) {
	f := newBugFixture(t)
	s := f.service(f.bugs)
	in := bugInvocation("acme/api", "Crash on save", "Saving crashes.")
	first, err := s.Propose(t.Context(), f.p, in, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Propose(t.Context(), f.p, bugInvocation("acme/api", "Crash on save", "Saving crashes."), "key-1")
	if err != nil || retry.ID != first.ID || frozenBug(t, retry.Facts.Payload).Marker != frozenBug(t, first.Facts.Payload).Marker {
		t.Fatalf("retry = %+v %v", retry.ID, err)
	}
	if _, err = s.Propose(t.Context(), f.p, bugInvocation("acme/api", "Crash on load", "Saving crashes."), "key-1"); !errors.Is(err, owneraction.ErrRequestConflict) {
		t.Fatalf("changed title with same key = %v", err)
	}
	fresh, err := s.Propose(t.Context(), f.p, in, "key-2")
	if err != nil || fresh.ID == first.ID || frozenBug(t, fresh.Facts.Payload).Marker == frozenBug(t, first.Facts.Payload).Marker {
		t.Fatalf("new key = %+v %v", fresh.ID, err)
	}
}

func confirmed(t *testing.T, f bugFixture, s *bugService, key string) owneraction.Proposal {
	t.Helper()
	p, err := s.Propose(t.Context(), f.p, bugInvocation("acme/api", "t", "d"), key)
	if err != nil {
		t.Fatal(err)
	}
	s.advance(time.Minute)
	if _, err = s.Confirm(t.Context(), f.p, p.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBugFileRecoverAdmitsMissingFiling(t *testing.T) {
	f := newBugFixture(t)
	s := f.service(failingExecute{f.bugs})
	p := confirmed(t, f, s, "key-1")
	got, err := s.Step(t.Context(), p.ID)
	if err != nil || got.State != "uncertain" {
		t.Fatalf("failed execute = %+v %v", got.State, err)
	}
	if _, err = f.db.BugFilingByExecution(t.Context(), p.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("filing before recovery = %v", err)
	}
	got, err = s.Step(t.Context(), p.ID)
	if err != nil || got.State != "executing" || got.Result.Summary != "filing" {
		t.Fatalf("recovered = %+v %+v %v", got.State, got.Result, err)
	}
	filing, err := f.db.BugFilingByExecution(t.Context(), p.ExecutionID)
	if err != nil || filing.Marker != frozenBug(t, p.Facts.Payload).Marker {
		t.Fatalf("admitted = %+v %v", filing, err)
	}
	// A later step reads the admitted filing again; nothing is admitted twice.
	if got, err = s.Step(t.Context(), p.ID); err != nil || got.State != "executing" {
		t.Fatalf("executing replay = %+v %v", got.State, err)
	}
}

func TestBugFileRecoverRefusesAfterRevocation(t *testing.T) {
	f := newBugFixture(t)
	s := f.service(failingExecute{f.bugs})
	p := confirmed(t, f, s, "key-1")
	if got, err := s.Step(t.Context(), p.ID); err != nil || got.State != "uncertain" {
		t.Fatalf("failed execute = %+v %v", got.State, err)
	}
	f.exec(t, `UPDATE connector_grants SET revoked_at=? WHERE id='grant'`, store.FormatTime(f.at))
	got, err := s.Step(t.Context(), p.ID)
	if err != nil || got.State != "failed" || got.Result.Summary != "authority was revoked before filing; nothing was filed" {
		t.Fatalf("revoked = %+v %+v %v", got.State, got.Result, err)
	}
	if _, err = f.db.BugFilingByExecution(t.Context(), p.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("filing after revocation = %v", err)
	}
}

func TestBugFileCommandClassifiedAndCatalogedOnlyWhenAvailable(t *testing.T) {
	tier, scope, err := owneraction.Classify(owneraction.Invocation{Command: BugFileCommand})
	if err != nil || tier != owneraction.Conversational || scope != "repository:actions" {
		t.Fatalf("classify = %q %q %v", tier, scope, err)
	}
	var entry *owneraction.ActionDefinition
	for _, e := range (Availability{BugFile: true}).ActionCatalog() {
		if e.Command == BugFileCommand {
			entry = &e
		}
	}
	if entry == nil || len(entry.Arguments) != 3 {
		t.Fatalf("catalog = %+v", entry)
	}
	for _, a := range entry.Arguments {
		if !a.Required || a.Type != string(TextArgument) {
			t.Fatalf("argument = %+v", a)
		}
	}
	for _, e := range (Availability{Brainstorm: true, Features: true}).ActionCatalog() {
		if e.Command == BugFileCommand {
			t.Fatal("bug_file listed while unavailable")
		}
	}
	if _, ok := (Domain{BugFile: BugFiles{}}).operation(BugFileCommand).(BugFiles); !ok {
		t.Fatal("bug_file not routed")
	}
}
