package owneraction

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestActionHistorySurvivesRepositoryRenameWithoutRestoringEffects(t *testing.T) {
	for _, transport := range []string{"discord", "mcp"} {
		t.Run(transport, func(t *testing.T) {
			s, d, discord, at := actionServiceFixture(t)
			origin := discord
			if transport == "mcp" {
				origin = actionMCPPrincipal(t, s, discord, "original", "repository:actions")
			}
			proposal, err := s.Propose(t.Context(), origin, Invocation{Command: "diagnose"}, "historical-repository")
			if err != nil {
				t.Fatal(err)
			}
			if transport == "discord" {
				if err = s.Available(t.Context(), proposal.ID, "delivered-proposal"); err != nil {
					t.Fatal(err)
				}
			}
			repo, err := s.db.GetRepo(t.Context(), d.facts.RepositoryID)
			if err != nil {
				t.Fatal(err)
			}
			repo.Name = "renamed"
			if _, err = s.db.UpsertRepo(t.Context(), repo); err != nil {
				t.Fatal(err)
			}
			*at = at.Add(5 * time.Second)
			inspected, err := s.Inspect(t.Context(), discord, proposal.ID)
			if err != nil || inspected.Facts.RepositoryRef != "github:acme/web" || inspected.Facts.TargetID != proposal.Facts.TargetID || inspected.Facts.TargetRevision != proposal.Facts.TargetRevision {
				t.Fatalf("historical facts lost after rename: %+v %v", inspected, err)
			}
			page, err := s.History(t.Context(), discord, "", 10)
			if err != nil || len(page.Proposals) != 1 || page.Proposals[0].Facts.RepositoryRef != "github:acme/web" {
				t.Fatalf("historical repository disappeared: %+v %v", page, err)
			}
			if _, err = s.Confirm(t.Context(), origin, proposal.ID); !errors.Is(err, store.ErrConnectorInvalid) {
				t.Fatalf("renamed repository restored execution authority: %v", err)
			}
			if transport == "mcp" {
				if _, err = s.Inspect(t.Context(), origin, proposal.ID); !errors.Is(err, store.ErrConnectorScope) {
					t.Fatalf("MCP adopted renamed repository coverage: %v", err)
				}
				scoped, e := s.History(t.Context(), origin, "", 10)
				if e != nil || len(scoped.Proposals) != 0 {
					t.Fatalf("MCP history bypassed current repository coverage: %+v %v", scoped, e)
				}
			}
			cancelled, err := s.Cancel(t.Context(), discord, proposal.ID)
			if err != nil || cancelled.State != "cancelled" || cancelled.Facts.RepositoryRef != "github:acme/web" {
				t.Fatalf("current owner cannot cancel historical target: %+v %v", cancelled, err)
			}
			if _, err = s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("read or cancellation admitted an execution: %v", err)
			}
		})
	}
}
