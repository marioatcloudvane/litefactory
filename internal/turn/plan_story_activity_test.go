package turn

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"os"
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
	parentOverride       *ticket.Snapshot
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
	if f.parentOverride != nil {
		return *f.parentOverride, nil
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

func TestPlanStoryMAR12ConflictReconciliation(t *testing.T) {
	for _, variant := range []string{"confirmed", "prose", "title", "metadata", "closed", "parent", "missing authority", "missing intent", "ambiguous", "github", "failed", "ordinary apply"} {
		t.Run(variant, func(t *testing.T) {
			h, _, a, r, restart := planBoundarySetup(t)
			ctx := t.Context()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile("../linear/testdata/mar-12-readback.md")
			must(err)
			body, err := linear.DecodeContent(string(raw))
			must(err)
			_, payload, _ := strings.Cut(body, "<!-- lf:plan-content v=1 data=")
			payload, _, _ = strings.Cut(payload, " -->")
			must(json.Unmarshal([]byte(payload), &r.Draft))
			a.SelectedKey = r.Draft.Key
			r.RequestID = "plan-request-708-6832"
			remote := &normalizedPlanTickets{attached: true}
			remote.parent = remote.snapshot("parent", "Parent", "Parent body")
			bridge := &TicketPlanBridge{DB: h.DB, Tickets: remote}
			parent, err := bridge.bind(ctx, remote.parent)
			must(err)
			legacy, err := compatibilityIssue(ctx, h.DB, parent)
			must(err)
			a.ParentID, a.ParentNumber = legacy.ID, legacy.Number
			a.Repo = github.Repo{Owner: "team", Name: "project"}
			h.Forge = bridge
			remote.child = remote.snapshot("child", "Drafting", planStoryMarker(a, a.SelectedKey)+"\nDrafting")
			child, err := compatibilityIssue(ctx, h.DB, remote.child)
			must(err)
			d, err := h.DB.ReservePlanStory(ctx, a.Parent, a.SelectedKey)
			must(err)
			shellRevision := PlanIssueRevision(child.Title, child.Body)
			must(h.DB.BindPlanStoryShell(ctx, a.Parent, a.SelectedKey, child.ID, child.Number, shellRevision))
			requestRaw, err := json.Marshal(r)
			must(err)
			c := store.PlanStoryCandidate{Parent: a.Parent, RequestID: r.RequestID, Key: r.Draft.Key, Candidate: r.Candidate, RequestJSON: string(requestRaw)}
			must(h.DB.PutPlanStoryCandidate(ctx, c))
			if variant != "missing authority" {
				authorityRaw, err := json.Marshal(a)
				must(err)
				must(h.DB.RetainPlanStoryAuthority(ctx, a.Parent, r.RequestID, string(authorityRaw)))
			}
			if variant != "missing intent" {
				for _, detail := range []string{"update intent retained; remote API has no atomic compare-and-swap", "content readback differs"} {
					must(h.DB.AppendPlanStoryEffect(ctx, store.PlanStoryEffect{Parent: a.Parent, RequestID: r.RequestID, Phase: "update", Outcome: "uncertain", IssueID: child.ID, IssueNumber: child.Number, Detail: detail}))
				}
			}
			outcome := "conflict"
			if variant == "failed" {
				outcome = "failed"
			}
			must(h.DB.AppendPlanStoryEffect(ctx, store.PlanStoryEffect{Parent: a.Parent, RequestID: r.RequestID, Phase: "update", Outcome: outcome, IssueID: child.ID, IssueNumber: child.Number, Detail: "owner edits conflict with expected content revision"}))
			must(h.prepareCompletion(ctx, a, r, r.Draft))
			must(h.hold(ctx, c, r, "owner edits conflict with expected content revision"))
			must(h.DB.UpdatePlanWorkflowState(ctx, a.Parent, func(s *planbrief.PlanState) (bool, error) {
				s.Stories[0].SetEffectHold("unrelated-request", "unrelated hold")
				return true, nil
			}))
			ref, err := naming.ParseRef(a.Parent)
			must(err)
			title, _, err := report.RenderPlanDraft(int64(a.ParentNumber), d.Ordinal, r.Draft, r.Stage, a.ContentScope, displayIssue(ref, legacy).TitleKey)
			must(err)
			// Preserve all fixture prose and metadata payload; only the parent marker
			// maps to the isolated database's synthetic compatibility identity.
			body = strings.Replace(body, "<!-- lf:plan-story parent=4503599627567323 key=story-001 -->", planStoryMarker(a, a.SelectedKey), 1)
			switch variant {
			case "prose":
				body = strings.Replace(body, "The Invoices table is crowded", "The Invoices table is edited", 1)
			case "title":
				title += " edited"
			case "metadata":
				body = strings.Replace(body, "ed786d383bf1c84a31dcd0268555de20fb607414cc5d06da8cbaf0ff44b97885", strings.Repeat("0", 64), 1)
			case "ambiguous":
				body = strings.Replace(body, "layout-only", "layout&#x2d;only", 1)
			}
			remote.child = remote.snapshot("child", title, body)
			if variant == "closed" {
				remote.child.State = "closed"
			}
			if variant == "parent" {
				other := remote.snapshot("other-parent", "Parent", "Parent body")
				remote.parentOverride = &other
			}
			if variant == "github" {
				// The qualified provider control exercises the observation-only gate.
				h.Forge = &legacyObservationPlanForge{TicketPlanBridge: bridge}
			}
			restart()
			bridge.DB = h.DB
			if variant == "ordinary apply" {
				planApply(t, h, a, r, "conflict")
			} else {
				must(h.Reconcile(ctx, a, 10))
			}
			draft, err := h.DB.GetPlanStoryDraft(ctx, a.Parent, a.SelectedKey)
			must(err)
			state, _, err := h.DB.GetPlanWorkflowState(ctx, a.Parent)
			must(err)
			holds := state.Stories[0].EffectHolds
			if holds["unrelated-request"] != "unrelated hold" {
				t.Fatal("unrelated hold changed")
			}
			if variant == "confirmed" {
				observed, err := bridge.GetIssue(ctx, a.Repo, child.Number)
				must(err)
				if draft.Candidate != r.Candidate || draft.IssueID != child.ID || draft.Revision != PlanIssueRevision(observed.Title, observed.Body) || holds[r.RequestID] != "" {
					t.Fatalf("reconciliation incomplete: %+v holds=%v", draft, holds)
				}
				restart()
				bridge.DB = h.DB
				must(h.Reconcile(ctx, a, 10))
				planApply(t, h, a, r, "applied")
			} else if holds[r.RequestID] == "" || draft.Revision != shellRevision || draft.Candidate != "" {
				t.Fatal("unconfirmed conflict was settled")
			}
			if remote.updates != 0 {
				t.Fatal("reconciliation mutated remote content")
			}
		})
	}
}

type legacyObservationPlanForge struct{ *TicketPlanBridge }

func (f *legacyObservationPlanForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	issue, err := f.TicketPlanBridge.GetIssue(ctx, repo, number)
	if issue.QualifiedSnapshot != nil {
		issue.QualifiedSnapshot.Ref.Identity.Provider = ticket.GitHub
	}
	return issue, err
}
