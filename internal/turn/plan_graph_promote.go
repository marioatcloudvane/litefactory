package turn

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// planSetRefusal is an owner command the factory declines with an explanation.
// It is a settled outcome, not a failure: retrying the same comment can never
// succeed, so the turn ends as a refusal that the owner reads on the issue and
// that consumes the comment, instead of an error that would retry it forever.
type planSetRefusal struct{ reason string }

func (e *planSetRefusal) Error() string { return e.reason }

var planSetRevisionText = regexp.MustCompile(`^[0-9a-f]{64}$`)

// planSetPromotionArgument reports whether body is an attempt at the promotion
// command and returns the text after it. The prefix carries a space, so the
// existing final-plan approval `/approve-plan-set` is never mistaken for it.
func planSetPromotionArgument(body string) (string, bool) {
	text := strings.TrimSpace(body)
	if text == strings.TrimSpace(planbrief.PlanSetPromotionPrefix) {
		return "", true
	}
	rest, ok := strings.CutPrefix(text, planbrief.PlanSetPromotionPrefix)
	return strings.TrimSpace(rest), ok
}

func planSetRefused(s planbrief.PlanState, why string) error {
	reason := "Story set not promoted: " + why
	if set := s.StorySet; set != nil && set.Revision != "" && !set.Promoted {
		reason += fmt.Sprintf(" The current proposed set is revision %s; to promote exactly that set, reply `%s`.", set.Revision, planbrief.PlanSetPromotionCommand(set.Revision))
	}
	return &planSetRefusal{reason: reason}
}

// promoteSet applies the owner's `/approve-plan set <revision>` parent comment.
//
// Authority is the same as for every other owner command on the parent: the
// factory compares the comment's forge-authenticated author with the configured
// numeric owner identity. Nothing an agent returned reaches this function, and
// the set effect that records a proposal never promotes, so no role can promote.
//
// Reservation happens in one place, in displayed order. Story ordinals are
// reserved first, idempotently, then the mapping, keys and stories are saved in
// one compare-and-set write of the Plan state; a crash between the two leaves
// the state unpromoted and the retry reserves the same rows again, and a repeat
// after the state was saved changes nothing. The comment is consumed in the same
// write, so it is handled exactly once.
func (r *planGraphRuntime) promoteSet(ctx context.Context, s planbrief.PlanState, revision int64, argument string) error {
	trigger := r.req.Trigger
	if trigger.AuthorID < 1 || trigger.AuthorID != r.ownerID {
		return planSetRefused(s, "only the authenticated owner can promote a story set.")
	}
	if slices.Contains(s.ConsumedReplies, trigger.CommentID) {
		return nil
	}
	if !planSetRevisionText.MatchString(argument) {
		return planSetRefused(s, "the command must be the exact text `/approve-plan set <revision>` naming the 64-character revision shown with the set.")
	}
	// A set promoted before keeps its mapping and ordinals: promotion then changes
	// nothing, the comment is still consumed and the run only continues.
	_, err := s.PromoteStorySet(argument)
	switch {
	case errors.Is(err, planbrief.ErrPlanStorySetMissing), errors.Is(err, planbrief.ErrPlanStorySetStaleRevision), errors.Is(err, planbrief.ErrPlanStorySetHasStories), errors.Is(err, planbrief.ErrPlanStorySetUnorderable), errors.Is(err, planbrief.ErrPlanStorySetRevisionMismatch):
		why := err.Error() + "."
		if errors.Is(err, planbrief.ErrPlanStorySetStaleRevision) {
			why = "the approval names a revision that is not the current story set, so it is refused; a changed set needs a new review."
		}
		return planSetRefused(s, why)
	case err != nil:
		return err
	}
	parent := r.result.Ref.String()
	for _, entry := range s.StorySet.Entries {
		key := s.StorySet.Keys[entry.ID]
		draft, reserveErr := r.opts.DB.ReservePlanStory(ctx, parent, key)
		if reserveErr != nil {
			return fmt.Errorf("reserve story %s: %w", key, reserveErr)
		}
		// The public ordinal shown on the sub-issue is the reserved ordinal, so it
		// must be the story's own number for the list order to be what is shown.
		if n, convErr := strconv.Atoi(strings.TrimPrefix(key, "story-")); convErr != nil || int64(n) != draft.Ordinal {
			return &planSetRefusal{reason: fmt.Sprintf("Story set not promoted: the parent already holds ordinal %d for story %s, so its public number would not match its position in the set.", draft.Ordinal, key)}
		}
	}
	s.ConsumedReplies = append(s.ConsumedReplies, trigger.CommentID)
	return r.persist(ctx, s, revision)
}
