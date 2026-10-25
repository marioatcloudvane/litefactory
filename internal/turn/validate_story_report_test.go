package turn

import (
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
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
	for _, verdict := range []string{"pass -- <what you checked>", "fail -- <what you saw>"} {
		command := ValidateCheckCommand + " " + st.Revision + " story-002:AC-002 " + verdict
		if strings.Count(first.Body, command) != 1 {
			t.Fatalf("each valid check command is offered once: %s", first.Body)
		}
		if _, err := ParseValidateCommand(command); err != nil {
			t.Fatal(err)
		}
	}
	parent := o.forge.bodies("**Validate report**")
	if len(parent) != 1 || !strings.Contains(first.Body, "]("+parent[0].HTMLURL+")") || !strings.HasPrefix(first.Body, lightYellow) {
		t.Errorf("checks route to exact parent report and story is yellow: %s", first.Body)
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
	// #971: the failed story write never suppresses the parent report.
	if n := len(o.forge.bodies("**Validate report**")); n != 1 {
		t.Fatalf("the parent report is posted despite the story failure: %d", n)
	}
	if r, err := o.db.GetValidateReport(t.Context(), o.instance.ID); err != nil || r.CommentID == 0 {
		t.Fatalf("the report row keeps the comment id: %+v %v", r, err)
	}
	o.forge.updateErr = errors.New("GitHub is down")
	if _, err := o.b.RefreshValidateReport(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID}); err == nil || !strings.Contains(err.Error(), "GitHub is down") {
		t.Fatalf("the story failure is surfaced: %v", err)
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
	if n := len(o.forge.bodies("**Validate report**")); n != 1 {
		t.Fatalf("the parent report is edited in place: %d", n)
	}
}

// storyFindingsState holds n blocking findings of story-002 that need the
// owner's choice, each with summary text.
func storyFindingsState(n int, summary string) ValidateState {
	st := ValidateState{Revision: "r1", Commit: strings.Repeat("c", 40)}
	for i := range n {
		name := fmt.Sprintf("F-%03d", i+1)
		st.Findings = append(st.Findings, ValidateFinding{ID: "story-002:" + name, Story: "story-002", Name: name, Role: "code", Status: "fail", Summary: summary, State: findingOpen, Blocking: true})
	}
	return st
}

func renderStoryDelivery(t *testing.T, body string) {
	t.Helper()
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.RenderReportDelivery(t.Context(), body, "<!-- lf:validate-story -->"); err != nil {
		t.Fatalf("the story comment is not deliverable: %v", err)
	}
}

// #1123 c4: a finding summary within the bound appears whole under Blocks and
// Your part; a longer one ends at a sentence with "(text shortened)".
func TestValidateStoryCommentShowsWholeFindingText(t *testing.T) {
	text := longFindingText(1500)
	body := renderValidateStoryComment(storyFindingsState(1, text), ValidateIssueRef{Key: "story-002"}, 843)
	if strings.Count(html.UnescapeString(body), text) != 2 || strings.Contains(body, "...") {
		t.Fatalf("the whole summary under Blocks and Your part:\n%s", body)
	}
	long := longFindingText(10000)
	body = renderValidateStoryComment(storyFindingsState(1, long), ValidateIssueRef{Key: "story-002"}, 843)
	want := shortenFindingText(long, validateFindingTextLimit)
	if strings.Count(html.UnescapeString(body), want) != 2 || !strings.Contains(body, "."+findingTextShortened) {
		t.Fatalf("the long summary is cut at a sentence end with the note:\n%s", body)
	}
}

// #1123 c6: many long findings still fit the comment limit and each says its
// text was shortened.
func TestValidateStoryCommentWithManyLongFindingsFits(t *testing.T) {
	body := renderValidateStoryComment(storyFindingsState(30, longFindingText(10000)), ValidateIssueRef{Key: "story-002"}, 843)
	if len(body) > github.CommentSizeLimit-validateStoryCommentReserve {
		t.Fatalf("body of %d bytes exceeds the budget", len(body))
	}
	for i := range 30 {
		name := fmt.Sprintf("F-%03d", i+1)
		block := "- Blocking finding `" + name + "` failed: "
		at := strings.Index(body, block)
		if at < 0 {
			t.Fatalf("%s is not listed with its text", name)
		}
		line, _, _ := strings.Cut(body[at:], "\n")
		if !strings.Contains(line, findingTextShortened) {
			t.Fatalf("%s does not say its text was shortened: %s", name, line)
		}
	}
	if strings.Count(html.UnescapeString(body), findingTextShortened) != 60 {
		t.Fatalf("every Blocks and Your part entry is shortened: %d", strings.Count(html.UnescapeString(body), findingTextShortened))
	}
	renderStoryDelivery(t, body)
}

// #1123 c9: beyond the 512-byte floor the comment still fits; later findings
// are listed by ID and command without text, and when even that does not fit
// (500 findings) the rest are counted in a closing line.
func TestValidateStoryCommentBeyondTheFloorStillNamesEveryFinding(t *testing.T) {
	for _, n := range []int{60, 150, 500} {
		body := renderValidateStoryComment(storyFindingsState(n, longFindingText(10000)), ValidateIssueRef{Key: "story-002"}, 843)
		if len(body) > github.CommentSizeLimit-validateStoryCommentReserve {
			t.Fatalf("%d findings: body of %d bytes exceeds the budget", n, len(body))
		}
		if full := renderValidateStoryCommentBound(storyFindingsState(n, longFindingText(10000)), ValidateIssueRef{Key: "story-002"}, "#843", validateStoryTextFloor, 0); len(full) <= github.CommentSizeLimit-validateStoryCommentReserve {
			t.Fatalf("%d findings fit at the floor; the case does not go beyond it", n)
		}
		listed := strings.Count(body, "- Blocking finding `") + strings.Count(body, "needs your choice")
		omitted := 0
		if i := strings.Index(body, " more findings are not listed here; see the parent issue's Validate report for their commands."); i >= 0 {
			start := strings.LastIndex(body[:i], "\n") + 1
			var err error
			omitted, err = strconv.Atoi(body[start:i])
			if err != nil {
				t.Fatalf("closing line: %v", err)
			}
		}
		if n == 500 && omitted == 0 {
			t.Fatal("500 findings do not reach the closing line")
		}
		if listed+omitted != 2*n {
			t.Fatalf("%d findings: listed %d and omitted %d do not account for every entry", n, listed, omitted)
		}
		if omitted == 0 {
			if !strings.Contains(body, findingTextShortened) {
				t.Fatalf("%d findings: earlier findings keep their shortened text", n)
			}
			last := fmt.Sprintf("- `F-%03d` (blocking finding) needs your choice\n  `%s r1 story-002:F-%03d ", n, ValidateFindingCommand, n)
			if !strings.Contains(body, last) {
				t.Fatalf("%d findings: the last is listed by ID and command without text:\n%s", n, body[max(0, len(body)-1500):])
			}
		} else if !strings.Contains(body, "` failed. It clears") {
			t.Fatalf("%d findings: later findings are listed without text:\n%s", n, body[max(0, len(body)-1500):])
		}
		renderStoryDelivery(t, body)
	}
}
