package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type followupRenderer interface {
	RenderFollowupContent(context.Context, string, string) (redact.Body, redact.Body, error)
	RenderFollowupNotice(context.Context, string, string) (redact.Body, error)
}
type followupForge interface {
	ListFollowupIssues(context.Context, github.Repo) ([]github.Issue, error)
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error)
	AddSubIssue(context.Context, github.Repo, int, int64) error
}

// FollowupRequest selects immutable proposal content and linked or attached creation.
type FollowupRequest struct {
	Generation int
	Key        string
	Attach     bool
}

// ParseFollowup parses explicit approval for a concrete generation and proposal.
func ParseFollowup(body string) (FollowupRequest, error) {
	var r FollowupRequest
	f := strings.Fields(body)
	if len(body) > 512 || len(f) != 4 || f[0] != "/followup" || (f[3] != "linked" && f[3] != "attach") {
		return r, fmt.Errorf("use /followup <generation> <proposal SHA256> linked|attach")
	}
	n, err := strconv.Atoi(f[1])
	if err != nil || n < 1 || len(f[2]) != 64 || strings.Trim(f[2], "0123456789abcdef") != "" {
		return r, fmt.Errorf("invalid followup generation or proposal identity")
	}
	r.Generation = n
	r.Key = f[2]
	r.Attach = f[3] == "attach"
	return r, nil
}

// FollowupControl is factory-authenticated explicit approval, never runner output.
type FollowupControl struct {
	Parent                      string
	CommentID, ActorID, OwnerID int64
	Body                        string
}

// ApplyFollowup checks owner authority and executes durable follow-up effects.
func (p *StoryProgress) ApplyFollowup(ctx context.Context, c FollowupControl) (err error) {
	if c.OwnerID < 1 || c.ActorID != c.OwnerID || c.CommentID < 1 {
		return fmt.Errorf("followup requires authenticated numeric owner comment")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	release, err := p.db.LockStoryProgress(ctx, c.Parent, p.lockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	r, err := ParseFollowup(c.Body)
	if err != nil {
		return p.refuseFollowup(ctx, c, err.Error())
	}
	f, err := p.db.GetManagedFeature(ctx, c.Parent)
	if errors.Is(err, store.ErrNotFound) {
		return p.refuseFollowup(ctx, c, "No approved separated workflow exists for this parent.")
	}
	if err != nil {
		return err
	}
	if f.WorkflowVersion != store.WorkflowSeparated || f.Generation != r.Generation {
		return p.refuseFollowup(ctx, c, "Followup generation/workflow mismatch.")
	}
	proposal, err := p.db.GetFollowup(ctx, r.Key)
	if errors.Is(err, store.ErrNotFound) {
		return p.refuseFollowup(ctx, c, "Unknown followup proposal; use a published concrete proposal identity.")
	}
	if err != nil {
		return err
	}
	if proposal.Parent != c.Parent || proposal.Generation != r.Generation || !proposal.Proposed {
		return p.refuseFollowup(ctx, c, "Followup has not been proposed for this scope.")
	}
	if proposal.ApprovalComment > 0 && (proposal.ActorID != c.ActorID || (proposal.Attach && !r.Attach)) {
		return p.refuseFollowup(ctx, c, "Approval conflicts with the retained owner or attachment scope.")
	}
	if err = p.db.ApproveFollowup(ctx, r.Key, c.CommentID, c.ActorID, r.Attach); err != nil {
		return err
	}
	return p.deliverFollowups(ctx, c.Parent)
}

// A terminal refusal consumes the owner comment only after its deduplicated
// diagnostic is delivered. Remote/storage failures still leave retry debt.
func (p *StoryProgress) refuseFollowup(ctx context.Context, c FollowupControl, detail string) error {
	q := store.FollowupProposal{Parent: c.Parent, Key: agent.StoryDigest([]byte(fmt.Sprintf("%s/%d", c.Parent, c.CommentID)))}
	return p.followupNotice(ctx, q, "refused", fmt.Sprintf("Follow-up approval refused for owner comment %d: %s No authority was recorded. Submit a corrected explicit control.", c.CommentID, detail))
}

func (p *StoryProgress) retainFollowups(ctx context.Context, parent string) error {
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return err
	}
	assessments, err := p.db.ListFollowupAssessments(ctx)
	if err != nil {
		return err
	}
	renderer, ok := p.renderer.(followupRenderer)
	for _, a := range assessments {
		if a.Parent != parent {
			continue
		}
		var scope agent.ValidationContext
		if err = json.Unmarshal([]byte(a.ContextJSON), &scope); err != nil {
			return err
		}
		out, err := agent.ParseValidationOutput([]byte(a.ResultJSON), &scope)
		if err != nil {
			return err
		}
		for _, s := range out.Stories {
			var source string
			for _, known := range scope.Stories {
				if known.Story.Key == s.Key {
					display, err := lookupTicketDisplay(ctx, p.forge, ref, known.Story.IssueNumber)
					if err != nil {
						return err
					}
					source = display.URL
					break
				}
			}
			for _, group := range []struct {
				name     string
				findings []agent.ValidationFinding
			}{{"criterion", s.Criteria}, {"review", s.Reviews}} {
				for _, finding := range group.findings {
					if finding.Status == "satisfied" {
						continue
					}
					if !ok {
						return fmt.Errorf("followup renderer unavailable")
					}
					title := fmt.Sprintf("Follow-up: %s %s finding", s.Key, group.name)
					body := fmt.Sprintf("## Source\n\nParent: %s\nStory: %s\n%s: %s\nAssessment run: %d; revision: %s; snapshot: %s\n\n## Finding\n\nStatus: %s (%s)\n\n%s\n\n## Proposed work\n\nAddress the recorded finding and retain evidence for a separate review. Creating this follow-up does not accept or waive the original obligation. Linked-only by default; attachment requires explicit owner approval.\n", parent, source, group.name, finding.Name, a.RunID, s.Revision, s.Snapshot, finding.Status, finding.Classification, finding.Evidence)
					key := agent.StoryDigest([]byte(title + "\x00" + body))
					body += "\n" + followupMarker(key)
					t, b, err := renderer.RenderFollowupContent(ctx, title, body)
					if err != nil {
						return err
					}
					proposal := store.FollowupProposal{Key: key, Parent: parent, Generation: a.Generation, RunID: a.RunID, Title: t.String(), Body: b.String()}
					if err = p.db.RetainFollowup(ctx, proposal); err != nil {
						return err
					}
				}
			}
		}
		if err := p.db.MarkFollowupsMaterialized(ctx, a.RunID); err != nil {
			return err
		}
	}
	return nil
}
func followupMarker(key string) string { return "<!-- litefactory:followup:" + key + " -->" }
func (p *StoryProgress) deliverFollowups(ctx context.Context, parent string) error {
	if err := p.retainFollowups(ctx, parent); err != nil {
		return err
	}
	all, err := p.db.ListFollowups(ctx)
	if err != nil {
		return err
	}
	var faults []error
	delivered := 0
	for _, proposal := range all {
		if proposal.Parent != parent || proposal.Reported || (proposal.Proposed && proposal.ApprovalComment == 0) {
			continue
		}
		if delivered >= 16 {
			break
		}
		delivered++
		if err := p.deliverFollowup(ctx, &proposal); err != nil {
			proposal.Diagnostic = err.Error()
			saveErr := p.db.SaveFollowupEffects(ctx, proposal)
			noticeErr := p.followupNotice(ctx, proposal, "pending", "Follow-up remains pending: "+err.Error()+"\nProposal: "+proposal.Key+". No completion or source waiver is claimed.")
			faults = append(faults, errors.Join(err, saveErr, noticeErr))
		}
	}
	return errors.Join(faults...)
}
func (p *StoryProgress) followupNotice(ctx context.Context, q store.FollowupProposal, stage, detail string) error {
	publisherID, publisherErr := p.ticketPublisherID(ctx, q.Parent)
	if publisherErr != nil {
		return publisherErr
	}
	renderer, ok := p.renderer.(followupRenderer)
	if !ok {
		return fmt.Errorf("followup renderer unavailable")
	}
	ref, err := naming.ParseRef(q.Parent)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	marker := fmt.Sprintf("<!-- litefactory:followup-notice:%s:%s -->", q.Key, stage)
	comments, err := p.forge.ListCommentsAfter(ctx, repo, ref.Issue, 0)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if !hasTicketNoticeGuard(p.forge) && c.Author.ID == publisherID && exactProgressMarker(c.Body, marker) {
			return nil
		}
	}
	body, err := renderer.RenderFollowupNotice(ctx, detail, marker)
	if err != nil {
		return err
	}
	c, err := postTicketNotice(ctx, p.forge, marker, q.Parent, publisherID, repo, ref.Issue, body)
	if err != nil {
		return err
	}
	if c.ID < 1 || c.Author.ID != publisherID {
		return fmt.Errorf("followup report provenance mismatch")
	}
	return nil
}
func (p *StoryProgress) deliverFollowup(ctx context.Context, q *store.FollowupProposal) error {
	publisherID, publisherErr := p.ticketPublisherID(ctx, q.Parent)
	if publisherErr != nil {
		return publisherErr
	}
	if !q.Proposed {
		detail := fmt.Sprintf("Follow-up proposal %s (generation %d)\n\nTitle: %s\n\n%s\n\nApprove exact content with `/followup %d %s linked`; use `attach` instead of `linked` to explicitly attach it to the parent (and block readiness until closed). Neither action waives its source requirement.", q.Key, q.Generation, q.Title, q.Body, q.Generation, q.Key)
		if err := p.followupNotice(ctx, *q, "proposal", detail); err != nil {
			return err
		}
		q.Proposed = true
		if err := p.db.SaveFollowupEffects(ctx, *q); err != nil {
			return err
		}
	}
	if q.ApprovalComment == 0 {
		return nil
	}
	gateway, ok := p.forge.(followupForge)
	if !ok {
		return fmt.Errorf("followup forge unavailable")
	}
	renderer, ok := p.renderer.(followupRenderer)
	if !ok {
		return fmt.Errorf("followup renderer unavailable")
	}
	ref, err := naming.ParseRef(q.Parent)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	if q.IssueID == 0 {
		var found github.Issue
		matches := 0
		qualified := false
		if q.Attempted {
			if recoverer, ok := gateway.(interface {
				RecoverIssueEffect(context.Context, string, github.Repo, int, string, string) (github.Issue, bool, error)
			}); ok {
				issue, handled, recoverErr := recoverer.RecoverIssueEffect(ctx, "followup:"+q.Key, repo, 0, q.Title, q.Body)
				if recoverErr != nil {
					return recoverErr
				}
				qualified = handled
				if handled {
					found, matches = issue, 1
				}
			}
		}
		if !qualified {
			issues, err := gateway.ListFollowupIssues(ctx, repo)
			if err != nil {
				return err
			}
			for _, issue := range issues {
				if issue.Author.ID == publisherID && exactProgressMarker(issue.Body, followupMarker(q.Key)) {
					found = issue
					matches++
				}
			}
		}
		if matches > 1 {
			return fmt.Errorf("ambiguous duplicate followup marker")
		}
		if matches == 0 {
			if q.Attempted {
				return fmt.Errorf("uncertain followup create; awaiting verified marker reconciliation before any retry")
			}
			t, b, err := renderer.RenderFollowupContent(ctx, q.Title, q.Body)
			if err != nil {
				return err
			}
			if t.String() != q.Title || b.String() != q.Body {
				return fmt.Errorf("followup rendered content changed; new proposal and approval required")
			}
			q.Attempted = true
			if err = p.db.SaveFollowupEffects(ctx, *q); err != nil {
				return err
			}
			found, err = createTicketEffect(ctx, gateway, "followup:"+q.Key, repo, 0, t, b)
			if err != nil {
				return err
			}
		}
		if found.ID < 1 || found.Number < 1 || found.Author.ID != publisherID || found.Title != q.Title || found.Body != q.Body {
			return fmt.Errorf("followup created content or identity mismatch")
		}
		verified, err := p.forge.GetIssue(ctx, repo, found.Number)
		if err != nil {
			return err
		}
		if verified.ID != found.ID || verified.Author.ID != publisherID || verified.Title != q.Title || verified.Body != q.Body {
			return fmt.Errorf("followup issue verification mismatch")
		}
		q.IssueID = found.ID
		q.IssueNumber = found.Number
		if err = p.db.SaveFollowupEffects(ctx, *q); err != nil {
			return err
		}
	}
	if q.Attach && !q.Attached {
		children, err := gateway.ListSubIssues(ctx, repo, ref.Issue)
		if err != nil {
			return err
		}
		found := false
		for _, child := range children {
			if child.ID == q.IssueID && child.Number == q.IssueNumber {
				found = true
			}
		}
		if !found {
			if err = gateway.AddSubIssue(ctx, repo, ref.Issue, q.IssueID); err != nil {
				return err
			}
		}
		relation, err := p.forge.GetParentIssue(ctx, repo, q.IssueNumber)
		if err != nil {
			return err
		}
		parent, err := p.forge.GetIssue(ctx, repo, ref.Issue)
		if err != nil {
			return err
		}
		if parent.ID < 1 || relation.ID != parent.ID || relation.Number != ref.Issue {
			return fmt.Errorf("followup attachment verification mismatch")
		}
		q.Attached = true
		if err = p.db.SaveFollowupEffects(ctx, *q); err != nil {
			return err
		}
	}
	display, err := lookupTicketDisplay(ctx, p.forge, ref, q.IssueNumber)
	if err != nil {
		return err
	}
	reference := display.Link()
	if ref.Forge != "linear" {
		reference += fmt.Sprintf(" (numeric ID %d)", q.IssueID)
	}
	detail := fmt.Sprintf("Approved follow-up created: %s. Owner %d approved via comment %d. Attached to parent: %t. Original requirement remains unchanged; no waiver or merge authority was granted.", reference, q.ActorID, q.ApprovalComment, q.Attached)
	stage := "complete"
	if q.Attached {
		stage = "attached"
		detail += fmt.Sprintf(" Attachment approval comment: %d.", q.AttachmentComment)
	}
	if err = p.followupNotice(ctx, *q, stage, detail); err != nil {
		return err
	}
	q.Reported = true
	q.Diagnostic = ""
	return p.db.SaveFollowupEffects(ctx, *q)
}
