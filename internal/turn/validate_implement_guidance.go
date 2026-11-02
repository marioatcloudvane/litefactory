package turn

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// implementPrerequisiteGuidance reuses the parked revision's retained answer.
// Refusing Validate must not make the owner search for its blocking decision.
func (rt *validateRuntimeState) implementPrerequisiteGuidance(ctx context.Context) (string, error) {
	feature, err := rt.opts.DB.GetManagedFeature(ctx, rt.session.TaskRef)
	if err != nil {
		return "", err
	}
	rows, err := rt.opts.DB.ListImplementStories(ctx, rt.session.TaskRef, int64(feature.Generation))
	if err != nil {
		return "", err
	}
	revisions, err := rt.opts.DB.ListApprovedStoryRevisions(ctx, rt.session.TaskRef, feature.Generation)
	if err != nil {
		return "", err
	}
	a := ImplementAuthority{Parent: rt.result.Ref, Generation: int64(feature.Generation)}
	for _, rev := range revisions {
		a.Stories = append(a.Stories, ImplementStory{Key: rev.Key, IssueNumber: rev.IssueNumber})
	}
	notes, err := implementParkNotes(ctx, rt.opts.DB, a, rows)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for _, row := range rows {
		if row.State != store.ImplementParked {
			continue
		}
		story, ok := a.story(row.Key)
		if !ok {
			continue
		}
		var link string
		if note, found := notes[row.Key]; found {
			target := a.Parent
			target.Issue = story.IssueNumber
			link, err = rt.opts.ResearchReporter.observedDecisionURL(ctx, target, note.CommentID)
			if err != nil {
				return "", err
			}
		}
		safe, err := rt.opts.ResearchReporter.renderer.RenderReportDelivery(ctx, row.Reason, "")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&text, "\n\n%s is parked: %s.", row.Key, implementInline(a.Parent.Forge == "linear", safe.String()))
		if link != "" {
			text.WriteString(" Read the [blocking decision](" + link + ")")
		} else {
			text.WriteString(" Its confirmed blocking report destination is unavailable")
		}
		text.WriteString("; reply on this parent issue naming " + row.Key + " with the requested decision or changed guidance. That reply permits the existing restart path; dependent stories still wait until prerequisites merge, and it does not approve delivery.")
	}
	if text.Len() == 0 {
		text.WriteString("\n\nNo parked-story decision is retained here. Running work and queued merges continue through normal admission; no owner reply can mark them merged.")
		link, err := rt.opts.ResearchReporter.workflowDecisionURL(ctx, a.Parent, rt.session.ID, report.ReplyGraphOwnerWaitRefused)
		if err != nil {
			return "", err
		}
		if link != "" {
			text.WriteString(" Resolve the [current Implement coordinator question](" + link + ") first; its answer permits the coordinator to retry the retained stopped step.")
		} else {
			text.WriteString(" No confirmed current coordinator decision destination is available from retained facts. A stopped coordinator must be resolved before Validate can be admitted.")
		}
	}
	return text.String(), nil
}
