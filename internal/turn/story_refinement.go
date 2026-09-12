package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func (r *Reporter) refinementDestination(ctx context.Context, sessionID, commentID int64, ref naming.Ref) (naming.Ref, *store.StoryRefinement, error) {
	refinement, err := r.db.GetStoryRefinement(ctx, sessionID, commentID)
	if errors.Is(err, store.ErrNotFound) {
		return ref, nil, nil
	}
	if err != nil {
		return ref, nil, err
	}
	ref.Issue = refinement.ChildNumber
	return ref, &refinement, nil
}

// The target context is a bounded history entry: BuildPrompt escapes and frames
// it as untrusted evidence. The managed plan handoff supplies full story bodies.
func prepareRefinementContext(ctx context.Context, opts Options, req Request, input *harness.PromptInput) error {
	r, err := opts.DB.GetStoryRefinement(ctx, req.SessionID, req.Trigger.CommentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	feature, err := opts.DB.GetManagedFeature(ctx, input.Ref.String())
	if err != nil {
		return err
	}
	if feature.State != "planning" || feature.Generation != r.Generation {
		return fmt.Errorf("user story is locked after promotion to Implement")
	}
	forge, ok := opts.SubIssueCreator.(interface {
		GetIssue(context.Context, github.Repo, int) (github.Issue, error)
		GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
	})
	if !ok {
		return fmt.Errorf("current story context requires verified GitHub reads")
	}
	repo := github.RepoFromRef(input.Ref)
	child, err := forge.GetIssue(ctx, repo, r.ChildNumber)
	if err != nil {
		return err
	}
	parent, err := forge.GetParentIssue(ctx, repo, r.ChildNumber)
	if err != nil {
		return err
	}
	currentParent, err := forge.GetIssue(ctx, repo, input.Ref.Issue)
	if err != nil {
		return err
	}
	if parent.ID <= 0 || currentParent.ID <= 0 || child.ID != r.ChildID || parent.ID != currentParent.ID || parent.Number != input.Ref.Issue {
		return fmt.Errorf("story relationship changed before context assembly")
	}
	if len(child.Body) > planbrief.MaxStoryBodyBytes || len(child.Title) > 1024 {
		return fmt.Errorf("current story exceeds context limits")
	}
	input.RefinementSource = fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d (owner ID %d)", input.Ref.Owner, input.Ref.Repo, r.ChildNumber, r.CommentID, r.ActorID)
	input.RefinementStory = fmt.Sprintf("Issue #%d (ID %d); expected_base %s\nTitle: %s\n\n%s", r.ChildNumber, r.ChildID, planbrief.StoryHash(child.Title, child.Body), child.Title, child.Body)
	input.History = append(input.History, harness.HistoryEntry{Role: harness.RoleOldThirdParty, Body: fmt.Sprintf("Planning refinement targets managed story #%d (GitHub issue ID %d), generation %d. Source comment ID %d by numeric owner %d. Parent task %s. Keep this parent plan and session as the sole execution identity.", r.ChildNumber, r.ChildID, r.Generation, r.CommentID, r.ActorID, input.Ref.String())})
	return nil
}

// AcknowledgeStoryRefinement links the pending child discussion to its parent
// before work starts. This delivery never consumes the discussion itself.
func (r *Reporter) AcknowledgeStoryRefinement(ctx context.Context, ref naming.Ref, sessionID, commentID int64) (err error) {
	if r.botUserID < 1 || r.storyLockDir == "" {
		return fmt.Errorf("recoverable story acknowledgement identity or lock directory missing")
	}
	reader, ok := r.forge.(interface {
		ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error)
	})
	if !ok {
		return fmt.Errorf("story acknowledgement recovery reads unavailable")
	}
	release, err := r.db.LockStoryProgress(ctx, ref.String(), r.storyLockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	destination, refinement, err := r.refinementDestination(ctx, sessionID, commentID, ref)
	if err != nil {
		return err
	}
	if refinement == nil {
		return fmt.Errorf("story acknowledgement requires admitted source")
	}
	ack, err := r.db.StoryRefinementAcknowledgement(ctx, sessionID, commentID)
	if err != nil || ack != 0 {
		return err
	}
	body, err := r.renderer.RenderReply(ctx, report.Reply{Ref: ref, Kind: report.ReplyStoryPlanning, SourceGeneration: refinement.Generation, SourceStoryIssue: refinement.ChildNumber, SourceCommentID: commentID})
	if err != nil {
		return err
	}
	// The admitted row is the durable intent: every unacknowledged admission
	// owes exactly this source/generation-bound operation. Recover before any
	// retry so a lost response never posts another signed acknowledgement.
	marker := report.StoryAcknowledgementMarker(ref, refinement.Generation, refinement.ChildNumber, commentID)
	comments, err := reader.ListCommentsAfter(ctx, github.RepoFromRef(ref), destination.Issue, 0)
	if err != nil {
		return err
	}
	var recovered int64
	for _, comment := range comments {
		if comment.Author.ID != r.botUserID || !strings.Contains(comment.Body, marker) {
			continue
		}
		if recovered != 0 || comment.ID < 1 || comment.Body != body.String() {
			return fmt.Errorf("ambiguous story acknowledgement recovery")
		}
		recovered = comment.ID
	}
	if recovered != 0 {
		return r.db.AcknowledgeStoryRefinement(ctx, sessionID, commentID, recovered)
	}
	posted, err := r.post(ctx, destination, body, r.log)
	if err != nil {
		return err
	}
	return r.db.AcknowledgeStoryRefinement(context.WithoutCancel(ctx), sessionID, commentID, posted.ID)
}
