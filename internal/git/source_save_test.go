package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
)

func TestSavedSourceReconcilesLostAcknowledgmentAndNoop(t *testing.T) {
	repo, w := sourceWorktree(t)
	ctx := t.Context()
	if e := os.WriteFile(filepath.Join(w.Path(), "tracked.txt"), []byte("validated repair\n"), 0600); e != nil {
		t.Fatal(e)
	}
	original, e := w.ObserveSource(ctx, git.SourceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	options := git.CommitOptions{Summary: "Repair approved empty input", Task: "github:acme/repo#893", Workflow: "bug", Run: 1, Author: git.Author{Name: "Fixture", Email: "fixture@example.com"}}
	first, e := w.SaveSource(ctx, original, git.SourceOptions{}, options)
	if e != nil {
		t.Fatal(e)
	}
	second, e := w.SaveSource(ctx, original, git.SourceOptions{}, options)
	if e != nil {
		t.Fatal(e)
	}
	if first.Head != second.Head || repo.GitIn(w.Path(), "rev-parse", "HEAD^") != original.Head {
		t.Fatal("lost acknowledgment duplicated save")
	}
	current, e := w.ObserveSource(ctx, git.SourceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	noop, e := w.SaveSource(ctx, current, git.SourceOptions{}, options)
	if e != nil {
		t.Fatal(e)
	}
	if noop.Head != first.Head || noop.Committed {
		t.Fatalf("noop=%+v", noop)
	}
	if e = os.WriteFile(filepath.Join(w.Path(), "unvalidated.txt"), []byte("different\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = w.SaveSource(ctx, original, git.SourceOptions{}, options); e == nil {
		t.Fatal("changed source reused tested save")
	}
}
