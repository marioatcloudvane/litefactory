package git_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func sourceWorktree(t *testing.T) (*gittest.Repo, *git.Worktree) {
	t.Helper()
	repo := gittest.New(t)
	repo.WriteFile("tracked.txt", "original\n")
	repo.WriteFile("deleted.txt", "delete me\n")
	baseline := repo.Commit("baseline")
	binary, e := git.Resolve(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	clone, e := binary.Open(t.Context(), repo.Dir)
	if e != nil {
		t.Fatal(e)
	}
	branch, dir := task(t, worktreeRoot(repo), 893, "source identity")
	result, e := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if e != nil {
		t.Fatal(e)
	}
	repo.GitIn(dir, "merge", "--ff-only", baseline)
	return repo, result.Worktree
}
func TestSourceIdentityDirtySavedModesDeletionAndIndexPreservation(t *testing.T) {
	repo, w := sourceWorktree(t)
	before, e := w.ObserveSource(t.Context(), git.SourceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(w.Path(), "tracked.txt"), []byte("repaired\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(w.Path(), "deleted.txt")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(w.Path(), "new.sh"), []byte("#!/bin/sh\nexit 0\n"), 0755); e != nil {
		t.Fatal(e)
	}
	index := repo.GitIn(w.Path(), "rev-parse", "--git-path", "index")
	raw, e := os.ReadFile(index)
	if e != nil {
		t.Fatal(e)
	}
	dirty, e := w.ObserveSource(t.Context(), git.SourceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	after, e := os.ReadFile(index)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(raw, after) {
		t.Fatal("source observation changed index")
	}
	if dirty.Head != before.Head || dirty.Digest == before.Digest {
		t.Fatal("dirty input not identified")
	}
	repo.GitIn(w.Path(), "add", "--all")
	repo.GitIn(w.Path(), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-m", "saved fixture")
	saved, e := w.ObserveSource(t.Context(), git.SourceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if saved.Digest != dirty.Digest || saved.Head == dirty.Head {
		t.Fatal("saved content identity differs from tested content")
	}
	if e = os.Chmod(filepath.Join(w.Path(), "new.sh"), 0644); e != nil {
		t.Fatal(e)
	}
	mode, e := w.ObserveSource(t.Context(), git.SourceOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if mode.Digest == saved.Digest {
		t.Fatal("executable mode omitted from identity")
	}
}
func TestSourceIdentityIgnoredInputsAndSymlinks(t *testing.T) {
	_, w := sourceWorktree(t)
	if e := os.WriteFile(filepath.Join(w.Path(), ".gitignore"), []byte("ignored.cfg\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(w.Path(), "ignored.cfg"), []byte("config one"), 0600); e != nil {
		t.Fatal(e)
	}
	opts := git.SourceOptions{IgnoredInputs: []string{"ignored.cfg"}}
	first, e := w.ObserveSource(t.Context(), opts)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(w.Path(), "ignored.cfg"), []byte("config two"), 0600); e != nil {
		t.Fatal(e)
	}
	second, e := w.ObserveSource(t.Context(), opts)
	if e != nil {
		t.Fatal(e)
	}
	if first.Digest == second.Digest {
		t.Fatal("declared ignored input mutation omitted")
	}
	if _, e = w.ObserveSource(t.Context(), git.SourceOptions{ExternalInputs: []string{"toolchain"}}); e == nil {
		t.Fatal("unknown external input certified")
	}
	if e = os.Symlink("tracked.txt", filepath.Join(w.Path(), "link")); e != nil {
		t.Fatal(e)
	}
	if _, e = w.ObserveSource(t.Context(), opts); e != nil {
		t.Fatal(e)
	}
}

func TestSourceIdentitySymlinkTargets(t *testing.T) {
	repo, w := sourceWorktree(t)
	if err := os.Mkdir(filepath.Join(w.Path(), "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"link": "nested/chain", "nested/chain": "../tracked.txt"} {
		if err := os.Symlink(target, filepath.Join(w.Path(), name)); err != nil {
			t.Fatal(err)
		}
	}
	dirty, err := w.ObserveSource(t.Context(), git.SourceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range dirty.Entries {
		if entry.Path != "link" && entry.Path != "nested/chain" {
			continue
		}
		target := "nested/chain"
		if entry.Path == "nested/chain" {
			target = "../tracked.txt"
		}
		sum := sha256.Sum256([]byte(target))
		if entry.Mode != "120000" || entry.Target != target || entry.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("entry %+v", entry)
		}
	}
	repo.GitIn(w.Path(), "add", "--all")
	repo.GitIn(w.Path(), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-m", "links")
	saved, err := w.ObserveSource(t.Context(), git.SourceOptions{})
	if err != nil || saved.Digest != dirty.Digest {
		t.Fatalf("saved %+v: %v", saved, err)
	}
	if err := os.Remove(filepath.Join(w.Path(), "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tracked.txt", filepath.Join(w.Path(), "link")); err != nil {
		t.Fatal(err)
	}
	changed, err := w.ObserveSource(t.Context(), git.SourceOptions{})
	if err != nil || changed.Digest == saved.Digest {
		t.Fatalf("target identity unchanged: %v", err)
	}
}

func TestSourceIdentityRecordsUnresolvedSymlinkTargets(t *testing.T) {
	for _, tc := range []struct{ name, target, intermediate string }{
		{"absolute", "/etc/passwd", ""},
		{"escape", "../outside", ""},
		{"chain", "hop", "../outside"},
		{"directory", "hop/file", "../outside"},
		{"absolute chain", "hop", "/etc"},
		{"dangling", "missing", ""},
		{"cycle", "link", ""},
		{"nondirectory", "tracked.txt/file", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := sourceWorktree(t)
			if tc.intermediate != "" {
				if err := os.Symlink(tc.intermediate, filepath.Join(w.Path(), "hop")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(tc.target, filepath.Join(w.Path(), "link")); err != nil {
				t.Fatal(err)
			}
			got, err := w.ObserveSource(t.Context(), git.SourceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"link": tc.target}
			if tc.intermediate != "" {
				want["hop"] = tc.intermediate
			}
			for _, entry := range got.Entries {
				target, ok := want[entry.Path]
				if !ok {
					continue
				}
				delete(want, entry.Path)
				sum := sha256.Sum256([]byte(target))
				if entry.Mode != "120000" || entry.Target != target || entry.SHA256 != hex.EncodeToString(sum[:]) {
					t.Fatalf("entry %+v", entry)
				}
			}
			if len(want) != 0 {
				t.Fatalf("missing symlink entries %v in %+v", want, got.Entries)
			}
		})
	}
}

func TestSourceIdentityRefusesSymlinkSourceParent(t *testing.T) {
	_, w := sourceWorktree(t)
	if err := os.Mkdir(filepath.Join(w.Path(), "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), "real", "file"), []byte("input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(w.Path(), "alias")); err != nil {
		t.Fatal(err)
	}
	_, err := w.ObserveSource(t.Context(), git.SourceOptions{IgnoredInputs: []string{"alias/file"}})
	var refusal *git.SourceSymlinkError
	if !errors.As(err, &refusal) || refusal.Path != "alias/file" || !strings.Contains(refusal.Reason, "symlink source parent") {
		t.Fatalf("got %v", err)
	}
}
