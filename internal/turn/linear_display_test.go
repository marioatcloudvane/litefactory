package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const linearParentSlot = 4503599627567323
const linearChildSlot = 4503599627426925

func linearDisplayFixture(number int, key string) github.Issue {
	return github.Issue{Number: number, ID: int64(number), QualifiedSnapshot: &ticket.Snapshot{
		Ref:        ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear, Origin: "https://api.linear.app", RemoteID: key}},
		DisplayKey: key, BrowserURL: "https://linear.app/cloudvane/issue/" + key,
	}}
}
func assertLinearDisplay(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, forbidden := range []string{fmt.Sprint(linearParentSlot), fmt.Sprint(linearChildSlot), "github.com"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("exposed %q in %s", forbidden, text)
		}
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}
func TestLinearPlanDisplay(t *testing.T) {
	t.Parallel()
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: linearParentSlot}
	parent := linearDisplayFixture(linearParentSlot, "MAR-11")
	child := linearDisplayFixture(linearChildSlot, "MAR-12")
	pd, cd := displayIssue(ref, parent), displayIssue(ref, child)
	a := PlanStoryAuthority{Parent: ref.String(), ParentNumber: linearParentSlot, ParentID: parent.ID}
	shell, body := planStoryShell(a, linearParentSlot, 1, "story-001", planbrief.PlanStorySetEntry{Name: "Invoices", Intent: "Simpler invoices"}, pd.TitleKey)
	assertLinearDisplay(t, shell, "MAR-11-001: Invoices")
	if !strings.Contains(body, planStoryMarker(a, "story-001")) {
		t.Fatal("identity marker changed")
	}
	draft := planbrief.PlanDraft{Key: "story-001", Name: "Invoices", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear invoices", Benefit: "less work"}, Problem: "Unclear invoices", InScope: []string{"Invoices"}, NotInScope: []string{"Payments"}}
	rendered, err := report.RenderPlanDraftParts(linearParentSlot, 1, draft, "story", planbrief.ContentScope{}, pd.TitleKey)
	if err != nil {
		t.Fatal(err)
	}
	assertLinearDisplay(t, rendered.Title, "MAR-11-001: Invoices")
	for _, title := range []string{rendered.Title, fmt.Sprintf("%d-001: Invoices", linearParentSlot)} {
		edit, err := planbrief.ReconcilePlanDraftEdit(draft, linearParentSlot, 1, title, rendered.Body, planbrief.ContentScope{}, pd.TitleKey)
		if err != nil || !edit.PresentationOnly {
			t.Fatalf("title roundtrip %q: %+v %v", title, edit, err)
		}
		projected, err := report.RenderRetainedPlanDraftParts(linearParentSlot, 1, edit.Draft, "story", planbrief.ContentScope{}, pd.TitleKey)
		if err != nil {
			t.Fatal(err)
		}
		assertLinearDisplay(t, projected.Title, "MAR-11-001:")
	}
	if _, err := planbrief.ReconcilePlanDraftEdit(draft, linearParentSlot, 1, "MAR-99-001: Invoices", rendered.Body, planbrief.ContentScope{}, pd.TitleKey); err == nil {
		t.Fatal("wrong parent prefix accepted")
	}
	link := "[MAR-12](https://linear.app/cloudvane/issue/MAR-12)"
	summary := planSummaryText([][4]string{{draft.Name, issueLink(ref, child.Number, cd.Link()), "story", "review"}})
	assertLinearDisplay(t, summary, link, "Plan progress")
	table := renderPlanStoryTable(ref, []planStoryRow{{Key: draft.Key, Name: draft.Name, Issue: child.Number, Reference: cd.Link()}}, true)
	assertLinearDisplay(t, table, link)
	refusal := report.PlanReplyExplanationText(draft.Key, child.Number, 123, "Reply with the current approval command.", cd.Reference())
	assertLinearDisplay(t, refusal, link, "nothing was approved")
}

type displayReader struct {
	issue github.Issue
	err   error
}

func (r displayReader) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return r.issue, r.err
}
func TestLinearDisplayLookupFailure(t *testing.T) {
	t.Parallel()
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: linearParentSlot}
	failure := errors.New("provider read failed")
	if _, err := lookupTicketDisplay(t.Context(), displayReader{err: failure}, ref, linearChildSlot); !errors.Is(err, failure) {
		t.Fatalf("lost error: %v", err)
	}
	for _, forge := range []any{nil, displayReader{issue: github.Issue{Number: linearChildSlot}}, displayReader{issue: linearDisplayFixture(linearChildSlot, "")}} {
		d, err := lookupTicketDisplay(t.Context(), forge, ref, linearChildSlot)
		if err != nil {
			t.Fatal(err)
		}
		assertLinearDisplay(t, d.Link(), "this issue")
	}
}
func TestLinearWorkflowDisplay(t *testing.T) {
	t.Parallel()
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: linearParentSlot}
	pd := displayIssue(ref, linearDisplayFixture(linearParentSlot, "MAR-11"))
	cd := displayIssue(ref, linearDisplayFixture(linearChildSlot, "MAR-12"))
	for _, mode := range []string{"plan", "implement", "validate", "bug", "doctor"} {
		t.Run(mode, func(t *testing.T) {
			in := harness.PromptInput{FeatureSlug: "invoices", Branch: "lf/invoices", RunOrdinal: 1, Instruction: "Fix invoices", Ref: ref, IssueReference: pd.Link(), Mode: mode, IssueTitle: "Invoices", IssueBody: "Fix invoices", CommentBody: "Please continue", IssueAuthor: "owner", CommentAuthor: "owner", History: []harness.HistoryEntry{{Role: harness.RoleOldOwner, Body: "Previous note"}}}
			// Canonical transport bindings remain internal; these modes have no managed transport here.
			for _, resume := range []bool{false, true} {
				in.Resume = resume
				prompt, err := harness.BuildPrompt(in)
				if err != nil {
					t.Fatal(err)
				}
				assertLinearDisplay(t, prompt, "MAR-11")
			}
		})
	}
	a := ImplementAuthority{Parent: ref, Stories: []ImplementStory{{Key: "story-001", Name: "Invoices", IssueNumber: linearChildSlot}}}
	text, _ := renderImplementHandoffNotes(a, []store.ImplementStory{{Key: "story-001", State: store.ImplementParked}}, nil, map[string]store.ReportDelivery{"story-001": {CommentID: 123}}, map[string]implementDisplay{"story-001": {Reference: cd.Link(), ReplyURL: "https://linear.app/cloudvane/issue/MAR-12#comment-observed"}})
	// Hidden marker deliberately retains the internal task identity.
	_, visible, _ := strings.Cut(text, "-->\n")
	assertLinearDisplay(t, visible, cd.Link(), "#comment-observed")
	target := ValidateDeliveryTarget{Task: ref, Parent: ValidateIssueRef{Number: linearParentSlot, Display: pd}, Stories: []ValidateIssueRef{{Number: linearChildSlot, Key: "story-001", Title: "Invoices", Display: cd}}}
	rows := validateStoryRows(ValidateState{Stories: []ValidateJoinedStory{{Story: "story-001"}}}, &target)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	assertLinearDisplay(t, rows[0].story, cd.Link())
	binding := GraphDeliveryBinding{IssueNumber: linearParentSlot, IssueReference: pd.Link()}
	assertLinearDisplay(t, binding.displayReference(), pd.Link())
	redactor := redact.New(redact.Options{Environ: []string{}})
	_, bugBody, err := (GraphBugFinalization{Redactor: redactor}).presentation(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	assertLinearDisplay(t, bugBody.String(), pd.Link(), "Approved diagnosis")
	remote := validateRemote{target: target, b: ValidateFlowBindings{Delivery: ValidateDelivery{Redactor: redactor}}}
	_, validateBody, err := remote.issueContent(t.Context(), ValidateFinding{ID: "finding-1", Story: "story-001", Name: "Invoices"}, "<!-- fixture -->")
	if err != nil {
		t.Fatal(err)
	}
	assertLinearDisplay(t, validateBody.String(), pd.Link(), "Follow-up bug from Validate")
}

func TestGitHubDisplayCompatibility(t *testing.T) {
	t.Parallel()
	ref := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 770}
	d := displayIssue(ref, github.Issue{Number: 770})
	if d.Reference() != "#770" || d.Link() != "[#770](https://github.com/acme/api/issues/770)" || d.TitleKey != "770" {
		t.Fatal(d)
	}
	title, _ := planStoryShell(PlanStoryAuthority{}, 770, 1, "story-001", planbrief.PlanStorySetEntry{Name: "Invoices"}, d.TitleKey)
	if title != "770-001: Invoices" {
		t.Fatal(title)
	}
}

type displaySourceReader struct{ displayReader }

func (displaySourceReader) SourceLinks(context.Context, naming.Ref, int, int64) (string, string, error) {
	return "https://linear.app/cloudvane/issue/MAR-11", "https://linear.app/cloudvane/issue/MAR-12#comment-exact", nil
}
func TestLinearSourceReplyUsesObservedURL(t *testing.T) {
	t.Parallel()
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: linearParentSlot}
	parent, reply, err := reportSourceLinks(t.Context(), displaySourceReader{}, ref, linearChildSlot, 123)
	if err != nil {
		t.Fatal(err)
	}
	assertLinearDisplay(t, parent+reply, "https://linear.app/cloudvane/issue/MAR-11", "https://linear.app/cloudvane/issue/MAR-12#comment-exact")
	if _, _, err = reportSourceLinks(t.Context(), nil, ref, linearChildSlot, 123); err == nil {
		t.Fatal("missing provider reply metadata fabricated a link")
	}
}
