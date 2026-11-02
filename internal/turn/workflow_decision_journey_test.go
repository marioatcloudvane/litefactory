package turn

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// This journey follows the destination the owner actually receives. Rendering
// the abbreviated story alone cannot prove that omitted choices are reachable.
func TestWorkflowDecisionJourneyOversizedValidateGuidance(t *testing.T) {
	const count = 700
	o := storyOwnerFixture(t)
	o.opts.ResearchReporter.validateHead = func(context.Context, store.Session) (string, error) { return o.head(), nil }
	o.stories[0].Criteria = nil
	var story strings.Builder
	story.WriteString("## User Story\nReview every screen.\n\n## Acceptance Criteria\n")
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&story, "- [ ] AC-%03d: (Manual) inspect screen %d\n", i, i)
		o.stories[0].Criteria = append(o.stories[0].Criteria, fmt.Sprintf("AC-%03d", i))
	}
	story.WriteString("\n## Tasks\nInspect the screens.\n")
	o.target.Stories[0].Body = story.String()
	issue := o.forge.storyIssues[ownerStoryNum]
	issue.Body = story.String()
	o.forge.storyIssues[ownerStoryNum] = issue
	o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		res := goodReview(req)
		if req.Role == agent.ReviewRoleAcceptance {
			for i := range res.Payload.Validate.Stories {
				for j := range res.Payload.Validate.Stories[i].Criteria {
					res.Payload.Validate.Stories[i].Criteria[j].Status = "unverified"
				}
			}
		}
		return res, nil
	}
	o.toOwner()
	st := o.state()
	if len(st.Findings) < count {
		t.Fatalf("only %d findings retained", len(st.Findings))
	}
	parent := o.forge.bodies("**Validate report**")
	if len(parent) != 1 {
		t.Fatalf("one parent index required; got %d", len(parent))
	}
	storyComments := o.forge.commentsOn(ownerStoryNum)
	if len(storyComments) != 1 {
		t.Fatalf("one abbreviated story report required; got %d", len(storyComments))
	}
	if strings.Contains(storyComments[0].Body, fmt.Sprintf("%s %s story-002:AC-%03d pass", ValidateCheckCommand, st.Revision, count)) {
		t.Fatal("fixture did not omit the final check from the story comment")
	}
	// Start from the exact destination offered by the abbreviated story report.
	// Discovering a parent comment independently would hide a broken handoff.
	var offeredParentID int64
	for _, c := range o.forge.commentsOn(ownerParent) {
		if c.HTMLURL != "" && strings.Contains(storyComments[0].Body, "]("+c.HTMLURL+")") && strings.Contains(c.Body, "**Validate report**") {
			offeredParentID = c.ID
		}
	}
	if offeredParentID != parent[0].ID {
		t.Fatal("abbreviated story did not offer the exact current parent report destination")
	}
	waits := o.forge.bodies("Validate is waiting for you")
	if len(waits) != 1 || !strings.Contains(waits[0].Body, "]("+parent[0].HTMLURL+")") {
		t.Fatal("shortened owner wait did not offer the exact current parent report destination")
	}
	// Resolve only links offered by the parent. Unlinked provider comments do
	// not count as a complete destination even if their bytes were posted.
	reached := parent[0].Body
	var partBodies []string
	linkedParts := 0
	for _, c := range o.forge.commentsOn(ownerParent) {
		if c.ID == parent[0].ID || c.HTMLURL == "" || !strings.Contains(parent[0].Body, "]("+c.HTMLURL+")") {
			continue
		}
		if len(c.Body) > github.CommentSizeLimit {
			t.Fatalf("part %d exceeds provider limit", c.ID)
		}
		if !strings.Contains(c.Body, st.Revision) {
			t.Fatalf("part %d has no current revision", c.ID)
		}
		reached += "\n" + c.Body
		partBodies = append(partBodies, c.Body)
		linkedParts++
	}
	if linkedParts < 2 {
		t.Fatalf("beyond-parent-capacity guidance needs numbered linked parts; got %d", linkedParts)
	}
	if strings.Contains(parent[0].Body, "**Partial publication**") {
		t.Fatal("confirmed complete index still claims partial publication")
	}
	for i := 1; i <= count; i++ {
		target := fmt.Sprintf("story-002:AC-%03d", i)
		var itemPart string
		for _, body := range partBodies {
			if strings.Contains(body, "**story-002 · `"+target+"`**") {
				itemPart = body
				break
			}
		}
		if itemPart == "" {
			t.Fatalf("no offered part retains intact item %s", target)
		}
		_, item, _ := strings.Cut(itemPart, "**story-002 · `"+target+"`**")
		if item, _, _ = strings.Cut(item, "\n\n**story-002 · `"); !strings.Contains(item, ValidateCheckCommand) {
			t.Fatalf("item %s lost its check command: %q", target, item)
		}
		// #1314: each part states the consequences once, not once per item.
		for _, consequence := range []string{"owner supplies evidence", "pass settles a check", "fail keeps it blocking", "No check or choice permits push, merge or close"} {
			if !strings.Contains(itemPart, consequence) || strings.Count(itemPart, consequence) != 1 {
				t.Fatalf("offered destination lost consequence %q for %s", consequence, target)
			}
		}
		for _, verdict := range []string{"pass", "fail"} {
			command := fmt.Sprintf("%s %s %s %s --", ValidateCheckCommand, st.Revision, target, verdict)
			if !strings.Contains(reached, command) {
				t.Fatalf("offered destination lost current %s command for %s", verdict, target)
			}
		}
	}
	// An owner check changes only that evidence; it does not grant delivery.
	o.say(o.command(ValidateCheckCommand, "story-002:AC-001", "pass", "-- screen inspected"))
	after := o.state()
	if after.Ready || after.Effects.Push != nil || after.Effects.Merge != nil || after.Effects.Close != nil {
		t.Fatal("one check granted delivery or cleared the remaining checks")
	}
}
