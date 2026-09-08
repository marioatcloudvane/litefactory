package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestStoryProgressLockSharedAliasesAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "factory.db")
	a, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	alias := filepath.Join(dir, "alias.db")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	b, err := store.Open(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	locks := t.TempDir()
	release, err := a.LockStoryProgress(t.Context(), "github:owner/repo#1", locks)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.LockStoryProgress(t.Context(), "github:owner/repo#1", locks); !errors.Is(err, store.ErrStoryProgressBusy) {
		t.Fatalf("alias bypassed ownership: %v", err)
	}
	// A different parent's subordinate lock is independent.
	other, err := b.LockStoryProgress(t.Context(), "github:owner/repo#2", locks)
	if err != nil {
		t.Fatal(err)
	}
	if err := other(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	recovered, err := b.LockStoryProgress(t.Context(), "github:owner/repo#1", locks)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(locks)
	if err != nil {
		t.Fatal(err)
	}
	// Never follow a replaced lock file into another target.
	for _, e := range entries {
		name := filepath.Join(locks, e.Name())
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, name); err != nil {
			t.Fatal(err)
		}
	}
	if release, err := a.LockStoryProgress(t.Context(), "github:owner/repo#1", locks); err == nil {
		_ = release()
		t.Fatal("followed lock symlink")
	}
}
