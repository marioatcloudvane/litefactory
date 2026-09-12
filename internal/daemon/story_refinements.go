package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// storyRelationForge is separate so ordinary issues retain their existing API.
type storyRelationForge interface {
	GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
}

// managedParents joins the registry into the ordinary fair parent dispatch.
// This does not depend on GitHub updating the parent's timestamp when a child
// changes. Closed children are inspected by the same parent worker.
func (d *Daemon) managedParents(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	features, err := d.db.ListManagedFeatures(ctx)
	if err != nil {
		return err
	}
	for _, f := range features {
		s, err := d.db.GetSessionByTaskRef(ctx, f.ParentTask)
		if err != nil {
			return err
		}
		q := byRepo[s.RepoID]
		if q == nil {
			continue
		}
		if existing := seen[s.ID]; existing != nil {
			existing.addSource("managed_registry")
			continue
		}
		ref, err := naming.ParseRef(s.TaskRef)
		if err != nil {
			return err
		}
		c := q.candidate(s, ref)
		c.addSource("managed_registry")
		q.candidates = append(q.candidates, c)
		seen[s.ID] = c
	}
	return nil
}

// refineStories runs before the parent's own queue, so an admitted refinement
// cannot be overtaken by a promotion read in the same tick. All execution still
// goes through Execute's parent session lock and the existing inflight slot.
func (d *Daemon) refineStories(ctx context.Context, t *task) (bool, error) {
	f, err := d.db.GetManagedFeature(ctx, t.ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	relation, ok := d.forge.(storyRelationForge)
	if !ok {
		return true, fmt.Errorf("managed stories require verified parent reads")
	}
	stories, err := d.db.ListManagedStories(ctx, f.ParentTask, f.Generation)
	if err != nil {
		return true, err
	}
	slices.SortFunc(stories, func(a, b store.ManagedStory) int { return cmp.Compare(a.IssueNumber, b.IssueNumber) })

	stories = d.storiesToSweep(ctx, t, f, stories)

	cursor, err := d.db.StoryDiscoveryRotation(ctx, t.session.ID)
	if err != nil {
		return true, err
	}
	start := 0
	for start < len(stories) && stories[start].IssueNumber <= cursor {
		start++
	}
	if start == len(stories) {
		start = 0
	}
	// At most sixteen children per parent worker per tick; persisted rotation
	// makes the bound fair across restarts as well as successive ticks.
	discoveryDeferred := false
	for n := 0; n < min(16, len(stories)); n++ {
		s := stories[(start+n)%len(stories)]
		discoveryErr := d.discoverStoryComments(ctx, t, relation, s)
		if errors.Is(discoveryErr, ErrDiscoveryDeferred) {
			discoveryDeferred = true
			break
		}
		// Rotate even after a child-specific failure: a detached or inaccessible
		// child must not permanently starve every later story.
		if err := d.db.SetStoryDiscoveryRotation(ctx, t.session.ID, s.IssueNumber); err != nil {
			return true, err
		}
		if discoveryErr != nil {
			return true, discoveryErr
		}
	}
	pending, err := d.db.ListPendingStoryRefinements(ctx, t.session.ID)
	if err != nil {
		return true, err
	}
	if len(pending) == 0 {
		if discoveryDeferred {
			return true, ErrDiscoveryDeferred
		}
		return false, nil
	}
	r := pending[0]
	// Recheck the current direct relationship and numeric identity at execution;
	// admission is not a capability that survives detachment or replacement.
	child, err := d.forge.GetIssue(ctx, t.forgeRepo, r.ChildNumber)
	if err != nil {
		return true, err
	}
	parent, err := relation.GetParentIssue(ctx, t.forgeRepo, r.ChildNumber)
	if err != nil {
		return true, err
	}
	if parent.ID <= 0 || t.issue.ID <= 0 || child.ID != r.ChildID || parent.ID != t.issue.ID || parent.Number != t.issue.Number {
		return true, fmt.Errorf("managed story relationship changed before refinement")
	}
	f, err = d.db.GetManagedFeature(ctx, t.ref.String())
	if err != nil {
		return true, err
	}
	command := turn.ParseCommand(r.Body)
	commandMode, commanded := command.Mode()
	refusal := report.ReplyInvalid
	completed := false
	for _, story := range stories {
		if story.IssueID == r.ChildID && story.State == "completed" {
			completed = true
		}
	}
	if f.State != "planning" || f.Generation != r.Generation || completed {
		refusal = report.ReplyStoryLocked
	} else if command.Kind != turn.KindFreeText && (!commanded || commandMode != agent.ModePlan) {
		refusal = report.ReplyStoryPlanOnly
	}
	if refusal != report.ReplyInvalid {
		_, err = d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: r.CommentID, Refusal: refusal})
		return true, err
	}
	ack, ok := d.reporter.(interface {
		AcknowledgeStoryRefinement(context.Context, naming.Ref, int64, int64) error
	})
	if !ok {
		return true, fmt.Errorf("managed story acknowledgement unavailable")
	}
	if err := ack.AcknowledgeStoryRefinement(ctx, t.ref, t.session.ID, r.CommentID); err != nil {
		return true, err
	}
	if err := d.forge.AddCommentReaction(ctx, t.forgeRepo, r.CommentID, github.ReactionEyes); err != nil {
		return true, err
	}
	copyTask := *t
	copyTask.refining = true
	command = turn.Command{Kind: turn.KindFreeText, Text: command.Text}
	err = d.run(ctx, &copyTask, turn.Trigger{CommentID: r.CommentID, Body: r.Body, Author: r.ActorHandle}, command, t.log)
	return true, err
}

// storiesToSweep narrows a generation's children to the ones a sweep can still
// learn something from.
//
// Every child of every dispatched parent used to be read on every tick — the
// child, its parent link, its comments — whether or not anything had been said
// on any of them. The repository comment listing already names the children
// that were commented on, once per tick for the whole repository, so that is
// the signal, and a child it did not name is not read at all.
//
// Deliberately not narrowed further by the feature's state. A feature past
// planning cannot act on a refinement, but it must still answer one that was
// written to — a refusal is an answer and silence is not — and once discovery
// is driven by the listing there is nothing left for a state gate to save: a
// locked feature with nothing said on it already reads no child at all.
//
// A failure to consult the debt is not a reason to skip the sweep. The walk is
// bounded and idempotent, so falling back to every child costs quota; guessing
// that nothing moved costs an owner's comment.
func (d *Daemon) storiesToSweep(ctx context.Context, t *task, f store.ManagedFeature, stories []store.ManagedStory) []store.ManagedStory {
	awaiting, err := d.db.ListStoriesAwaitingDiscovery(ctx, t.session.ID, f.ParentTask, f.Generation)
	if err != nil {
		t.log.WarnContext(ctx, "could not read which children owe a comment sweep; sweeping all of them",
			slog.Any("error", err))
		return stories
	}
	owed := make(map[int]bool, len(awaiting))
	for _, child := range awaiting {
		owed[child] = true
	}
	out := stories[:0]
	for _, s := range stories {
		if owed[s.IssueNumber] {
			out = append(out, s)
		}
	}
	return out
}

func (d *Daemon) discoverStoryComments(ctx context.Context, t *task, relation storyRelationForge, s store.ManagedStory) error {
	child, err := d.discoveryIssue(ctx, t.forgeRepo, s.IssueNumber)
	if err != nil {
		return err
	}
	parent, err := discoveryCall(ctx, d, discoveryKey(t.forgeRepo, "parent", s.IssueNumber), 1, func(ctx context.Context) (github.Issue, error) {
		return relation.GetParentIssue(ctx, t.forgeRepo, s.IssueNumber)
	})
	if err != nil {
		return err
	}
	if parent.ID <= 0 || t.issue.ID <= 0 || child.ID != s.IssueID || parent.ID != t.issue.ID || parent.Number != t.issue.Number {
		return fmt.Errorf("managed story identity or direct parent changed")
	}
	after, err := d.db.StoryDiscoveryCursor(ctx, t.session.ID, s.IssueNumber)
	if err != nil {
		return err
	}
	comments, err := d.discoveryComments(ctx, t.forgeRepo, s.IssueNumber, after)
	if err != nil {
		return err
	}
	slices.SortFunc(comments, func(a, b github.Comment) int { return cmp.Compare(a.ID, b.ID) })
	ids := d.filter.Identities()
	for _, c := range comments {
		if c.ID <= after {
			continue
		}
		if ids.OwnerUserID > 0 && c.Author.ID == ids.OwnerUserID && c.Author.ID != ids.BotUserID && !c.Author.IsBot && !turn.HasMarker(c.Body) {
			if err := d.db.AdmitStoryRefinement(ctx, store.StoryRefinement{SessionID: t.session.ID, CommentID: c.ID, ChildNumber: s.IssueNumber, ChildID: s.IssueID, Generation: s.Generation, ActorID: c.Author.ID, ActorHandle: c.Author.Login, Body: c.Body}); err != nil {
				return err
			}
		}
		// This is only discovery. Pending admitted rows remain independently
		// retryable until the reporting transaction marks them consumed.
		if err := d.db.AdvanceStoryDiscovery(ctx, t.session.ID, s.IssueNumber, c.ID); err != nil {
			return err
		}
	}
	// A sweep that read nothing still has to leave a cursor behind. The row's
	// existence is what says this child's history has been seen — without it a
	// child nobody ever comments on reads as never swept forever, and the walk
	// goes straight back to costing three requests a tick. MAX makes this a
	// no-op against a cursor the loop above already moved further.
	if err := d.db.AdvanceStoryDiscovery(ctx, t.session.ID, s.IssueNumber, after); err != nil {
		return err
	}
	// Last, and only on the path where every comment above was admitted and the
	// cursor advanced past it. An early return leaves the debt standing, which
	// costs one more sweep; clearing it before the cursor moved would cost the
	// comment itself.
	return d.db.ClearStoryDiscoveryPending(ctx, t.session.ID, s.IssueNumber)
}
