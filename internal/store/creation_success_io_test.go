package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// AC-007: source association and original proposal are one transaction; replay
// must neither replace a newer current proposal nor refresh retained timestamps.
func TestCreationSuccessDB_AtomicPreparationAndImmutableReplay(t *testing.T) {
	f := newCreationDBFixture(t)
	s, p := f.proposal(t, "api", 314, 1)
	source := CreationSource{Repository: p.Repository, SourceTask: p.SourceTask, CommentID: 70, OwnerID: 10}
	if _, err := f.db.sql.ExecContext(t.Context(), `CREATE TRIGGER fail_source BEFORE INSERT ON creation_preparations BEGIN SELECT RAISE(ABORT,'source unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.RetainProposalForSource(t.Context(), p, source); err == nil {
		t.Fatal("source retention fault succeeded")
	}
	for _, table := range []string{"creation_preparations", "mcp_creation_proposals", "mcp_creation_current"} {
		var count int
		if err := f.db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("partial admission persisted in %s", table)
		}
	}
	if _, err := f.db.sql.ExecContext(t.Context(), `DROP TRIGGER fail_source`); err != nil {
		t.Fatal(err)
	}
	original, err := f.c.RetainProposalForSource(t.Context(), p, source)
	if err != nil {
		t.Fatal(err)
	}
	var retained int64
	if err = f.db.sql.QueryRowContext(t.Context(), `SELECT retained_ns FROM mcp_creation_proposals WHERE hash=?`, p.Hash).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	newer := presentationProposal(t, s, p.Repository, "Newer proposal")
	if err = f.c.RetainProposal(t.Context(), newer); err != nil {
		t.Fatal(err)
	}
	hash, revision := currentRevision(t, f, p.SourceTask)
	f.now = f.now.Add(24 * time.Hour)
	replay, err := f.c.RetainProposalForSource(t.Context(), newer, source)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, original) {
		t.Fatal("replay replaced original content")
	}
	gotHash, gotRevision := currentRevision(t, f, p.SourceTask)
	if gotHash != hash || gotRevision != revision {
		t.Fatalf("replay changed current %s@%d to %s@%d", hash, revision, gotHash, gotRevision)
	}
	var after int64
	if err = f.db.sql.QueryRowContext(t.Context(), `SELECT retained_ns FROM mcp_creation_proposals WHERE hash=?`, p.Hash).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != retained {
		t.Fatal("replay refreshed retention")
	}
	for name, change := range map[string]func(*CreationSource){"owner": func(s *CreationSource) { s.OwnerID++ }, "repository": func(s *CreationSource) { s.Repository = "other/api" }, "task": func(s *CreationSource) { s.SourceTask = "github:owner/api#315" }, "comment": func(s *CreationSource) { s.CommentID++ }} {
		t.Run(name, func(t *testing.T) {
			bad := source
			change(&bad)
			if _, err := f.db.GetCreationPreparation(t.Context(), bad); !errors.Is(err, ErrNotFound) {
				t.Fatalf("foreign source recovered: %v", err)
			}
		})
	}
}

// AC-008: legacy rows need no source journal. Recovery is observation only;
// original identity, expiry, proposal revision and later binding checks survive.
func TestCreationSuccessDB_LegacyApprovalPreservesExpiryAndBinding(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "superseded"
		if expired {
			name = "expired"
		}
		t.Run(name, func(t *testing.T) {
			f := newCreationDBFixture(t)
			s, p := f.proposal(t, "api", 314, 1)
			if err := f.c.RetainProposal(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			id, err := f.c.AdmitOwnerIntent(t.Context(), CreationApproval{Repository: p.Repository, SourceTask: p.SourceTask, ProposalHash: p.Hash, CommentID: 70, OwnerID: 10, ActorID: 10, CreatedAt: f.now}, "linked")
			if err != nil {
				t.Fatal(err)
			}
			var created, expiry, revision int64
			if err = f.db.sql.QueryRowContext(t.Context(), `SELECT created_ns,expires_ns,proposal_revision FROM mcp_creation_intents WHERE id=?`, id).Scan(&created, &expiry, &revision); err != nil {
				t.Fatal(err)
			}
			if expired {
				f.now = f.now.Add(24 * time.Hour)
			} else {
				f.now = f.now.Add(time.Minute)
				if err = f.c.RetainProposal(t.Context(), presentationProposal(t, s, p.Repository, "Superseding")); err != nil {
					t.Fatal(err)
				}
			}
			source := CreationSource{Repository: p.Repository, SourceTask: p.SourceTask, CommentID: 70, OwnerID: 10}
			got, err := f.db.GetCreationOwnerIntent(t.Context(), source)
			if err != nil || got != id {
				t.Fatalf("legacy recovery %d %v", got, err)
			}
			var afterCreated, afterExpiry, afterRevision int64
			if err = f.db.sql.QueryRowContext(t.Context(), `SELECT created_ns,expires_ns,proposal_revision FROM mcp_creation_intents WHERE id=?`, id).Scan(&afterCreated, &afterExpiry, &afterRevision); err != nil {
				t.Fatal(err)
			}
			if created != afterCreated || expiry != afterExpiry || revision != afterRevision {
				t.Fatal("recovery changed original authority")
			}
			lease := f.lease(t, s, "recovery", "research", f.now.Add(time.Hour))
			if _, err = f.c.BindGrant(t.Context(), lease, id); !errors.Is(err, ErrCreationAuthority) {
				t.Fatalf("recovered stale admission bypassed grant checks: %v", err)
			}
			for name, change := range map[string]func(*CreationSource){"owner": func(s *CreationSource) { s.OwnerID++ }, "repository": func(s *CreationSource) { s.Repository = "other/api" }, "task": func(s *CreationSource) { s.SourceTask = "github:owner/api#315" }, "comment": func(s *CreationSource) { s.CommentID++ }} {
				t.Run(name, func(t *testing.T) {
					bad := source
					change(&bad)
					if _, err := f.db.GetCreationOwnerIntent(t.Context(), bad); !errors.Is(err, ErrNotFound) {
						t.Fatalf("foreign intent recovered: %v", err)
					}
				})
			}
			var count int
			if err = f.db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM mcp_creation_intents`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("intent count %d", count)
			}
			if err = f.db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM mcp_creation_grants`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("grant count %d", count)
			}
		})
	}
}
