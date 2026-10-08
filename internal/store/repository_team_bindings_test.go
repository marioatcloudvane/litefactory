package store

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestRepositoryTeamBindingLifecycle(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "binding")
	ctx := t.Context()
	b := RepositoryTeamBinding{RepoID: repo.ID, Source: "https://example.com/catalogue.json", Slug: "standard", Version: "1.0.0", Kind: "fetched", ReleaseDigest: strings.Repeat("a", 64)}
	if _, err := db.GetRepositoryTeamBinding(ctx, repo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if changed, err := db.ReplaceRepositoryTeamBinding(ctx, b); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := db.ReplaceRepositoryTeamBinding(ctx, b); err != nil || changed {
		t.Fatal(changed, err)
	}
	repo.BaseBranch = "other"
	if _, err := db.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetRepositoryTeamBinding(ctx, repo.ID); err != nil || got != b {
		t.Fatal(got, err)
	}
	invalid := b
	invalid.ReleaseDigest = "invalid"
	if _, err := db.ReplaceRepositoryTeamBinding(ctx, invalid); err == nil {
		t.Fatal("invalid binding accepted")
	}
	if got, err := db.GetRepositoryTeamBinding(ctx, repo.ID); err != nil || got != b {
		t.Fatal("failed replacement changed binding", got, err)
	}
	b.Source = "local"
	b.Kind = "local"
	b.ReleaseDigest = ""
	b.Version = "2.0.0"
	if changed, err := db.ReplaceRepositoryTeamBinding(ctx, b); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if prior, changed, err := db.DeleteRepositoryTeamBinding(ctx, repo.ID); err != nil || !changed || prior != b {
		t.Fatal(prior, changed, err)
	}
	if _, changed, err := db.DeleteRepositoryTeamBinding(ctx, repo.ID); err != nil || changed {
		t.Fatal(changed, err)
	}
	if _, _, err := db.DeleteRepositoryTeamBinding(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatal("unregistered unlink", err)
	}
	b.RepoID = 999999
	if changed, err := db.ReplaceRepositoryTeamBinding(ctx, b); err == nil || changed {
		t.Fatal("foreign key ignored", changed, err)
	}
}

func TestRepositoryTeamUsesExactIdentityAndCorruption(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repos := []Repo{testRepo(t, db, "a", "z"), testRepo(t, db, "a", "a"), testRepo(t, db, "a", "b")}
	b := RepositoryTeamBinding{Source: "https://example.com/catalogue.json", Slug: "standard", Version: "1.0.0", Kind: "fetched", ReleaseDigest: strings.Repeat("b", 64)}
	for i, r := range repos {
		next := b
		next.RepoID = r.ID
		if i == 2 {
			next.Source = "https://other.example/catalogue.json"
		}
		if _, err := db.ReplaceRepositoryTeamBinding(ctx, next); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{repos[0].Path, repos[1].Path}
	slices.Sort(want)
	got, err := db.RepositoryTeamUses(ctx, b.Source, b.Slug, b.Version)
	if err != nil || !slices.Equal(got, want) {
		t.Fatal(got, want, err)
	}
	if got, err := db.RepositoryTeamUses(ctx, b.Source, b.Slug, "2.0.0"); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	// Corrupt an unrelated row. Removal must fail closed, even for another version.
	if _, err := db.sql.ExecContext(ctx, `UPDATE repository_team_bindings SET version='bad' WHERE repo_id=?`, repos[2].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetRepositoryTeamBinding(ctx, repos[2].ID); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("corrupt state treated as absence", err)
	}
	if _, err := db.RepositoryTeamUses(ctx, b.Source, b.Slug, "2.0.0"); err == nil {
		t.Fatal("corrupt state allowed removal")
	}
}

func TestRepositoryTeamBindingRejectsInvalidIdentities(t *testing.T) {
	t.Parallel()
	base := RepositoryTeamBinding{RepoID: 1, Source: "https://example.com/catalogue.json", Slug: "standard", Version: "1.0.0", Kind: "fetched", ReleaseDigest: strings.Repeat("a", 64)}
	for _, tc := range []struct {
		name   string
		mutate func(*RepositoryTeamBinding)
	}{
		{"missing repository", func(b *RepositoryTeamBinding) { b.RepoID = 0 }},
		{"unsafe slug", func(b *RepositoryTeamBinding) { b.Slug = "../bad" }},
		{"version alias", func(b *RepositoryTeamBinding) { b.Version = "latest" }},
		{"leading zero", func(b *RepositoryTeamBinding) { b.Version = "1.0.0-01" }},
		{"credential source", func(b *RepositoryTeamBinding) { b.Source = "https://user@example.com/catalogue.json" }},
		{"query source", func(b *RepositoryTeamBinding) { b.Source += "?token=value" }},
		{"uppercase digest", func(b *RepositoryTeamBinding) { b.ReleaseDigest = strings.Repeat("A", 64) }},
		{"local digest", func(b *RepositoryTeamBinding) { b.Kind = "local"; b.Source = "local" }},
		{"unknown kind", func(b *RepositoryTeamBinding) { b.Kind = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := base
			tc.mutate(&b)
			if err := validateRepositoryTeamBinding(b); err == nil {
				t.Fatal("accepted invalid identity")
			}
		})
	}
}
