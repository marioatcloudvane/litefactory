package daemon

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// Legacy journal retirement must work even when no classifier or forge is
// available. In particular, bookkeeping must not restore an owner-removed
// label or attempt to delete a retained one.
func TestPersistentModeRetirementWithoutClassifiers(t *testing.T) {
	for _, startup := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			name := "drain"
			if startup {
				name = "startup"
			}
			if explicit {
				name += "/explicit"
			} else {
				name += "/inferred"
			}
			t.Run(name, func(t *testing.T) {
				db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				})
				if err := db.Migrate(t.Context()); err != nil {
					t.Fatal(err)
				}
				repo, err := db.UpsertRepo(t.Context(), store.Repo{
					Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "retirement",
					RemoteURL: "git@github.com:acme/retirement.git", BaseBranch: "main",
					FeaturePrefix: "lf", WorktreeRoot: t.TempDir(),
				})
				if err != nil {
					t.Fatal(err)
				}
				session, err := db.GetOrCreateSession(t.Context(), repo, 170)
				if err != nil {
					t.Fatal(err)
				}
				for _, trigger := range []int64{41, 42} {
					if _, err := db.GetOrCreateModeSelection(t.Context(), store.ModeSelection{
						SessionID: session.ID, TriggerCommentID: trigger, Mode: "plan", Label: "Mode: Plan", LabelApplied: true,
					}); err != nil {
						t.Fatal(err)
					}
				}
				if explicit {
					if err := db.MarkModeCleanupPending(t.Context(), session.ID, 41); err != nil {
						t.Fatal(err)
					}
				} else if _, err := db.AdvanceWatermark(t.Context(), session.ID, 41); err != nil {
					t.Fatal(err)
				}
				d := &Daemon{db: db, instanceID: "retirement", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
				if startup {
					_, err = d.Start(t.Context())
				} else {
					err = d.retireModeSelections(t.Context(), session.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.GetModeSelection(t.Context(), session.ID, 41); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("completed journal was not retired: %v", err)
				}
				if row, err := db.GetModeSelection(t.Context(), session.ID, 42); err != nil || row.Mode != "plan" || !row.LabelApplied {
					t.Fatalf("unfinished journal changed: %+v, %v", row, err)
				}
			})
		}
	}
}
