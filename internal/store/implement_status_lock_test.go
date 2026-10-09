package store_test

import (
	"github.com/cloudvane/litefactory/internal/testutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestImplementStatusLockIsIndependentAndSerializesAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "factory.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	alias := filepath.Join(dir, "alias.db")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	other, err := store.Open(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	})
	locks := t.TempDir()
	parent := "github:owner/repo#1"
	merge, err := db.LockStoryProgress(t.Context(), parent, locks)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.LockImplementStatus(t.Context(), parent, locks)
	if err != nil {
		t.Fatal(err)
	}
	if err := merge(); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	go func() {
		release, err := other.LockImplementStatus(t.Context(), parent, locks)
		if err == nil {
			err = release()
		}
		acquired <- err
	}()
	// While one parent's writer waits, another parent and merges are independent.
	independent, err := other.LockImplementStatus(t.Context(), "github:owner/repo#2", locks)
	if err != nil {
		t.Fatal(err)
	}
	if err := independent(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-acquired:
		t.Fatalf("second writer bypassed ownership: %v", err)
	default:
	}
	if err := first(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("status writer did not acquire released lock")
	}
}
