package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestNeutralPlanCreationRetainsPartialAndUncertainEffects(t *testing.T) {
	for _, phase := range []string{"create", "attach", "unobservable"} {
		t.Run(phase, func(t *testing.T) {
			h, f, a, r, restart := planBoundarySetup(t)
			client, ok := h.Forge.(*github.Client)
			if !ok {
				t.Fatal("fixture client missing")
			}
			tickets, err := github.NewTickets(client)
			if err != nil {
				t.Fatal(err)
			}
			bridge := &TicketPlanBridge{DB: h.DB, Tickets: tickets}
			h.Forge = bridge
			f.failCreate = phase != "attach"
			f.failAttach = phase == "attach"
			planApplySharedFailure(t, h, a, r)
			restart()
			bridge.DB = h.DB
			if phase == "unobservable" {
				f.hidden = true
				planApply(t, h, a, r, "uncertain")
				if f.creates != 1 {
					t.Fatal("uncertain create duplicated")
				}
				f.hidden = false
			}
			got := planApply(t, h, a, r, "applied")
			if got.IssueID != 4000 || got.IssueNumber != 400 || f.creates != 1 || !f.attached {
				t.Fatalf("partial creation lost %+v", got)
			}
			restart()
			bridge.DB = h.DB
			planApply(t, h, a, r, "applied")
			if f.creates != 1 {
				t.Fatal("confirmed create duplicated")
			}
		})
	}
}

type foreignPlanOrigin struct{ PlanTickets }

func (f foreignPlanOrigin) Origin() ticket.Origin { return "https://another.example/api" }
func TestNeutralPlanBindingRejectsOriginAndExpectedContentChanges(t *testing.T) {
	h, f, a, r, _ := planBoundarySetup(t)
	client, ok := h.Forge.(*github.Client)
	if !ok {
		t.Fatal("fixture client missing")
	}
	tickets, err := github.NewTickets(client)
	if err != nil {
		t.Fatal(err)
	}
	bridge := &TicketPlanBridge{DB: h.DB, Tickets: tickets}
	h.Forge = bridge
	got := planApply(t, h, a, r, "applied")
	oldTitle, titleOK := f.child["title"].(string)
	oldBody, bodyOK := f.child["body"].(string)
	if !titleOK || !bodyOK {
		t.Fatal("fixture content missing")
	}
	expected := PlanIssueRevision(oldTitle, oldBody)
	f.child["body"] = oldBody + "\nowner edit"
	title, err := h.Redactor.Apply(t.Context(), "replacement", 256)
	if err != nil {
		t.Fatal(err)
	}
	body, err := h.Redactor.Apply(t.Context(), "replacement", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bridge.UpdateIssueExpected(t.Context(), a.Repo, got.IssueNumber, expected, title, body); err == nil {
		t.Fatal("owner edit overwritten")
	}
	if f.patches != 0 {
		t.Fatal("conflicting content dispatched")
	}
	originalKey := planDestination(bridge.locator(a.Repo, a.ParentNumber))
	originalBinding, err := h.DB.GetTicketEffect(t.Context(), originalKey)
	if err != nil {
		t.Fatal(err)
	}
	bridge.Tickets = foreignPlanOrigin{tickets}
	_, err = bridge.GetIssue(t.Context(), a.Repo, a.ParentNumber)
	var failure *ticket.Failure
	if !errors.As(err, &failure) || failure.Kind != ticket.Refused || failure.Operation != "address" {
		t.Fatalf("origin rebound: %v", err)
	}
	afterBinding, err := h.DB.GetTicketEffect(t.Context(), originalKey)
	if err != nil || afterBinding != originalBinding {
		t.Fatalf("foreign origin changed historical metadata: %+v %v", afterBinding, err)
	}
}

func TestPlanPartUncertainDispatchDoesNotResend(t *testing.T) {
	h, f, a, r, restart := planBoundarySetup(t)
	r.Draft.Problem = strings.Repeat("x", 100000)
	f.failComment = true
	got, err := h.Apply(t.Context(), a, r)
	if err == nil || got.Outcome != "uncertain" {
		t.Fatalf("lost response %+v %v", got, err)
	}
	restart()
	planApply(t, h, a, r, "uncertain")
	if len(f.comments) != 0 || f.creates != 1 {
		t.Fatal("uncertain part was duplicated")
	}
}

type substitutedPlanSnapshot struct {
	PlanTickets
	snapshot ticket.Snapshot
}

func (f substitutedPlanSnapshot) Resolve(context.Context, ticket.Locator) (ticket.Snapshot, error) {
	return f.snapshot, nil
}
func (f substitutedPlanSnapshot) Read(context.Context, ticket.Ref) (ticket.Snapshot, error) {
	return f.snapshot, nil
}

func TestNeutralPlanRejectsSubstitutedSnapshotBeforeBinding(t *testing.T) {
	h, _, a, _, _ := planBoundarySetup(t)
	client, ok := h.Forge.(*github.Client)
	if !ok {
		t.Fatal("fixture client missing")
	}
	tickets, err := github.NewTickets(client)
	if err != nil {
		t.Fatal(err)
	}
	original, err := tickets.Resolve(t.Context(), ticket.Locator{Provider: ticket.GitHub, Origin: tickets.Origin(), Container: a.Repo.String(), Key: "362"})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"origin", "locator", "identity"} {
		t.Run(change, func(t *testing.T) {
			s := original
			switch change {
			case "origin":
				s.Ref.Identity.Origin = "https://foreign.example"
				s.Ref.Locator.Origin = s.Ref.Identity.Origin
			case "locator":
				s.Ref.Locator.Key = "999"
			case "identity":
				s.Ref.Identity.RemoteID = "9990"
			}
			bridge := &TicketPlanBridge{DB: h.DB, Tickets: substitutedPlanSnapshot{PlanTickets: tickets, snapshot: s}}
			if change == "origin" {
				if _, err := bridge.bind(t.Context(), s); !errors.Is(err, store.ErrTicketIdentityConflict) {
					t.Fatalf("foreign snapshot bound: %v", err)
				}
				if _, err := h.DB.GetTicketEffect(t.Context(), planDestination(s.Ref.Locator)); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("foreign snapshot metadata created: %v", err)
				}
			}
			if change == "identity" {
				if _, err := bridge.DB.BindTicketEffect(t.Context(), planDestination(original.Ref.Locator), original.Ref.Locator, ticket.Identity{}); err != nil {
					t.Fatal(err)
				}
				if _, err := bridge.DB.ResolveTicketEffect(t.Context(), planDestination(original.Ref.Locator), original.Ref); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := bridge.GetIssue(t.Context(), a.Repo, a.ParentNumber); !errors.Is(err, store.ErrTicketIdentityConflict) {
				t.Fatalf("substituted snapshot accepted: %v", err)
			}
			session, err := h.DB.GetSessionByTaskRef(t.Context(), a.Parent)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := h.DB.GetSessionTicket(t.Context(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if binding.RemoteID != "" {
				t.Fatalf("substituted session identity retained %+v", binding)
			}
			if change == "locator" {
				if _, err := h.DB.GetTicketEffect(t.Context(), planDestination(s.Ref.Locator)); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("foreign metadata created: %v", err)
				}
			}
		})
	}
}

func TestLegacyPlanBatchRefusesWithoutRemoteIO(t *testing.T) {
	bridge := &TicketPlanBridge{}
	_, err := bridge.CreateSubIssues(t.Context(), github.Repo{Owner: "acme", Name: "app"}, 362, []github.SubIssueSpec{{StoryID: "story"}})
	var failure *ticket.Failure
	if !errors.As(err, &failure) || failure.Kind != ticket.Unsupported {
		t.Fatalf("batch bypass was not refused: %v", err)
	}
}
