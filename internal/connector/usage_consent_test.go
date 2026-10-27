package connector

import (
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestUsageConsentRequiresIndependentTargetsAndReportsCoverage(t *testing.T) {
	scope := FactoryUsageScope + " " + RepositoryUsageScope + " " + TaskUsageScope
	j := beginBrowserJourneyScopes(t, browserIdentity{owner: 42}, scope)
	page := j.callback(t, j.state)
	if page.Code != http.StatusOK {
		t.Fatalf("callback %d %s", page.Code, page.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())[1]
	cookie := page.Result().Cookies()[0]
	for _, field := range []string{"usage_repository_ref", "usage_task_ref"} {
		if !strings.Contains(page.Body.String(), `name="`+field+`"`) {
			t.Fatalf("missing %s", field)
		}
	}
	repo, err := j.fixture.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := j.fixture.db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"scope": strings.Fields(scope), "duration": {"1"}, "usage_repository_ref": {"github:acme/api"}, "usage_task_ref": {"github:missing/repo#7"}}
	if code := postConsent(t, j, csrf, cookie, form); code != http.StatusBadRequest {
		t.Fatalf("unknown task %d", code)
	}
	browserNoGrants(t, j.fixture)
	form.Set("usage_task_ref", session.TaskRef)
	if code := postConsent(t, j, csrf, cookie, form); code != http.StatusFound {
		t.Fatalf("Allow %d", code)
	}
	grants, err := NewGrantService(j.fixture.db).ListGrantSummaries(t.Context(), j.fixture.now)
	if err != nil || len(grants) != 1 {
		t.Fatalf("local grants %+v %v", grants, err)
	}
	coverage := grants[0].Coverage
	if !coverage.FactoryUsage || coverage.UsageRepositoryRef != "github:acme/api" || coverage.UsageTaskRef != session.TaskRef || !coverage.UsageTaskFamily || coverage.TaskRef != "" || coverage.AllRegisteredRepositories {
		t.Fatalf("wrong independent coverage %+v", coverage)
	}
}
