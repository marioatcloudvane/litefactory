package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

const storyBody = "## User Story\nThe owner repairs it.\n\n## Acceptance Criteria\n- [ ] AC-001: the repair runs\n- [ ] AC-002: the page looks right\n\n## Tasks\nDo it.\n"

// checkOnlyReview satisfies everything except AC-002, which awaits the owner.
func checkOnlyReview(req ValidateReviewRequest) (*agentresult.Result, error) {
	res := goodReview(req)
	for si := range res.Payload.Validate.Stories {
		s := &res.Payload.Validate.Stories[si]
		for i := range s.Criteria {
			if s.Criteria[i].Name == "AC-002" {
				s.Criteria[i].Status = "unverified"
			}
		}
	}
	return res, nil
}

// storyOwnerFixture is the owner fixture with story-002 published as sub-issue
// #520 carrying the frozen approved body.
func storyOwnerFixture(t *testing.T) *ownerFixture {
	t.Helper()
	o := newOwnerFixture(t)
	o.target.Stories = []ValidateIssueRef{{Number: ownerStoryNum, ID: 5200, Key: "story-002", Title: "Repair", Revision: strings.Repeat("ab", 32), Body: storyBody}}
	o.forge.storyIssues[ownerStoryNum] = github.Issue{ID: 5200, Number: ownerStoryNum, Title: "Repair", Body: storyBody}
	o.reviewers.run = checkOnlyReview
	return o
}

func (o *ownerFixture) storyBody() string {
	o.t.Helper()
	issue, err := o.forge.GetIssue(o.t.Context(), github.Repo{}, ownerStoryNum)
	if err != nil {
		o.t.Fatal(err)
	}
	return issue.Body
}

// #571 BC-006, BC-007: satisfied criteria are ticked and the rest of the body is
// unchanged; the sub-issue has one Validate comment with one command template
// per open item, edited in place to "nothing to do" after the owner's check.
func TestValidateStorySubIssueTicksAndOneComment(t *testing.T) {
	o := storyOwnerFixture(t)
	o.toOwner()
	st := o.state()
	want := strings.Replace(storyBody, "- [ ] AC-001", "- [x] AC-001", 1)
	if got := o.storyBody(); got != want {
		t.Fatalf("AC-001 ticked, AC-002 not, rest unchanged:\n%s", got)
	}
	comments := o.forge.commentsOn(ownerStoryNum)
	if len(comments) != 1 {
		t.Fatalf("one comment on the sub-issue: %d", len(comments))
	}
	first := comments[0]
	template := "`" + ValidateCheckCommand + " " + st.Revision + " story-002:AC-002 pass|fail -- <evidence>`"
	if strings.Count(first.Body, template) != 1 || strings.Count(first.Body, ValidateCheckCommand) != 1 || strings.Contains(first.Body, ValidateFindingCommand) {
		t.Fatalf("one template for the open check:\n%s", first.Body)
	}
	if !strings.Contains(first.Body, "on the parent #497") || !strings.HasPrefix(first.Body, lightYellow) {
		t.Errorf("commands are given on the parent and the story is yellow:\n%s", first.Body)
	}

	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- opened it, fine"))
	comments = o.forge.commentsOn(ownerStoryNum)
	if len(comments) != 1 || comments[0].ID != first.ID {
		t.Fatalf("the same comment is edited in place: %+v", comments)
	}
	if !strings.Contains(comments[0].Body, "nothing to do") || strings.Contains(comments[0].Body, ValidateCheckCommand) || !strings.HasPrefix(comments[0].Body, lightGreen) {
		t.Fatalf("nothing to do after the pass:\n%s", comments[0].Body)
	}
	want = strings.Replace(want, "- [ ] AC-002", "- [x] AC-002", 1)
	if got := o.storyBody(); got != want {
		t.Fatalf("the owner-passed criterion is ticked:\n%s", got)
	}
	if n := len(o.forge.bodies("**Validate report**")); n != 1 {
		t.Fatalf("one parent report: %d", n)
	}
}

// #571 BC-008: a failed story write never fails the step, leaves the report row
// unsaved for the new state and is retried on the next refresh.
func TestValidateStorySubIssueWriteRetriedOnNextRefresh(t *testing.T) {
	o := storyOwnerFixture(t)
	o.forge.updateErr = errors.New("GitHub is down")
	o.toOwner()
	st := o.state()
	if got := o.storyBody(); got != storyBody {
		t.Fatalf("nothing ticked while the write failed:\n%s", got)
	}
	if r, err := o.db.GetValidateReport(t.Context(), o.instance.ID); err == nil && r.Revision == st.Revision && r.Digest != "" {
		t.Fatalf("the report row is not saved for the new state: %+v", r)
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	changed, err := o.b.RefreshValidateReport(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID})
	if err != nil || !changed {
		t.Fatalf("the next refresh catches up: %v %v", changed, err)
	}
	if got := o.storyBody(); !strings.Contains(got, "- [x] AC-001") || !strings.Contains(got, "- [ ] AC-002") {
		t.Fatalf("the next refresh ticks the body:\n%s", got)
	}
	if len(o.forge.commentsOn(ownerStoryNum)) != 1 {
		t.Fatal("one sub-issue comment")
	}
}
