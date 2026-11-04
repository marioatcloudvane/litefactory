package turn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/attach"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func testRef(t *testing.T) naming.Ref {
	t.Helper()
	return naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "api", Issue: 33}
}

// TestManifestLandsInTheWorktree is AC-7.3's mechanism: the manifest is a
// worktree file so that litefactory's end-of-run commit picks it up like any
// other change the turn made. If it were written anywhere else it would
// never be committed, and the audit trail would last exactly as long as the
// machine's temp directory.
func TestManifestLandsInTheWorktree(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	result := attach.Result{Records: []attach.Record{{
		Name:      "login screen",
		Source:    "https://github.com/user-attachments/assets/aaa",
		MediaType: "image/png",
		Bytes:     2048,
		File:      "01-login-screen.png",
		Outcome:   attach.OutcomeAccepted,
		Route:     attach.RouteFirstAttempt,
		At:        time.Now(),
	}}}

	writeAttachmentManifest(context.Background(), worktree, testRef(t), 7, result, discardLogger())

	path := filepath.Join(worktree, ".aiteam", attach.ManifestFile)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	for _, want := range []string{"login screen", "01-login-screen.png", "image/png", "run 0007"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("manifest does not mention %q:\n%s", want, body)
		}
	}
}

// TestNoManifestWithoutAttachments keeps every ordinary run from gaining an
// empty file it then commits. A turn that never looked at an attachment
// should leave no trace of the feature at all.
func TestNoManifestWithoutAttachments(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	writeAttachmentManifest(context.Background(), worktree, testRef(t), 1, attach.Result{}, discardLogger())

	if _, err := os.Stat(filepath.Join(worktree, ".aiteam")); !os.IsNotExist(err) {
		t.Errorf("a turn with no attachments created %s", filepath.Join(worktree, ".aiteam"))
	}
}

// TestAnUnwritableManifestDoesNotStopTheTurn covers the story's own framing:
// the attachments are already fetched and already named in the prompt by this
// point, so failing here would discard work that succeeded in order to report
// that the paperwork did not.
func TestAnUnwritableManifestDoesNotStopTheTurn(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	// A file where the directory needs to be: MkdirAll cannot proceed.
	if err := os.WriteFile(filepath.Join(worktree, ".aiteam"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed the obstruction: %v", err)
	}
	result := attach.Result{Records: []attach.Record{{
		Name: "shot", Source: "https://h/a", Outcome: attach.OutcomeAccepted, At: time.Now(),
	}}}

	// The assertion is that this returns at all rather than panicking or
	// propagating: writeAttachmentManifest has no error return by design.
	writeAttachmentManifest(context.Background(), worktree, testRef(t), 1, result, discardLogger())
}

// TestIngestionIsGatedOnPermissionAndCapability pins that both are required
// (US-5's gate and a configured fetcher), because either one alone silently
// fetching would be a permission bug rather than a visible failure.
func TestIngestionIsGatedOnPermissionAndCapability(t *testing.T) {
	t.Parallel()

	body := "![shot](https://github.com/user-attachments/assets/aaa)"
	req := Request{
		Issue:   Issue{Body: body},
		Trigger: Trigger{Body: body, CommentID: 1},
	}

	t.Run("no permission", func(t *testing.T) {
		t.Parallel()
		req := req
		req.AttachmentsAllowed = false
		opts := Options{AssetFetcher: refusingFetcher{t}}
		if got := ingestAttachments(context.Background(), opts, req, testRef(t), 1, discardLogger()); !got.Empty() {
			t.Errorf("ingested without permission: %+v", got)
		}
	})

	t.Run("no fetcher", func(t *testing.T) {
		t.Parallel()
		req := req
		req.AttachmentsAllowed = true
		if got := ingestAttachments(context.Background(), Options{}, req, testRef(t), 1, discardLogger()); !got.Empty() {
			t.Errorf("ingested without a fetcher: %+v", got)
		}
	})
}

// refusingFetcher fails the test if it is ever called.
type refusingFetcher struct{ t *testing.T }

func (f refusingFetcher) FetchAsset(context.Context, string) (io.ReadCloser, string, error) {
	f.t.Error("the fetcher was called for a turn that may not ingest attachments")
	return nil, "", nil
}

// linearUploadTransport plays uploads.linear.app (and any other host) for a
// real linear.Client, recording every request so a test can count dials.
type linearUploadTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	respond  func(req *http.Request, attempt int) *http.Response
}

func (l *linearUploadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	l.mu.Lock()
	l.requests = append(l.requests, req)
	attempt := 0
	for _, seen := range l.requests {
		if seen.URL.String() == req.URL.String() {
			attempt++
		}
	}
	l.mu.Unlock()
	return l.respond(req, attempt), nil
}

func (l *linearUploadTransport) count(host string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, req := range l.requests {
		if req.URL.Hostname() == host {
			n++
		}
	}
	return n
}

func uploadResponse(status int, payload []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(payload))}
}

var (
	linearPNG  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 2040)...)
	linearHEIC = append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), bytes.Repeat([]byte{0}, 100)...)
)

func linearFetcher(t *testing.T, transport *linearUploadTransport) *linear.Client {
	t.Helper()
	client, err := linear.New(linear.Config{Origin: "https://api.linear.app", WorkspaceID: "w", TeamID: "t", ProjectID: "p", AuthKind: "api_key"},
		secrets.NewToken("lin_api_test"), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type linearTurn struct {
	result  attach.Result
	section string
	dir     string
}

func runLinearTurn(t *testing.T, fetcher attach.Fetcher, trigger, issue string) linearTurn {
	t.Helper()
	home := t.TempDir()
	opts := Options{Paths: config.NewPaths(home), AssetFetcher: fetcher}
	req := Request{AttachmentsAllowed: true, Issue: Issue{Body: issue}, Trigger: Trigger{Body: trigger, CommentID: 1}}
	result := ingestAttachments(context.Background(), opts, req, testRef(t), 1, discardLogger())
	dir, err := naming.AttachmentsDir(home, testRef(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	return linearTurn{result: result, section: attachmentsPrompt(result), dir: dir}
}

func landed(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestLinearUploadLandsAndIsNamedToTheAgent is C1: the MAR-81 regression.
func TestLinearUploadLandsAndIsNamedToTheAgent(t *testing.T) {
	t.Parallel()
	transport := &linearUploadTransport{respond: func(req *http.Request, _ int) *http.Response {
		if req.URL.Hostname() != "uploads.linear.app" || req.Header.Get("Authorization") != "lin_api_test" {
			return uploadResponse(http.StatusUnauthorized, nil)
		}
		return uploadResponse(http.StatusOK, linearPNG)
	}}
	got := runLinearTurn(t, linearFetcher(t, transport), "![shot](https://uploads.linear.app/org/upload-1/shot.png)", "")

	if len(got.result.Accepted) != 1 || len(got.result.Records) != 1 || got.result.Records[0].Outcome != attach.OutcomeAccepted {
		t.Fatalf("result = %+v, want one accepted file and one accepted record", got.result)
	}
	att := got.result.Accepted[0]
	if !strings.Contains(got.section, att.Path) || !strings.Contains(got.section, "image/png") || !strings.Contains(got.section, "2048 bytes") {
		t.Errorf("prompt section does not name the PNG with its size:\n%s", got.section)
	}
	if files := landed(t, got.dir); len(files) != 1 || filepath.Join(got.dir, files[0]) != att.Path {
		t.Errorf("attachments directory holds %v, want the landed PNG", files)
	}
	worktree := t.TempDir()
	writeAttachmentManifest(context.Background(), worktree, testRef(t), 1, got.result, discardLogger())
	manifest, err := os.ReadFile(filepath.Join(worktree, ".aiteam", attach.ManifestFile))
	if err != nil || !strings.Contains(string(manifest), "accepted") {
		t.Errorf("manifest = %q, %v", manifest, err)
	}
}

// TestFailingLinearUploadIsAFetchFailedRejection is C3.
func TestFailingLinearUploadIsAFetchFailedRejection(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		transport := &linearUploadTransport{respond: func(*http.Request, int) *http.Response { return uploadResponse(status, nil) }}
		got := runLinearTurn(t, linearFetcher(t, transport), "![shot](https://uploads.linear.app/org/upload-1/shot.png)", "")
		if n := transport.count("uploads.linear.app"); n != 2 {
			t.Errorf("status %d: %d requests, want 2 (one retry)", status, n)
		}
		if files := landed(t, got.dir); len(files) != 0 {
			t.Errorf("status %d: left %v behind", status, files)
		}
		if len(got.result.Records) != 1 || got.result.Records[0].Outcome != attach.OutcomeRejected || got.result.Records[0].Reason != attach.ReasonFetchFailed {
			t.Errorf("status %d: records = %+v", status, got.result.Records)
		}
		if len(got.result.Rejected) != 1 || strings.Count(got.section, got.result.Rejected[0].Line()) != 1 {
			t.Errorf("status %d: prompt section does not carry one rejection line:\n%s", status, got.section)
		}
	}

	transport := &linearUploadTransport{respond: func(_ *http.Request, attempt int) *http.Response {
		if attempt == 1 {
			return uploadResponse(http.StatusUnauthorized, nil)
		}
		return uploadResponse(http.StatusOK, linearPNG)
	}}
	got := runLinearTurn(t, linearFetcher(t, transport), "![shot](https://uploads.linear.app/org/upload-1/shot.png)", "")
	if len(got.result.Accepted) != 1 || len(got.result.Records) != 1 || got.result.Records[0].Route != attach.RouteRetry {
		t.Errorf("401 then 200: result = %+v", got.result)
	}
}

// TestUnsupportedLinearUploadIsNamedToTheAgent is C4: a HEIC upload is named
// as unsupported instead of being left as a raw link the agent then tries.
func TestUnsupportedLinearUploadIsNamedToTheAgent(t *testing.T) {
	t.Parallel()
	transport := &linearUploadTransport{respond: func(*http.Request, int) *http.Response { return uploadResponse(http.StatusOK, linearHEIC) }}
	got := runLinearTurn(t, linearFetcher(t, transport), "[IMG_0001.heic](https://uploads.linear.app/org/upload-2/IMG_0001.heic)", "")

	if files := landed(t, got.dir); len(files) != 0 {
		t.Errorf("left %v behind", files)
	}
	if len(got.result.Records) != 1 || got.result.Records[0].Reason != attach.ReasonUnsupportedType {
		t.Fatalf("records = %+v, want one unsupported-type rejection", got.result.Records)
	}
	if !strings.Contains(got.section, "IMG_0001.heic") || !strings.Contains(got.section, attach.ReasonUnsupportedType.ReasonText()) {
		t.Errorf("prompt section does not name the HEIC as unsupported:\n%s", got.section)
	}
}

// TestLinearPartialSuccessAndDedup is C5.
func TestLinearPartialSuccessAndDedup(t *testing.T) {
	t.Parallel()
	const pngURL = "https://uploads.linear.app/org/upload-1/shot.png"
	const heicURL = "https://uploads.linear.app/org/upload-2/IMG_0001.heic"
	transport := &linearUploadTransport{respond: func(req *http.Request, _ int) *http.Response {
		if strings.HasSuffix(req.URL.Path, ".heic") {
			return uploadResponse(http.StatusOK, linearHEIC)
		}
		return uploadResponse(http.StatusOK, linearPNG)
	}}
	got := runLinearTurn(t, linearFetcher(t, transport), "![shot]("+pngURL+")\n[IMG_0001.heic]("+heicURL+")", "![again]("+pngURL+")")

	if len(got.result.Accepted) != 1 || len(got.result.Rejected) != 1 || len(got.result.Records) != 2 {
		t.Fatalf("result = %+v, want one accepted, one rejected, two records", got.result)
	}
	if got.result.Records[0].Outcome != attach.OutcomeAccepted || got.result.Records[1].Outcome != attach.OutcomeRejected {
		t.Errorf("records are not in body order: %+v", got.result.Records)
	}
	if strings.Index(got.section, got.result.Accepted[0].Path) > strings.Index(got.section, got.result.Rejected[0].Line()) {
		t.Errorf("section is not in body order:\n%s", got.section)
	}
	if n := transport.count("uploads.linear.app"); n != 2 {
		t.Errorf("%d upload requests, want one per distinct URL", n)
	}
	if files := landed(t, got.dir); len(files) != 1 {
		t.Errorf("landed %v, want one file", files)
	}
}

// TestNoAttachmentsLeavesThePromptSectionEmpty pins AC-3.3 through the turn's
// own composition of the section.
func TestNoAttachmentsLeavesThePromptSectionEmpty(t *testing.T) {
	t.Parallel()
	if got := attachmentsPrompt(attach.Result{}); got != "" {
		t.Errorf("attachmentsPrompt(empty) = %q", got)
	}
}

// githubOnlyFetcher stands in for the GitHub client's allow-list: it refuses
// every non-GitHub host before any dial, the way github.FetchAsset does.
type githubOnlyFetcher struct{ dialed *int }

func (f githubOnlyFetcher) FetchAsset(_ context.Context, rawURL string) (io.ReadCloser, string, error) {
	if !strings.HasPrefix(rawURL, "https://github.com/") {
		return nil, "", errors.New("asset host not allowed")
	}
	*f.dialed++
	return nil, "", errors.New("unexpected")
}

// TestGitHubTurnReportsALinearUploadOnlyAsARejection is C6's turn half.
func TestGitHubTurnReportsALinearUploadOnlyAsARejection(t *testing.T) {
	t.Parallel()
	dialed := 0
	got := runLinearTurn(t, githubOnlyFetcher{&dialed}, "![shot](https://uploads.linear.app/org/upload-1/shot.png)", "")
	if dialed != 0 || len(got.result.Accepted) != 0 || len(got.result.Rejected) != 1 {
		t.Errorf("dialed %d, result %+v", dialed, got.result)
	}
	if !strings.Contains(got.section, got.result.Rejected[0].Line()) {
		t.Errorf("section does not name the refused upload:\n%s", got.section)
	}
}
