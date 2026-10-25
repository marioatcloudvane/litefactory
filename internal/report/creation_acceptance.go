package report

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// CreationAcceptance carries durable historical admission values, never mutable
// approval text or a new deadline. Publication is descriptive context only.
type CreationAcceptance struct {
	Hash, Relationship   string
	CreatedAt, ExpiresAt time.Time
	Publication          CreationPublication
	// Published is the factory's own publication of the approved proposal. Nil
	// keeps the historical acknowledgment for a runtime without publishing.
	Published *CreationPublished
}

// CreationPublished is the retained outcome of publishing an approved proposal.
// State is confirmed, uncertain or refused; Reason is a refusal code or the
// cause of an uncertain outcome.
type CreationPublished struct {
	State, Reason string
	Issues        []PublishedIssue
}

// PublishedIssue is one begun artifact. Number is zero when its issue was never
// confirmed.
type PublishedIssue struct {
	Title        string
	Reference    string
	Number       int
	Relationship string
}

func (a *CreationAcceptance) valid() bool {
	if a == nil {
		return false
	}
	hash, err := hex.DecodeString(a.Hash)
	return err == nil && len(hash) == 32 && strings.ToLower(a.Hash) == a.Hash &&
		(a.Relationship == "linked" || a.Relationship == "attach") &&
		!a.CreatedAt.IsZero() && !a.ExpiresAt.IsZero() && a.ExpiresAt.Equal(a.CreatedAt.Add(30*time.Minute)) &&
		a.CreatedAt.Year() >= 1 && a.ExpiresAt.Year() <= 9999
}

func (a CreationAcceptance) body() string {
	if a.Published != nil {
		return a.published()
	}
	return fmt.Sprintf("Creation approval was accepted and recorded for proposal `%s` with relationship `%s`. Approval alone started no publication.\n\nThe original approval-comment timestamp was **%s**; the stored original expiry was **%s**. An eligible publishing attempt must bind before that deadline. This is a historical acknowledgment and does not establish that the approval is still usable or the proposal is still current.\n\n%s", a.Hash, a.Relationship, a.CreatedAt.UTC().Format(time.RFC3339Nano), a.ExpiresAt.UTC().Format(time.RFC3339Nano), CreationGuidance(a.Publication))
}

func (a CreationAcceptance) published() string {
	p := a.Published
	var b strings.Builder
	switch p.State {
	case "confirmed":
		fmt.Fprintf(&b, "Published proposal `%s`: %d issue(s) created.\n\n", a.Hash, len(p.Issues))
	case "refused":
		fmt.Fprintf(&b, "Approval for proposal `%s` was recorded, but nothing was published: %s\n", a.Hash, ownerPublicationRefusal(p.Reason))
		return b.String()
	default:
		fmt.Fprintf(&b, "Publishing proposal `%s` stopped part-way: %s\n\n", a.Hash, p.Reason)
	}
	for _, issue := range p.Issues {
		reference := issue.Reference
		if reference == "" {
			reference = fmt.Sprintf("#%d", issue.Number)
		}
		switch {
		case issue.Number == 0:
			fmt.Fprintf(&b, "- %s: **not confirmed**. Its creation outcome is unknown; repeating creation could create it twice.\n", issue.Title)
		case issue.Relationship == "attached":
			fmt.Fprintf(&b, "- %s %s (sub-issue)\n", reference, issue.Title)
		case issue.Relationship == "uncertain":
			fmt.Fprintf(&b, "- %s %s (created; attaching it as a sub-issue was not confirmed)\n", reference, issue.Title)
		default:
			fmt.Fprintf(&b, "- %s %s\n", reference, issue.Title)
		}
	}
	if p.State != "confirmed" {
		b.WriteString("\nThe drafts after the stopping point were not published. Approving the same proposal again does nothing. Inspect the listed issues and repository issue list to establish which creations and attachments succeeded before requesting a new proposal containing only confirmed missing work.\n")
	}
	return b.String()
}

func ownerPublicationRefusal(reason string) string {
	switch reason {
	case "expired":
		return "the approval was processed more than 30 minutes after it was posted. Post the same `/mcp-create` command again."
	case "stale":
		return "a newer proposal replaced this one. Approve the newest proposal instead."
	case "quota":
		return "the repository's limit of 20 created issues per 24 hours would be exceeded. Approve again once earlier issues age out of the window."
	case "bound":
		return "an agent run already bound this approval; its current publication status is not established by this refusal. Ask the factory operator to inspect the retained approval on the factory host with `litefactory logs --all`."
	case "disabled":
		return "issue creation is disabled in this factory's configuration (`allow_mcp_story_creation`). Ask the factory operator to restore publishing eligibility before requesting a fresh proposal."
	default:
		return reason
	}
}
