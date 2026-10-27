package connector

import (
	"github.com/cloudvane/litefactory/internal/store"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDiscussionConsentRequiresExistingExactTaskAndReportsCoverage(t *testing.T) {
	for _, scope := range []string{DiscussionScope, Scope + " " + DiscussionScope} {
		t.Run(scope, func(t *testing.T) {
			j := beginBrowserJourneyScopes(t, browserIdentity{owner: 42}, scope)
			page := j.callback(t, j.state)
			if page.Code != http.StatusOK {
				t.Fatalf("callback %d: %s", page.Code, page.Body.String())
			}
			csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())[1]
			cookie := page.Result().Cookies()[0]
			if !strings.Contains(page.Body.String(), `name="task_ref"`) {
				t.Fatal("missing exact-task selection")
			}
			if !strings.Contains(scope, Scope) && strings.Contains(page.Body.String(), "all registered repositories") {
				t.Fatal("discussion claims repository-wide coverage")
			}
			browserNoGrants(t, j.fixture)
			if code := postConsent(t, j, csrf, cookie, url.Values{"scope": strings.Fields(scope), "duration": {"1"}, "task_ref": {"github:missing/task#1"}}); code != http.StatusBadRequest {
				t.Fatalf("unknown target: %d", code)
			}
			browserNoGrants(t, j.fixture)
			repo, err := j.fixture.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			session, err := j.fixture.db.GetOrCreateSession(t.Context(), repo, 7)
			if err != nil {
				t.Fatal(err)
			}
			if code := postConsent(t, j, csrf, cookie, url.Values{"scope": strings.Fields(scope), "duration": {"1"}, "task_ref": {session.TaskRef}}); code != http.StatusFound {
				t.Fatalf("Allow: %d", code)
			}
			summaries, err := NewGrantService(j.fixture.db).ListGrantSummaries(t.Context(), j.fixture.now)
			if err != nil || len(summaries) != 1 {
				t.Fatalf("local grants: %+v %v", summaries, err)
			}
			coverage := summaries[0].Coverage
			if coverage.TaskRef != session.TaskRef || coverage.AllRegisteredRepositories != strings.Contains(scope, Scope) || coverage.FutureRepositories != strings.Contains(scope, Scope) {
				t.Fatalf("wrong coverage: %+v", coverage)
			}
		})
	}
}
