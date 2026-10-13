package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// The adapter tests exercise GraphQL. This fake exercises the bridge and graph
// ledger with actual provider bodies, including a response lost after mutation.
type normalizedPlanTickets struct {
	PlanTickets
	parent, child        ticket.Snapshot
	updates              int
	attached, loseUpdate bool
}

func (*normalizedPlanTickets) Provider() ticket.Provider { return ticket.Linear }
func (*normalizedPlanTickets) Origin() ticket.Origin     { return "https://linear.example.test" }
func (f *normalizedPlanTickets) snapshot(id, title, body string) ticket.Snapshot {
	identity := ticket.Identity{Provider: ticket.Linear, Origin: f.Origin(), RemoteID: id}
	return ticket.Snapshot{Ref: ticket.Ref{Identity: identity, Locator: ticket.Locator{Provider: ticket.Linear, Origin: f.Origin(), Container: "team/project", Key: id}}, Title: title, Body: body, ProviderBody: linear.EncodeContent(body), Revision: PlanIssueRevision(title, linear.EncodeContent(body)), State: "open", Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.Linear, Origin: f.Origin(), RemoteID: "bot"}}}
}
func (f *normalizedPlanTickets) Resolve(_ context.Context, locator ticket.Locator) (ticket.Snapshot, error) {
	if locator == f.parent.Ref.Locator {
		return f.parent, nil
	}
	if locator == f.child.Ref.Locator {
		return f.child, nil
	}
	return ticket.Snapshot{}, errors.New("unknown fixture locator")
}
func (f *normalizedPlanTickets) Read(ctx context.Context, ref ticket.Ref) (ticket.Snapshot, error) {
	return f.Resolve(ctx, ref.Locator)
}
func (*normalizedPlanTickets) normalize(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line != "" && !strings.HasPrefix(line, "<!--") {
			lines[i] += " "
		}
	}
	return strings.Join(lines, "\n")
}
func (f *normalizedPlanTickets) Parent(_ context.Context, child ticket.Ref) (ticket.Snapshot, error) {
	if !f.attached || child != f.child.Ref {
		return ticket.Snapshot{}, github.ErrNotFound
	}
	return f.parent, nil
}
func (f *normalizedPlanTickets) UpdateContent(_ context.Context, ref ticket.Ref, revision string, title, body redact.Body) (ticket.Snapshot, error) {
	if ref != f.child.Ref || revision != f.child.Revision {
		return ticket.Snapshot{}, errors.New("stale raw content base")
	}
	f.updates++
	f.child = f.snapshot("child", title.String(), f.normalize(body.String()))
	if f.loseUpdate {
		f.loseUpdate = false
		return ticket.Snapshot{}, errors.New("lost response after mutation")
	}
	return f.child, nil
}

func TestPlanStoryNormalizedReadbackAndRecovery(t *testing.T) {
	h, _, a, r, restart := planBoundarySetup(t)
	remote := &normalizedPlanTickets{}
	remote.parent = remote.snapshot("parent", "Parent", "Parent body")
	bridge := &TicketPlanBridge{DB: h.DB, Tickets: remote}
	parent, err := bridge.bind(t.Context(), remote.parent)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := compatibilityIssue(t.Context(), h.DB, parent)
	if err != nil {
		t.Fatal(err)
	}
	a.ParentID, a.ParentNumber = legacy.ID, legacy.Number
	a.Repo = github.Repo{Owner: "team", Name: "project"}
	h.Forge = bridge
	// Promotion already created and attached the shell, as in the incident.
	remote.child = remote.snapshot("child", "Drafting", planStoryMarker(a, a.SelectedKey)+"\nDrafting")
	remote.attached = true
	child, err := compatibilityIssue(t.Context(), h.DB, remote.child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.DB.ReservePlanStory(t.Context(), a.Parent, a.SelectedKey); err != nil {
		t.Fatal(err)
	}
	if err = h.DB.BindPlanStoryShell(t.Context(), a.Parent, a.SelectedKey, child.ID, child.Number, PlanIssueRevision(child.Title, child.Body)); err != nil {
		t.Fatal(err)
	}
	created := planApply(t, h, a, r, "applied")
	checkRevision := func(revision string) {
		t.Helper()
		fetched, err := bridge.GetIssue(t.Context(), a.Repo, created.IssueNumber)
		if err != nil {
			t.Fatal(err)
		}
		draft, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
		if err != nil {
			t.Fatal(err)
		}
		if revision != PlanIssueRevision(fetched.Title, fetched.Body) || draft.Revision != revision {
			t.Fatal("stored revision is not the observed content revision")
		}
	}
	checkRevision(created.Revision)
	for i, name := range []string{"first", "lost-response", "next-base"} {
		r.Operation = "update"
		r.RequestID = name
		r.ExpectedRevision = created.Revision
		r.Candidate = "candidate-" + name
		a.Candidate = r.Candidate
		r.ChangedSections = []string{"intent"}
		r.Draft.Problem = "Explanation " + name
		if i == 1 {
			remote.loseUpdate = true
			if _, err := h.Apply(t.Context(), a, r); err == nil {
				t.Fatal("lost response was not reported")
			}
			restart()
			bridge.DB = h.DB
		}
		created = planApply(t, h, a, r, "applied")
		checkRevision(created.Revision)
		planApply(t, h, a, r, "applied")
		if remote.updates != i+2 {
			t.Fatal("replay duplicated mutation")
		}
	}
}
