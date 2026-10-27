package codeevidence_test

import (
	"encoding/json"
	"html"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/codeevidence"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type providerFixture struct {
	provider   *codeevidence.Provider
	target     store.ConnectorCodeTarget
	repo       *gittest.Repo
	db         *store.DB
	base, task string
}

func newProviderFixture(t *testing.T, content string, r *redact.Redactor) providerFixture {
	t.Helper()
	repo := gittest.New(t)
	repo.Git("remote", "set-url", "origin", "https://github.com/acme/code.git")
	repo.WriteFile("evidence.txt", "base evidence\n")
	base := repo.Commit("Base evidence")
	repo.Git("update-ref", "refs/remotes/origin/main", base)
	repo.Git("checkout", "-b", "review-task")
	repo.WriteFile("evidence.txt", content)
	task := repo.Commit("Task evidence")
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "provider.db"))
	mustReview(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mustReview(t, db.Migrate(t.Context()))
	target := seedReviewTarget(t, db, repo.Dir)
	seedReviewBook(t, db)
	binary, err := git.Resolve(t.Context())
	mustReview(t, err)
	if r == nil {
		r = redact.New(redact.Options{Environ: []string{}})
	}
	now := time.Date(2026, time.October, 27, 11, 30, 0, 0, time.UTC)
	return providerFixture{codeevidence.New(db, binary, r, func() time.Time { return now }), target, repo, db, base, task}
}

func providerPages(t *testing.T, f providerFixture, request connector.CodeEvidenceRequest) ([]map[string]any, connector.CodeEvidencePin) {
	t.Helper()
	var all []map[string]any
	var pin connector.CodeEvidencePin
	for n := 0; n < 1000; n++ {
		page, err := f.provider.Evidence(t.Context(), f.target, request)
		mustReview(t, err)
		if n == 0 {
			pin = page.Pin
		} else if !reflect.DeepEqual(pin, page.Pin) {
			t.Fatal("evidence pin changed between pages")
		}
		all = append(all, page.Items...)
		if !page.HasMore {
			return all, pin
		}
		if page.NextOffset <= request.Offset {
			t.Fatal("pagination did not advance")
		}
		request.Pin, request.Offset = page.Pin, page.NextOffset
	}
	t.Fatal("finite source did not finish in bounded pages")
	return nil, pin
}
func providerContents(t *testing.T, items []map[string]any) string {
	t.Helper()
	var text strings.Builder
	end := 0
	for _, item := range items {
		part, ok := item["content"].(string)
		if !ok {
			t.Fatalf("missing content: %+v", item)
		}
		start, ok := item["offset"].(float64)
		if !ok {
			t.Fatalf("missing offset: %+v", item)
		}
		next, ok := item["end"].(float64)
		if !ok {
			t.Fatalf("missing end: %+v", item)
		}
		if int(start) != end || int(next)-int(start) != len(part) || !utf8.ValidString(part) {
			t.Fatalf("invalid Unicode range: %v", item)
		}
		text.WriteString(part)
		end = int(next)
	}
	return text.String()
}

func TestProviderSanitizesCompleteUnitsBeforeUnicodePaging(t *testing.T) {
	secret := "private<&\"credential-value-123"
	encoded, err := json.Marshal(secret)
	mustReview(t, err)
	nested, err := json.Marshal(`{"credential":` + string(encoded) + `}`)
	mustReview(t, err)
	content := strings.Repeat("x", 4080) + secret + "\n" + strings.Repeat("é🙂\n", 2000) + "\n" +
		`{"credential":` + string(encoded) + `,"nested":` + string(nested) + "}\n" +
		"<pre>" + html.EscapeString(`{"credential":`+string(encoded)+`}`) + "</pre>\n" +
		"https://user:password@example.com/source?query-secret=hidden#fragment\n" +
		"/Users/alice/work/private.txt /home/alice/private.txt\n"
	f := newProviderFixture(t, content, redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}}))
	items, pin := providerPages(t, f, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 1})
	got := providerContents(t, items)
	if len(items) < 3 || pin.TaskCommit != f.task || pin.BaseCommit != f.base {
		t.Fatalf("incomplete multi-page evidence: %d %+v", len(items), pin)
	}
	for _, unsafe := range []string{secret, string(encoded), "credential-value-123", "query-secret", "password@example", "/Users/alice", "/home/alice"} {
		if strings.Contains(html.UnescapeString(got), unsafe) {
			t.Fatalf("sensitive complete-unit content survived: %q", unsafe)
		}
	}
	if !strings.Contains(got, strings.Repeat("é🙂\n", 2000)) {
		t.Fatal("safe Unicode source changed")
	}
	if !strings.Contains(got, "https://example.com/source") && !strings.Contains(got, "[link unavailable]") {
		t.Fatal("unsafe URL was neither sanitized nor explicitly withheld")
	}
	if items[len(items)-1]["source_complete"] != true {
		t.Fatal("complete source missing completion flag")
	}
}

func TestProviderPinsBranchAndBookVersionsAcrossMovement(t *testing.T) {
	f := newProviderFixture(t, "task evidence\n"+strings.Repeat("unicode🙂\n", 1000), nil)
	request := connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 1}
	first, err := f.provider.Evidence(t.Context(), f.target, request)
	mustReview(t, err)
	if !first.HasMore {
		t.Fatal("fixture did not produce continuation")
	}
	f.repo.WriteFile("evidence.txt", "new task head\n")
	later := f.repo.Commit("Later head")
	request.Pin = first.Pin
	request.Offset = first.NextOffset
	rest, pin := providerPages(t, f, request)
	if pin.TaskCommit != f.task || strings.Contains(providerContentsFromAnyOffset(t, rest), "new task head") {
		t.Fatal("head movement altered retained view")
	}
	fresh, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 1})
	mustReview(t, err)
	if fresh.Pin.TaskCommit != later || !strings.Contains(providerContentsFromAnyOffset(t, fresh.Items), "new task head") {
		t.Fatal("new view did not observe new head")
	}
	countsRequest := connector.CodeEvidenceRequest{Operation: "standards_role_counts", Categories: []string{"review"}, Limit: 50, Pin: first.Pin}
	counts, err := f.provider.Evidence(t.Context(), f.target, countsRequest)
	mustReview(t, err)
	assertProviderCount(t, counts, "story-worker", 1, 2)
	service := standards.Service{Store: f.db, OwnerID: 42}
	proposal, err := service.Create(t.Context(), "provider-next-book", first.Pin.BookRevision, []standards.Operation{{Kind: "standard.add", Standard: &standards.Standard{ID: "later", CategoryID: "review", Rule: "MUST Later", Description: "Description.", Evidence: "Evidence.", Scope: "Story.", Check: "manual", AppliesTo: []string{"story-worker"}}}})
	mustReview(t, err)
	_, err = service.Approve(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	mustReview(t, err)
	_, err = service.Publish(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	mustReview(t, err)
	pinnedCounts, err := f.provider.Evidence(t.Context(), f.target, countsRequest)
	mustReview(t, err)
	assertProviderCount(t, pinnedCounts, "story-worker", 1, 2)
	if pinnedCounts.Pin.BookDigest != first.Pin.BookDigest {
		t.Fatal("count view changed published book")
	}
	countsRequest.Pin = connector.CodeEvidencePin{}
	currentCounts, err := f.provider.Evidence(t.Context(), f.target, countsRequest)
	mustReview(t, err)
	assertProviderCount(t, currentCounts, "story-worker", 2, 3)
	countsRequest.Categories = []string{"empty"}
	empty, err := f.provider.Evidence(t.Context(), f.target, countsRequest)
	mustReview(t, err)
	assertProviderCount(t, empty, "story-worker", 0, 3)
	countsRequest.Operation = "standards_read"
	countsRequest.Role = "story-worker"
	emptyRules, err := f.provider.Evidence(t.Context(), f.target, countsRequest)
	mustReview(t, err)
	if len(emptyRules.Items) != 0 || len(emptyRules.Gaps) == 0 {
		t.Fatal("empty selection was not a named gap")
	}
}
func providerContentsFromAnyOffset(t *testing.T, items []map[string]any) string {
	t.Helper()
	var text strings.Builder
	for _, item := range items {
		part, ok := item["content"].(string)
		if !ok {
			t.Fatalf("missing content: %v", item)
		}
		text.WriteString(part)
	}
	return text.String()
}
func assertProviderCount(t *testing.T, page connector.CodeEvidencePage, role string, selected, whole int) {
	t.Helper()
	if len(page.Items) != 1 {
		t.Fatalf("count page: %+v", page)
	}
	selectedCounts, ok := page.Items[0]["selected_categories"].(map[string]any)
	if !ok {
		t.Fatalf("selected counts: %+v", page.Items)
	}
	wholeCounts, ok := page.Items[0]["whole_book"].(map[string]any)
	if !ok {
		t.Fatalf("whole counts: %+v", page.Items)
	}
	if selectedCounts[role] != float64(selected) || wholeCounts[role] != float64(whole) {
		t.Fatalf("wrong count bases: %+v", page.Items)
	}
}

func TestProviderNamesMissingAndOversizedEvidenceGaps(t *testing.T) {
	f := newProviderFixture(t, "task evidence\n", nil)
	missing := f.target
	missing.TaskBranch = "missing-task"
	page, err := f.provider.Evidence(t.Context(), missing, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 1})
	mustReview(t, err)
	if len(page.Items) != 0 || page.Pin.TaskCommit != "" || page.Pin.BaseCommit == "" || !strings.Contains(strings.Join(page.Gaps, " "), "selected_branch_unavailable") {
		t.Fatalf("missing task silently substituted base: %+v", page)
	}
	f.repo.WriteFile("large.txt", strings.Repeat("x", (1<<20)+1))
	f.repo.Commit("Oversized evidence")
	large, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "large.txt", Limit: 1})
	mustReview(t, err)
	if len(large.Items) != 0 || !strings.Contains(strings.Join(large.Gaps, " "), "source_exceeds_acquisition_bound") {
		t.Fatalf("oversized source disclosed: %+v", large)
	}
}

func TestProviderSanitizesStructuredPathAndLinkMetadata(t *testing.T) {
	secret := "private<&\"credential-value-123"
	f := newProviderFixture(t, "task evidence\n", redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}}))
	path := "files/" + secret + ".txt"
	f.repo.WriteFile(path, "safe content\n")
	f.repo.Commit("Evidence filename")
	page, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: path, Limit: 1})
	mustReview(t, err)
	raw, err := json.Marshal(page)
	mustReview(t, err)
	decoded, err := url.PathUnescape(string(raw))
	mustReview(t, err)
	decoded = html.UnescapeString(decoded)
	if strings.Contains(decoded, "credential-value-123") {
		t.Fatalf("sensitive encoded path/link metadata survived: %s", raw)
	}
	if len(page.Items) != 1 {
		t.Fatalf("safe file content unavailable: %+v", page)
	}
}

func TestProviderRefusesSensitiveBranchAndChangedPolicy(t *testing.T) {
	secret := "branch-sensitive-value-123"
	redactor := redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	f := newProviderFixture(t, "task evidence\n", redactor)
	f.repo.Git("update-ref", "refs/heads/"+secret, f.task)
	sensitive := f.target
	sensitive.TaskBranch = secret
	page, err := f.provider.Evidence(t.Context(), sensitive, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 1})
	if err == nil || len(page.Items) != 0 || !strings.Contains(err.Error(), "sensitive branch") {
		t.Fatalf("sensitive branch metadata released: items=%v error=%v", page.Items, err)
	}
	request := connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 1}
	first, err := f.provider.Evidence(t.Context(), f.target, request)
	mustReview(t, err)
	redactor.AddToken(secrets.NewToken("new-sensitive-connection-token"))
	request.Pin = first.Pin
	changed, err := f.provider.Evidence(t.Context(), f.target, request)
	if err == nil || len(changed.Items) != 0 || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("changed policy reused view: items=%v error=%v", changed.Items, err)
	}
}

func TestProviderDoesNotInventCommittedLinesAfterMultilineRedaction(t *testing.T) {
	secret := "secret-first\nsecret-second\n"
	r := redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}})
	f := newProviderFixture(t, "first\n"+secret+"last\n", r)
	request := connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 50}
	page, err := f.provider.Evidence(t.Context(), f.target, request)
	mustReview(t, err)
	if len(page.Items) == 0 || len(page.Gaps) == 0 {
		t.Fatalf("redacted layout missing named provenance gap: %+v", page)
	}
	for _, item := range page.Items {
		if _, ok := item["line"]; ok {
			t.Fatal("sanitized line presented as exact committed line")
		}
		if _, ok := item["url"]; ok {
			t.Fatal("invented line permalink")
		}
	}
	request.Pin = page.Pin
	request.StartLine = 4
	request.Lines = 1
	selection, err := f.provider.Evidence(t.Context(), f.target, request)
	mustReview(t, err)
	if len(selection.Items) != 0 || len(selection.Gaps) == 0 {
		t.Fatal("unsupported committed line selection disclosed unrelated lines")
	}
}

func TestProviderSearchSanitizesWholeFilesBeforeMatchingLines(t *testing.T) {
	secret := "multiline-first\nmultiline-second\nmultiline-third"
	f := newProviderFixture(t, "public needle\n"+secret+"\n", redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}}))
	f.repo.WriteFile("public.txt", "public needle\n")
	f.repo.Commit("Add public search evidence")
	request := connector.CodeEvidenceRequest{Operation: "repo_grep", Branch: "task", Pattern: "needle|multiline-second", Limit: 50}
	page, err := f.provider.Evidence(t.Context(), f.target, request)
	mustReview(t, err)
	raw, err := json.Marshal(page.Items)
	mustReview(t, err)
	if !strings.Contains(string(raw), "public needle") || strings.Contains(string(raw), "multiline-second") {
		t.Fatalf("search clipped a secret before sanitation: %s", raw)
	}
}

func TestProviderWithholdsIncompleteEncodedSourceUnits(t *testing.T) {
	secret := "private<&\"credential-value-123"
	encoded, err := json.Marshal(secret)
	mustReview(t, err)
	malformed := `{"credential":` + strings.TrimSuffix(string(encoded), `"`)
	f := newProviderFixture(t, malformed, redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}}))
	page, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 50})
	if err == nil && len(page.Items) > 0 {
		t.Fatal("incomplete encoded credential source disclosed")
	}
}

func TestProviderDiffWithholdsClippedMultilineSecrets(t *testing.T) {
	secret := strings.Repeat("secret-key-line\n", 20)
	f := newProviderFixture(t, "before\n"+secret+"last\n", redact.New(redact.Options{Environ: []string{"REVIEW_SECRET=" + secret}}))
	f.repo.WriteFile("evidence.txt", "before\n"+secret+"changed\n")
	f.repo.Commit("Change after multiline secret")
	page, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_diff", Branch: "task", Path: "evidence.txt", Mode: "base_to_task", Limit: 50})
	mustReview(t, err)
	if len(page.Items) != 0 || len(page.Gaps) == 0 {
		t.Fatalf("diff hunk disclosed clipped secret: %+v", page)
	}
	b, err := json.Marshal(page)
	mustReview(t, err)
	if strings.Contains(string(b), "secret-key-line") {
		t.Fatal("diff private-key fragment survived")
	}
}

func TestProviderUsesRetainedCodeHostOrNamesMissingPermalink(t *testing.T) {
	for _, scenario := range []struct{ remote, url string }{{"ssh://git@code.example/acme/code.git", "https://code.example/acme/code/blob/"}, {"https://mirror.example/unrelated/path.git", ""}} {
		t.Run(scenario.remote, func(t *testing.T) {
			f := newProviderFixture(t, "committed code\n", nil)
			f.repo.Git("remote", "set-url", "origin", scenario.remote)
			f.target.RemoteURL = scenario.remote
			page, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 50})
			mustReview(t, err)
			if len(page.Items) != 1 {
				t.Fatalf("source unavailable %+v", page)
			}
			link, _ := page.Items[0]["url"].(string)
			if scenario.url == "" {
				if link != "" || len(page.Gaps) == 0 {
					t.Fatal("invented a mirror permalink")
				}
			} else if !strings.HasPrefix(link, scenario.url) {
				t.Fatalf("wrong code host %q", link)
			}
		})
	}
}

func TestProviderConfiguredHostRootsArePrivateAndInvalidateViews(t *testing.T) {
	r := redact.New(redact.Options{Environ: []string{}})
	f := newProviderFixture(t, "/opt/custom-test-home/cache/private.txt\npublic code\n", r)
	binary, err := git.Resolve(t.Context())
	mustReview(t, err)
	now := func() time.Time { return time.Date(2026, time.October, 27, 11, 30, 0, 0, time.UTC) }
	provider := codeevidence.New(f.db, binary, r, now, "/opt/custom-test-home")
	request := connector.CodeEvidenceRequest{Operation: "repo_read", Branch: "task", Path: "evidence.txt", Limit: 50}
	page, err := provider.Evidence(t.Context(), f.target, request)
	mustReview(t, err)
	data, err := json.Marshal(page.Items)
	mustReview(t, err)
	if strings.Contains(string(data), "/opt/custom-test-home") {
		t.Fatal("configured host root disclosed")
	}
	changed := codeevidence.New(f.db, binary, r, now, "/opt/another-home")
	request.Pin = page.Pin
	if _, err = changed.Evidence(t.Context(), f.target, request); err == nil {
		t.Fatal("changed host-root policy continued an old view")
	}
}

func TestProviderSearchRetainsGitExpressionAndLiteralPath(t *testing.T) {
	f := newProviderFixture(t, "not a match\n", nil)
	f.repo.WriteFile("colon:é.txt", "echo echo\n")
	f.repo.Commit("Add expression evidence")
	page, err := f.provider.Evidence(t.Context(), f.target, connector.CodeEvidenceRequest{Operation: "repo_grep", Branch: "task", Pattern: `(echo) \1|echo echo`, Limit: 50})
	mustReview(t, err)
	if len(page.Items) != 1 || page.Items[0]["path"] != "colon:é.txt" || page.Items[0]["content"] != "echo echo" {
		t.Fatalf("Git expression or literal path lost: %+v", page)
	}
}
