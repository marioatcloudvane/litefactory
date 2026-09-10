package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestParseOwnerException(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"criterion", "/waive 1 criterion:US-1:AC-1 -- no longer relevant", true},
		{"story", "/waive 1 story:US-1 -- risk accepted", true},
		{"review", "/waive 1 review:US-1:code -- risk accepted", true},
		{"tests", "/waive 1 tests:US-1 -- risk accepted", true},
		{"all", "/waive 1 all -- risk accepted", true},
		{"readiness", "/waive 1 readiness -- risk accepted", true},
		{"missing reason", "/waive 1 all", false},
		{"empty reason", "/waive 1 all -- ", false},
		{"free prose", "please waive everything", false},
		{"invalid generation", "/waive 0 all -- reason", false},
		{"unknown selector", "/waive 1 merge -- reason", false},
		{"duplicate selector", "/waive 1 all,all -- reason", false},
		{"oversized reason", "/waive 1 all -- " + strings.Repeat("r", 4097), false},
		{"oversized command", strings.Repeat("r", 8193), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseOwnerException(tc.body)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v result=%+v error=%v", tc.valid, got, err)
			}
			if tc.valid && (got.Generation != 1 || got.Reason == "" || len(got.Selectors) != 1) {
				t.Fatalf("lost explicit command scope: %+v", got)
			}
		})
	}
}

type exceptionTestForge struct{ *acceptanceTestForge }

func (f *exceptionTestForge) PostComment(ctx context.Context, r github.Repo, n int, b redact.Body) (github.Comment, error) {
	if n == testIssue {
		return f.progressForge.PostComment(ctx, r, n, b)
	}
	return f.acceptanceTestForge.PostComment(ctx, r, n, b)
}
func (f *exceptionTestForge) ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error) {
	var out []github.Issue
	for _, i := range f.children {
		out = append(out, i)
	}
	return out, nil
}
func ownerExceptionFixture(t *testing.T, status string) (*fixture, *exceptionTestForge, store.ValidationAssessment) {
	t.Helper()
	f, base, a := acceptanceFixture(t, status)
	forge := &exceptionTestForge{base}
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "exception-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
	return f, forge, a
}
func ownerExceptionControl(parent, body string) OwnerExceptionControl {
	return OwnerExceptionControl{Parent: parent, CommentID: 123, ActorID: 9, OwnerID: 9, Body: body}
}

func TestOwnerExceptionRejectsUnauthorizedAndAmbiguousControls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*OwnerExceptionControl)
	}{
		{"third party", func(c *OwnerExceptionControl) { c.ActorID = 8 }},
		{"zero identity", func(c *OwnerExceptionControl) { c.ActorID = 0; c.OwnerID = 0 }},
		{"no comment identity", func(c *OwnerExceptionControl) { c.CommentID = 0 }},
		{"free prose", func(c *OwnerExceptionControl) { c.Body = "I accept all risks" }},
		{"checkbox", func(c *OwnerExceptionControl) { c.Body = "- [x] AC-1" }},
		{"wrong generation", func(c *OwnerExceptionControl) { c.Body = "/waive 2 all -- accepted" }},
		{"unknown story", func(c *OwnerExceptionControl) { c.Body = "/waive 1 story:US-999 -- accepted" }},
		{"unknown criterion", func(c *OwnerExceptionControl) { c.Body = "/waive 1 criterion:US-1:AC-999 -- accepted" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, forge, a := ownerExceptionFixture(t, "unverified")
			c := ownerExceptionControl(a.Parent, "/waive 1 all -- accepted")
			tc.change(&c)
			err := f.opts.StoryProgress.ApplyOwnerException(t.Context(), c)
			if (tc.name == "third party" || tc.name == "zero identity" || tc.name == "no comment identity") && err == nil {
				t.Fatal("invalid identity accepted")
			}
			if err == nil && len(forge.comments[testIssue]) == 0 {
				t.Fatal("rejected scope lacked diagnostic")
			}
			if _, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID); err == nil {
				t.Fatal("invalid control persisted authority")
			}
			if forge.children[200].State != "open" || strings.Contains(forge.children[200].Body, "[x]") {
				t.Fatal("invalid control projected acceptance")
			}
		})
	}
}

func TestOwnerExceptionWaivesWithoutFabricatingEvidence(t *testing.T) {
	for _, status := range []string{"absent", "unverified", "failed"} {
		t.Run(status, func(t *testing.T) {
			f, forge, a := ownerExceptionFixture(t, status)
			if status != "absent" {
				retainAcceptanceFixture(t, f, a)
			}
			c := ownerExceptionControl(a.Parent, "/waive 1 story:US-1 -- requirement retired")
			mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
			d, err := f.db.LatestAcceptance(t.Context(), a.Parent, 1, "US-1")
			mustProgress(t, err)
			var findings store.AcceptanceFindings
			mustProgress(t, json.Unmarshal([]byte(d.FindingsJSON), &findings))
			want := "unverified"
			if status == "failed" {
				want = "unmet"
			}
			if len(findings.Criteria) != 1 || findings.Criteria[0].Status != want || findings.Criteria[0].WaiverReason != "requirement retired" {
				t.Fatalf("original fact lost: %+v", findings)
			}
			if !d.Accepted || d.ExceptionID == 0 || forge.children[200].State != "closed" || forge.children[201].State != "open" {
				t.Fatalf("independent explicit acceptance missing: %+v children=%+v", d, forge.children)
			}
			if !strings.Contains(forge.children[200].Body, "[x]") {
				t.Fatal("waived criterion not visibly checked")
			}
			comments := ""
			for _, comment := range forge.comments[200] {
				comments += comment.Body
			}
			for _, want := range []string{"OWNER-WAIVED", "requirement retired", want} {
				if !strings.Contains(comments, want) {
					t.Fatalf("exception fact %q missing: %s", want, comments)
				}
			}
			if !strings.HasSuffix(strings.Join(forge.order, ","), "report,body,close") {
				t.Fatalf("ordered effects violated: %v", forge.order)
			}
			e, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
			mustProgress(t, err)
			if e.ActorID != 9 || e.CommentID != 123 || !strings.Contains(e.RequestJSON, "requirement retired") || !strings.Contains(e.ExpansionJSON, "AC-1") {
				t.Fatalf("audit incomplete: %+v", e)
			}
		})
	}
}

func TestOwnerExceptionReplayPreservesExactReadinessScope(t *testing.T) {
	f, forge, a := ownerExceptionFixture(t, "unverified")
	forge.children[300] = github.Issue{ID: 3000, Number: 300, State: "open", Title: "unmanaged child"}
	c := ownerExceptionControl(a.Parent, "/waive 1 readiness -- defer attached work")
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	before, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	scope, err := f.db.ListReadinessExceptions(t.Context(), a.Parent, 1)
	mustProgress(t, err)
	found := false
	for _, o := range scope {
		if o.IssueID == 3000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmanaged child omitted: %+v", scope)
	}
	forge.children[301] = github.Issue{ID: 3010, Number: 301, State: "open"}
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "restarted"))
	mustProgress(t, err)
	mustProgress(t, p.ApplyOwnerException(t.Context(), c))
	after, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	if before.ExpansionJSON != after.ExpansionJSON || before.ID != after.ID {
		t.Fatal("replay widened persisted expansion")
	}
	scope, err = f.db.ListReadinessExceptions(t.Context(), a.Parent, 1)
	mustProgress(t, err)
	for _, o := range scope {
		if o.IssueID == 3010 {
			t.Fatal("new child silently waived on replay")
		}
	}
	if len(forge.comments[testIssue]) != 1 {
		t.Fatalf("duplicate exception reports: %+v", forge.comments[testIssue])
	}
	for _, child := range forge.children {
		if child.State != "open" {
			t.Fatal("readiness exception silently closes child")
		}
	}
}

func TestOwnerExceptionPartialCriterionDoesNotWaiveReviews(t *testing.T) {
	f, forge, a := ownerExceptionFixture(t, "absent")
	c := ownerExceptionControl(a.Parent, "/waive 1 criterion:US-1:AC-1 -- criterion retired")
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	d, err := f.db.LatestAcceptance(t.Context(), a.Parent, 1, "US-1")
	mustProgress(t, err)
	var findings store.AcceptanceFindings
	mustProgress(t, json.Unmarshal([]byte(d.FindingsJSON), &findings))
	if d.Accepted || forge.children[200].State != "open" {
		t.Fatal("criterion waiver waived missing reviews")
	}
	for _, r := range findings.Reviews {
		if r.WaiverReason != "" || r.Status != "unverified" {
			t.Fatalf("review evidence or authority fabricated: %+v", r)
		}
	}
	if !strings.Contains(forge.children[200].Body, "[x]") {
		t.Fatal("individually waived criterion not checked")
	}
	c.CommentID++
	c.Body = "/waive 1 story:US-1 -- accept missing reviews"
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	if forge.children[200].State != "closed" {
		t.Fatal("remaining explicit review waiver cannot complete acceptance")
	}
}

func TestOwnerExceptionAllDoesNotDispatchOrClaimIntegration(t *testing.T) {
	f, forge, a := ownerExceptionFixture(t, "absent")
	before, err := f.db.ListRuns(t.Context(), f.session.ID)
	mustProgress(t, err)
	c := ownerExceptionControl(a.Parent, "/waive 1 all -- accept all remaining substantive obligations")
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	after, err := f.db.ListRuns(t.Context(), f.session.ID)
	mustProgress(t, err)
	if len(before) != len(after) {
		t.Fatal("exception automatically dispatched mode")
	}
	for _, number := range []int{200, 201} {
		if forge.children[number].State != "closed" {
			t.Fatalf("bulk exception omitted story %d", number)
		}
	}
	for _, change := range forge.changes {
		if change != "200:closed" && change != "201:closed" {
			t.Fatalf("acceptance changed parent/delivery state: %s", change)
		}
	}
	for _, comments := range forge.comments {
		for _, comment := range comments {
			for _, claim := range []string{"merged into main successfully", "tests passed", "integration succeeded"} {
				if strings.Contains(strings.ToLower(comment.Body), claim) {
					t.Fatalf("fabricated operational fact %q: %s", claim, comment.Body)
				}
			}
		}
	}
}

func TestOwnerExceptionLargeReadinessScopeAcknowledgesAndReplays(t *testing.T) {
	f, forge, a := ownerExceptionFixture(t, "unverified")
	for i := 300; i < 1800; i++ {
		forge.children[i] = github.Issue{ID: int64(i * 10), Number: i, State: "open", Title: "attached follow-up"}
	}
	c := ownerExceptionControl(a.Parent, "/waive 1 readiness -- owner accepts all currently attached work")
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	record, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	if len(record.ExpansionJSON) <= github.CommentSizeLimit {
		t.Fatalf("fixture did not exceed comment limit: %d", len(record.ExpansionJSON))
	}
	if record.ReportCommentID == 0 || len(forge.comments[testIssue]) != 1 {
		t.Fatal("large scope acknowledgement remains pending")
	}
	comment := forge.comments[testIssue][0].Body
	if len(comment) > github.CommentSizeLimit || !strings.Contains(comment, "owner accepts all currently attached work") {
		t.Fatalf("bounded acknowledgement lost reason or exceeded limit: %d", len(comment))
	}
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	replay, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	if record.ExpansionJSON != replay.ExpansionJSON || len(forge.comments[testIssue]) != 1 {
		t.Fatal("bulk replay changed exact scope or duplicated acknowledgement")
	}
}
