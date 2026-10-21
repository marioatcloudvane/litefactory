package store

import (
	"errors"
	"testing"
)

func TestPreventionRepositoryIdentityIsUniqueAndCaseInsensitive(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	first, err := db.UpsertRepo(ctx, Repo{Path: t.TempDir(), Forge: "github", Owner: "Mixed", Name: "Case"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.ResolvePreventionRepository(ctx, "mixed/case")
	if err != nil || got.ID != first.ID {
		t.Fatal(got, err)
	}
	if _, err = db.ResolvePreventionRepository(ctx, "other/repo"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = db.UpsertRepo(ctx, Repo{Path: t.TempDir(), Forge: "github", Owner: "mixed", Name: "case"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ResolvePreventionRepository(ctx, "Mixed/Case"); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("ambiguous source chosen", err)
	}

}
