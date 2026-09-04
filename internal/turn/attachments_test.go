package turn

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/attach"
	"github.com/cloudvane/litefactory/internal/naming"
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
