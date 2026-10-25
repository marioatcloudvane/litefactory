package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// PublishApprovedCreation publishes the exact proposal an owner approved, on
// the factory's own authority, from whatever mode or workflow path the task is
// on. The owner's approval of the hashed bytes is the consent; no agent run is
// needed to bind it.
//
// It is idempotent per approval intent. A replay of a finished publication
// reports its retained outcome. A resumed publication skips confirmed artifacts
// and stops, uncertain, at an artifact that was begun but never confirmed, so a
// crash between the remote write and its record can never create a duplicate.
// Only storage failures are returned as errors; a forge failure is a known
// outcome and is reported to the owner as uncertain.
func (r *MCPRuntime) PublishApprovedCreation(ctx context.Context, intentID int64) (*report.CreationPublished, error) {
	if r == nil || r.Creation == nil || r.CreationAuthorizer == nil || r.CreationAuthorizer.store == nil || r.Redactor == nil || r.Authority == nil {
		return nil, errors.New("creation publishing unavailable")
	}
	a := r.CreationAuthorizer
	var pub store.OwnerPublication
	var proposal store.CreationProposal
	var err error
	if ctx.Err() != nil {
		// Cancellation may stop accepted work, but must not admit new work.
		pub, proposal, err = a.store.LookupOwnerPublication(context.WithoutCancel(ctx), intentID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, ctx.Err()
		}
	} else {
		pub, proposal, err = a.store.ClaimOwnerPublication(ctx, intentID, a.enabled)
	}
	if err != nil {
		return nil, err
	}
	var e submission.CreationEnvelope
	if err := json.Unmarshal(proposal.Envelope, &e); err != nil {
		return nil, err
	}
	out := &report.CreationPublished{State: pub.State, Reason: pub.Reason}
	if pub.State == "refused" {
		return out, nil
	}
	db := r.Authority.db
	// The claim row elects one publisher per approval and a task's comments are
	// handled one at a time, so no further lock is needed.
	if pub.State == "dispatched" {
		state, cause := r.publishOwnerArtifacts(ctx, db, intentID, e)
		reason := ""
		if cause != nil {
			reason = cause.Error()
		}
		if err := a.store.FinishOwnerPublication(context.WithoutCancel(ctx), intentID, state, reason); err != nil {
			return nil, err
		}
		out.State = state
		if cause != nil {
			out.Reason = cause.Error()
		}
	}
	artifacts, err := db.OwnerArtifacts(context.WithoutCancel(ctx), intentID)
	if err != nil {
		return nil, err
	}
	source, err := naming.ParseRef(e.SourceTask)
	if err != nil {
		return nil, err
	}
	for _, d := range artifacts {
		display, err := lookupTicketDisplay(context.WithoutCancel(ctx), r.Creation, source, d.IssueNumber)
		if err != nil {
			return nil, err
		}
		out.Issues = append(out.Issues, report.PublishedIssue{Reference: display.Reference(), Title: e.Artifacts[d.Ordinal].Title, Number: d.IssueNumber, Relationship: d.Relationship})
	}
	return out, nil
}

// publishOwnerArtifacts writes each artifact not yet begun and returns the
// publication's outcome with the cause of an uncertain one.
func (r *MCPRuntime) publishOwnerArtifacts(ctx context.Context, db *store.DB, intentID int64, e submission.CreationEnvelope) (string, error) {
	source, err := naming.ParseRef(e.SourceTask)
	if err != nil {
		return "uncertain", err
	}
	repo := github.RepoFromRef(source)
	known, err := db.OwnerArtifacts(context.WithoutCancel(ctx), intentID)
	if err != nil {
		return "uncertain", err
	}
	for i, a := range e.Artifacts {
		if k := slices.IndexFunc(known, func(k store.OwnerPublicationArtifact) bool { return k.Ordinal == i }); k >= 0 {
			if known[k].IssueID < 1 {
				return "uncertain", fmt.Errorf("draft %d was started before an interruption and its issue was never confirmed", i+1)
			}
			if a.ParentIssue > 0 && known[k].Relationship != "attached" {
				return "uncertain", fmt.Errorf("draft %d has a confirmed issue but its required attachment was never confirmed", i+1)
			}
			continue
		}
		if err := r.checkOwnerPublicationWrite(ctx, intentID); err != nil {
			return "uncertain", err
		}
		// Reapplying redaction is hygiene only. Any changed byte demands new review.
		title, err := r.Redactor.Apply(ctx, a.Title, 256)
		if err != nil {
			return "uncertain", err
		}
		body, err := r.Redactor.Apply(ctx, a.Body, 65536)
		if err != nil {
			return "uncertain", err
		}
		if title.String() != a.Title || body.String() != a.Body || title.Truncated() || body.Truncated() {
			return "uncertain", errors.New("reviewed creation content changed")
		}
		begun, err := db.BeginOwnerArtifact(ctx, intentID, i)
		if err != nil {
			return "uncertain", err
		}
		if !begun {
			return "uncertain", fmt.Errorf("draft %d was already started; its outcome must not be retried", i+1)
		}
		if err := r.checkOwnerPublicationWrite(ctx, intentID); err != nil {
			return "uncertain", err
		}
		issue, err := createTicketEffect(ctx, r.Creation, fmt.Sprintf("owner-artifact:%d:%d", intentID, i), repo, a.ParentIssue, title, body)
		if err != nil {
			return "uncertain", fmt.Errorf("create draft %d: %w", i+1, err)
		}
		if issue.ID < 1 || issue.Number < 1 || issue.Author.ID != r.CreationAuthorizer.botID || issue.Title != a.Title || issue.Body != a.Body {
			return "uncertain", fmt.Errorf("created issue for draft %d does not match the approved content", i+1)
		}
		detail := store.OwnerPublicationArtifact{Ordinal: i, IssueID: issue.ID, IssueNumber: issue.Number, Relationship: "linked"}
		if a.ParentIssue > 0 {
			detail.Relationship = "uncertain"
		}
		if err := db.SaveOwnerArtifact(context.WithoutCancel(ctx), intentID, detail); err != nil {
			return "uncertain", err
		}
		if a.ParentIssue > 0 {
			if err := r.checkOwnerPublicationWrite(ctx, intentID); err != nil {
				return "uncertain", err
			}
			if err := r.Creation.AddSubIssue(ctx, repo, a.ParentIssue, issue.ID); err != nil {
				return "uncertain", fmt.Errorf("attach %s under its approved parent: %w", displayIssue(source, issue).Reference(), err)
			}
			parent, err := r.Creation.GetParentIssue(context.WithoutCancel(ctx), repo, issue.Number)
			if err != nil {
				return "uncertain", err
			}
			if parent.Number != a.ParentIssue {
				return "uncertain", fmt.Errorf("%s is not attached under its approved parent", displayIssue(source, issue).Reference())
			}
			detail.Relationship = "attached"
			if err := db.SaveOwnerArtifact(context.WithoutCancel(ctx), intentID, detail); err != nil {
				return "uncertain", err
			}
		}
	}
	return "confirmed", nil
}

// A retained claim bypasses expiry, but never cancellation or replacement of the
// exact approved proposal. Check immediately before each new remote write.
func (r *MCPRuntime) checkOwnerPublicationWrite(ctx context.Context, intentID int64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publication execution stopped: %w", err)
	}
	if err := r.CreationAuthorizer.store.CheckOwnerPublicationCurrent(ctx, intentID); err != nil {
		return fmt.Errorf("approved publication is no longer current: %w", err)
	}
	return nil
}
