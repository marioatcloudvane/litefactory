package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// abBundle writes <root>/<rel>/run-1/bootstrap.md and dates every entry at.
func abBundle(t *testing.T, root, rel string, at time.Time) (dir, digest string) {
	t.Helper()
	dir = filepath.Join(root, filepath.FromSlash(rel))
	run := filepath.Join(dir, "run-1")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("bootstrap " + rel)
	if err := os.WriteFile(filepath.Join(run, "bootstrap.md"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(run, "bootstrap.md"), run, dir} {
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(body)
	return dir, hex.EncodeToString(sum[:])
}

func abExists(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Lstat(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

type abFailingTasks struct{}

func (abFailingTasks) TaskRefInUse(context.Context, string) (bool, error) {
	return false, errors.New("database is locked")
}

func TestArchiveBundlesRemovesOnlyForgottenTasks(t *testing.T) {
	x := newArchiveITFixture(t)
	root := filepath.Join(t.TempDir(), "bundles")
	old, young := x.now.Add(-48*time.Hour), x.now.Add(-time.Hour)

	orphan, _ := abBundle(t, root, "github/o/r/7", old)
	live, _ := abBundle(t, root, "github/acme/api/213", old)
	recent, _ := abBundle(t, root, "github/young/r/9", young)
	qualified, _ := abBundle(t, root, "github/q/r/5", old)
	stamp := store.FormatTime(x.now)
	x.exec(t, `INSERT INTO sessions(repo_id,issue_number,task_ref,state,created_at,updated_at) VALUES(?,?,?,?,?,?)`, x.repo.ID, 5, "github:q/r#5~abcdef0123456789", store.SessionIdle, stamp, stamp)
	if err := os.WriteFile(filepath.Join(root, "github", "stray.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "github", "o", "r", "not-an-issue"), 0o700); err != nil {
		t.Fatal(err)
	}

	pass := &ArchivePass{Sweep: &store.ArchiveSweep{}, Now: x.now}
	if err := NewArchiveBundlesStep(ArchiveBundlesOptions{Root: root, Tasks: x.db}).Run(t.Context(), pass); err != nil {
		t.Fatal(err)
	}
	if abExists(t, orphan) {
		t.Error("bundle of a task without a sessions row survived")
	}
	for name, p := range map[string]string{"live task": live, "young orphan": recent, "qualified task": qualified} {
		if !abExists(t, p) {
			t.Errorf("%s bundle removed", name)
		}
	}
	if !abExists(t, filepath.Join(root, "github", "o", "r", "not-an-issue")) {
		t.Error("folder that names no issue was touched")
	}
	if pass.Sweep.Counts[ArchiveBundlesRemoved] != 1 || pass.Sweep.Counts[ArchiveBundlesFailures] != 0 {
		t.Errorf("counts = %v", pass.Sweep.Counts)
	}

	// Once the only issue folder below them is gone, empty parents are pruned
	// and the root itself stays.
	if err := os.RemoveAll(filepath.Join(root, "github", "o", "r", "not-an-issue")); err != nil {
		t.Fatal(err)
	}
	gone, _ := abBundle(t, root, "github/o/r/8", old)
	if err := NewArchiveBundlesStep(ArchiveBundlesOptions{Root: root, Tasks: x.db}).Run(t.Context(), pass); err != nil {
		t.Fatal(err)
	}
	if abExists(t, gone) || abExists(t, filepath.Join(root, "github", "o")) {
		t.Error("orphan bundle or its empty parents survived")
	}
	if !abExists(t, root) || !abExists(t, filepath.Join(root, "github")) {
		t.Error("root or a non-empty parent was removed")
	}
}

func TestArchiveBundlesLookupFailureKeepsFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bundles")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	dir, _ := abBundle(t, root, "github/o/r/7", now.Add(-48*time.Hour))
	pass := &ArchivePass{Sweep: &store.ArchiveSweep{}, Now: now}
	if err := NewArchiveBundlesStep(ArchiveBundlesOptions{Root: root, Tasks: abFailingTasks{}}).Run(t.Context(), pass); err != nil {
		t.Fatal(err)
	}
	if !abExists(t, dir) {
		t.Fatal("folder removed although the lookup failed")
	}
	if pass.Sweep.Counts[ArchiveBundlesFailures] != 1 || pass.Sweep.Counts[ArchiveBundlesRemoved] != 0 {
		t.Fatalf("counts = %v", pass.Sweep.Counts)
	}
	// A missing root is not an error.
	if err := NewArchiveBundlesStep(ArchiveBundlesOptions{Root: filepath.Join(t.TempDir(), "absent"), Tasks: abFailingTasks{}}).Run(t.Context(), pass); err != nil {
		t.Fatalf("missing root: %v", err)
	}
}

func TestArchiveBundlesKeptWhileArchivedRemovedAfterForget(t *testing.T) {
	f := newPlanForgetFixture(t)
	root := filepath.Join(t.TempDir(), "bundles")
	f.d.archive.StartSteps = []ArchiveStep{
		NewArchiveExpiryStep(ArchiveExpiryOptions{Runs: f.db, RunsDir: f.runs, RetainDays: aeRetain}),
		NewArchiveBundlesStep(ArchiveBundlesOptions{Root: root, Tasks: f.db}),
	}
	dir, digest := abBundle(t, root, "github/acme/api/213", f.now.Add(-60*24*time.Hour))
	ref, err := naming.ParseRef(f.parent.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	sessionRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.parent.ID, store.FirstSegmentOrdinal, sessionRef, store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(dir, "run-1")
	if err := f.db.SetSegmentDelivery(t.Context(), segment.ID, "identity-1", runRoot); err != nil {
		t.Fatal(err)
	}

	// Archived but not forgotten: the skeleton keeps the bundle usable.
	f.expectKept(t, ForgetReasonArchive)
	identity, stored, err := f.db.SegmentDelivery(t.Context(), segment.ID)
	if err != nil || stored != runRoot {
		t.Fatalf("stored delivery %q %q %v", identity, stored, err)
	}
	if err := agent.VerifyDelivery(t.Context(), agent.Delivery{Root: stored, Identity: identity, RenderedFiles: map[string]string{"bootstrap.md": digest}}); err != nil {
		t.Fatalf("archived task cannot run on its retained bundle: %v", err)
	}

	// The archive is gone, so this pass forgets the family; the bundle step
	// ran before that at pass start and still kept the folder.
	if err := os.RemoveAll(f.taskDir()); err != nil {
		t.Fatal(err)
	}
	if v := f.pass(t); v["outcome"] != ArchiveOutcomeForgotten {
		t.Fatalf("visit %v, want forgotten", v)
	}
	if !abExists(t, dir) {
		t.Fatal("bundle removed in the same pass that forgot the family")
	}
	f.pass(t)
	if abExists(t, dir) || abExists(t, filepath.Join(root, "github", "acme")) {
		t.Fatal("bundle of the forgotten task survived the next pass")
	}
	if s := f.sweep(t); s.Counts[ArchiveBundlesRemoved] != 1 {
		t.Fatalf("persisted sweep counts = %v", s.Counts)
	}
}
