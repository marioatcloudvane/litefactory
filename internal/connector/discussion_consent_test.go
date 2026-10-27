package connector

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func registerTestRepo(t *testing.T, db *store.DB, owner, name string) store.Repo {
	t.Helper()
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), name), Forge: store.ForgeGitHub, Owner: owner, Name: name, RemoteURL: "git@github.com:" + owner + "/" + name + ".git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func registerTestTask(t *testing.T, db *store.DB, repo store.Repo, issue int) store.Session {
	t.Helper()
	session, err := db.GetOrCreateSession(t.Context(), repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// consentPageFor runs a browser journey to the consent page.
func consentPageFor(t *testing.T, f *connectorFixture, scope string) (browserJourney, string, string, *http.Cookie) {
	t.Helper()
	j := beginBrowserJourneyFixture(t, f, browserIdentity{owner: 42}, scope)
	page := j.callback(t, j.state)
	if page.Code != http.StatusOK {
		t.Fatalf("callback %d: %s", page.Code, page.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())[1]
	return j, page.Body.String(), csrf, page.Result().Cookies()[0]
}

func postConsentResponse(t *testing.T, j browserJourney, csrf string, cookie *http.Cookie, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{"csrf_token": {csrf}, "decision": {"allow"}}
	for k, v := range extra {
		values[k] = v
	}
	return connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", values, nil, cookie)
}

// exchangeConsent trades the consent redirect's code for tokens.
func exchangeConsent(t *testing.T, j browserJourney, consent *httptest.ResponseRecorder) connectorTestTokens {
	t.Helper()
	if consent.Code != http.StatusFound {
		t.Fatalf("allow: HTTP %d %s", consent.Code, consent.Body.String())
	}
	location, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := j.fixture.db.ListConnectorGrants(t.Context(), 42, j.fixture.now)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants %+v %v", grants, err)
	}
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {grants[0].ClientID}, "redirect_uri": {"https://client.example/callback"}, "code": {location.Query().Get("code")}, "code_verifier": {strings.Repeat("v", 43)}}
	response := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/token", exchange, nil)
	var tokens connectorTestTokens
	connectorDecode(t, response, &tokens)
	if response.Code != http.StatusOK {
		t.Fatalf("token: HTTP %d %s", response.Code, response.Body.String())
	}
	return tokens
}

type toolCallResult struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Result struct {
		IsError    bool `json:"isError"`
		Structured struct {
			Summary string `json:"summary"`
		} `json:"structuredContent"`
	} `json:"result"`
}

func callTool(t *testing.T, handler http.Handler, f *connectorFixture, token, name string, args map[string]any) (int, toolCallResult) {
	t.Helper()
	response := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, connectorBearer(token))
	var out toolCallResult
	if response.Code == http.StatusOK {
		connectorDecode(t, response, &out)
	}
	return response.Code, out
}

func (r toolCallResult) ok() bool { return r.Error == nil && !r.Result.IsError }

func coverageFixture(t *testing.T) (*connectorFixture, *[]store.ConnectorGrantTarget, *[]store.ConnectorCodeTarget, *[]UsageRequest) {
	t.Helper()
	f := newConnectorFixture(t, nil)
	var tasks []store.ConnectorGrantTarget
	var code []store.ConnectorCodeTarget
	var usage []UsageRequest
	f.options.TaskEvidence = fixtureEvidence(func(_ context.Context, target store.ConnectorGrantTarget, _ EvidenceRequest) (EvidencePage, error) {
		tasks = append(tasks, target)
		return EvidencePage{Revision: "rev"}, nil
	})
	f.options.CodeEvidence = codeProtocolProvider(func(_ context.Context, target store.ConnectorCodeTarget, _ CodeEvidenceRequest) (CodeEvidencePage, error) {
		code = append(code, target)
		return CodeEvidencePage{Pin: CodeEvidencePin{ObservedAt: f.now, Policy: "test"}}, nil
	})
	f.options.Usage = &usageTestProvider{call: func(_ context.Context, r UsageRequest) (UsageProjection, error) {
		usage = append(usage, r)
		return usageTestProjection(1), nil
	}}
	return f, &tasks, &code, &usage
}

func TestConsentPageListsRegisteredRepositoriesAsCheckboxes(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	registerTestRepo(t, f.db, "acme", "web")
	registerTestRepo(t, f.db, "acme", "api")
	_, page, _, _ := consentPageFor(t, f, DiscussionScope)
	all := regexp.MustCompile(`<input [^>]*name="repositories"[^>]*>`).FindAllString(page, -1)
	if len(all) != 1 || !strings.Contains(all[0], `value="all"`) || !strings.Contains(all[0], `type="checkbox"`) || !regexp.MustCompile(`\schecked[\s>]`).MatchString(all[0]) {
		t.Fatalf("all-repositories checkbox %v", all)
	}
	if !strings.Contains(page, "All repositories (including ones registered later)") {
		t.Fatal("all-repositories label missing")
	}
	var values []string
	for _, input := range regexp.MustCompile(`<input [^>]*name="repository"[^>]*>`).FindAllString(page, -1) {
		if !strings.Contains(input, `type="checkbox"`) || regexp.MustCompile(`\schecked[\s>]`).MatchString(input) {
			t.Fatalf("repository input %q", input)
		}
		values = append(values, regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(input)[1])
	}
	if !slices.Equal(values, []string{"github:acme/api", "github:acme/web"}) {
		t.Fatalf("repository checkboxes %v", values)
	}
	for _, field := range []string{"task_ref", "code_repository_ref", "usage_repository_ref", "usage_task_ref", `type="text"`} {
		if strings.Contains(page, field) {
			t.Errorf("consent still asks for free text %s", field)
		}
	}
}

func TestConsentNamesWhatIsWrongAndStaysUsable(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	registerTestRepo(t, f.db, "acme", "api")
	j, _, csrf, cookie := consentPageFor(t, f, DiscussionScope)
	for name, tc := range map[string]struct {
		form url.Values
		want string
	}{
		"no repository":             {url.Values{"scope": {DiscussionScope, TaskUsageScope}}, "choose all repositories or at least one registered repository for task:discussion, task:usage"},
		"unknown repository":        {url.Values{"scope": {DiscussionScope}, "repository": {"github:missing/repo"}}, `repository not registered: "github:missing/repo"`},
		"code without discussion":   {url.Values{"scope": {CodeReviewScope}, "repositories": {"all"}}, "task:code-review also needs task:discussion; tick both"},
		"unknown repository choice": {url.Values{"scope": {DiscussionScope}, "repositories": {"some"}}, `unknown repository choice: "some"`},
	} {
		tc.form.Set("duration", "1")
		response := postConsentResponse(t, j, csrf, cookie, tc.form)
		body := html.UnescapeString(response.Body.String())
		if response.Code != http.StatusBadRequest || !strings.Contains(body, tc.want) || strings.Contains(body, "invalid_grant") {
			t.Errorf("%s: HTTP %d %s", name, response.Code, body)
		}
	}
	browserNoGrants(t, f)
	if strings.Contains(f.logs.String(), "invalid_grant") || !strings.Contains(f.logs.String(), `"reason":"consent:invalid_request"`) {
		t.Errorf("audit %s", f.logs.String())
	}
	// Scopes without repository coverage need no selection.
	if code := postConsent(t, j, csrf, cookie, url.Values{"scope": {Scope, FactoryUsageScope}, "duration": {"1"}}); code != http.StatusFound {
		t.Fatalf("allow without repository scopes: HTTP %d", code)
	}
}

// All repositories covers every task, including tasks in repositories
// registered after consent; the task is chosen on each call and code review
// derives the task's own code repository.
func TestAllRepositoriesGrantCoversTasksChosenPerCall(t *testing.T) {
	t.Parallel()
	f, tasks, code, usage := coverageFixture(t)
	codeTarget := seedCodeProtocolTarget(t, f.db)
	scope := strings.Join(scopeNames(), " ")
	j, _, csrf, cookie := consentPageFor(t, f, scope)
	tokens := exchangeConsent(t, j, postConsentResponse(t, j, csrf, cookie, url.Values{"scope": strings.Fields(scope), "duration": {"1"}, "repositories": {"all"}}))
	later := registerTestTask(t, f.db, registerTestRepo(t, f.db, "acme", "web"), 3)
	handler := f.server(t)
	for _, task := range []string{codeTarget.TaskRef, later.TaskRef} {
		if status, out := callTool(t, handler, f, tokens.AccessToken, "task_evidence_read", map[string]any{"task_ref": task, "section": "status"}); status != http.StatusOK || !out.ok() {
			t.Fatalf("discussion %s: %d %+v", task, status, out)
		}
	}
	if len(*tasks) != 2 || (*tasks)[0].TaskRef != codeTarget.TaskRef || (*tasks)[1].SessionID != later.ID {
		t.Fatalf("evidence targets %+v", *tasks)
	}
	if status, out := callTool(t, handler, f, tokens.AccessToken, "repo_read", map[string]any{"task_ref": codeTarget.TaskRef}); status != http.StatusOK || !out.ok() {
		t.Fatalf("code review: %d %+v", status, out)
	}
	if len(*code) != 1 || (*code)[0] != codeTarget {
		t.Fatalf("code target %+v, want %+v", *code, codeTarget)
	}
	if status, out := callTool(t, handler, f, tokens.AccessToken, "usage_read", map[string]any{"scope": "task", "repository": "github:acme/web", "issue": 3}); status != http.StatusOK || !out.ok() {
		t.Fatalf("task usage: %d %+v", status, out)
	}
	if status, out := callTool(t, handler, f, tokens.AccessToken, "usage_read", map[string]any{"scope": "repository", "repository": "github:acme/web"}); status != http.StatusOK || !out.ok() {
		t.Fatalf("repository usage: %d %+v", status, out)
	}
	if len(*usage) != 2 || (*usage)[0].Filter.RepoID != later.RepoID || (*usage)[1].Filter.RepoID != later.RepoID {
		t.Fatalf("usage requests %+v", *usage)
	}
	grants, err := NewGrantService(f.db).ListGrantSummaries(t.Context(), f.now)
	if err != nil || len(grants) != 1 || grants[0].Coverage.Repositories == nil || !grants[0].Coverage.Repositories.All || !grants[0].Coverage.Repositories.Future {
		t.Fatalf("grant coverage %+v %v", grants, err)
	}
}

// One selected repository refuses tasks and repositories elsewhere, naming the
// reason in the tool result; revocation still cuts everything.
func TestOneRepositoryGrantRefusesOtherRepositoriesClearly(t *testing.T) {
	t.Parallel()
	f, tasks, _, usage := coverageFixture(t)
	api := registerTestRepo(t, f.db, "acme", "api")
	covered := registerTestTask(t, f.db, api, 7)
	other := registerTestTask(t, f.db, registerTestRepo(t, f.db, "acme", "web"), 3)
	scope := DiscussionScope + " " + RepositoryUsageScope + " " + TaskUsageScope
	j, _, csrf, cookie := consentPageFor(t, f, scope)
	tokens := exchangeConsent(t, j, postConsentResponse(t, j, csrf, cookie, url.Values{"scope": strings.Fields(scope), "duration": {"1"}, "repository": {"github:acme/api"}}))
	handler := f.server(t)
	if status, out := callTool(t, handler, f, tokens.AccessToken, "task_evidence_read", map[string]any{"task_ref": covered.TaskRef, "section": "status"}); status != http.StatusOK || !out.ok() {
		t.Fatalf("covered task: %d %+v", status, out)
	}
	for task, want := range map[string]string{
		other.TaskRef:         `Access refused: repository not covered by this grant: "github:acme/web" (task "github:acme/web#3").`,
		"github:acme/api#999": `Access refused: task not found: "github:acme/api#999".`,
	} {
		status, out := callTool(t, handler, f, tokens.AccessToken, "task_evidence_read", map[string]any{"task_ref": task, "section": "status"})
		if status != http.StatusOK || !out.Result.IsError || out.Result.Structured.Summary != want {
			t.Fatalf("%s: %d %+v", task, status, out)
		}
	}
	if len(*tasks) != 1 {
		t.Fatalf("refused calls reached evidence: %+v", *tasks)
	}
	for _, args := range []map[string]any{{"scope": "repository", "repository": "github:acme/web"}, {"scope": "task", "repository": "github:acme/web", "issue": 3}} {
		status, out := callTool(t, handler, f, tokens.AccessToken, "usage_read", args)
		if status != http.StatusOK || out.Error == nil || !strings.Contains(out.Error.Message, `repository not covered by this grant: "github:acme/web"`) {
			t.Fatalf("usage %v: %d %+v", args, status, out)
		}
	}
	if len(*usage) != 0 {
		t.Fatalf("refused usage reached provider: %+v", *usage)
	}
	grants, err := NewGrantService(f.db).ListGrantSummaries(t.Context(), f.now)
	if err != nil || len(grants) != 1 || grants[0].Coverage.Repositories == nil || grants[0].Coverage.Repositories.All || !slices.Equal(grants[0].Coverage.Repositories.Repositories, []string{"github:acme/api"}) {
		t.Fatalf("grant coverage %+v %v", grants, err)
	}
	if _, err = NewGrantService(f.db).RevokeGrant(t.Context(), grants[0].ID, f.now); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	if status, _ := callTool(t, handler, f, tokens.AccessToken, "task_evidence_read", map[string]any{"task_ref": covered.TaskRef, "section": "status"}); status != http.StatusUnauthorized {
		t.Fatalf("revoked grant answered: %d", status)
	}
}
