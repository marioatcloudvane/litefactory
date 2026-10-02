package turn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
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

// flakyParentForge fails the next failPosts parent-issue comments, as a forge
// outage would, and otherwise delegates to the wrapped forge.
type flakyParentForge struct {
	*exceptionTestForge
	failPosts int
}

func (f *flakyParentForge) PostComment(ctx context.Context, r github.Repo, n int, b redact.Body) (github.Comment, error) {
	if n == testIssue && f.failPosts > 0 {
		f.failPosts--
		return github.Comment{}, errors.New("forge unavailable")
	}
	return f.exceptionTestForge.PostComment(ctx, r, n, b)
}

// noFeatureExceptionFixture is a parent with no managed feature at all.
func noFeatureExceptionFixture(t *testing.T) (*fixture, *progressForge, *flakyParentForge, *report.Reporter) {
	t.Helper()
	f := newFixture(t, nil)
	base := &progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}
	forge := &flakyParentForge{exceptionTestForge: &exceptionTestForge{&acceptanceTestForge{progressForge: base, db: f.db}}}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	p, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(t.TempDir(), "no-feature-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
	return f, base, forge, renderer
}

func exceptionReplies(comments []github.Comment, marker string) int {
	n := 0
	for _, c := range comments {
		if c.Author.ID == 9 && exactProgressMarker(c.Body, marker) {
			n++
		}
	}
	return n
}

// #404 case 1: an exception on a parent without a managed feature is refused
// with one reply instead of a raw ErrNotFound that wedges the queue.
func TestOwnerExceptionWithoutManagedFeatureRepliesOnce(t *testing.T) {
	f, base, forge, renderer := noFeatureExceptionFixture(t)
	c := ownerExceptionControl(f.session.TaskRef, "/waive 1 all -- accepted")
	if err := f.opts.StoryProgress.ApplyOwnerException(t.Context(), c); err != nil {
		t.Fatalf("missing managed feature stayed pending: %v", err)
	}
	marker := report.OwnerExceptionNotice{Parent: c.Parent, CommentID: c.CommentID}.Marker()
	if len(base.comments[testIssue]) != 1 || exceptionReplies(base.comments[testIssue], marker) != 1 {
		t.Fatalf("want exactly one refusal reply: %+v", base.comments[testIssue])
	}
	if !strings.Contains(base.comments[testIssue][0].Body, "no approved separated workflow") {
		t.Fatalf("refusal lacks its reason: %s", base.comments[testIssue][0].Body)
	}
	if _, err := f.db.GetOwnerException(t.Context(), c.Parent, c.CommentID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refused exception persisted authority: %v", err)
	}
	decisions, err := f.db.ListPendingAcceptance(t.Context())
	mustProgress(t, err)
	if _, err := f.db.LatestAcceptance(t.Context(), c.Parent, 1, "US-1"); !errors.Is(err, store.ErrNotFound) || len(decisions) != 0 {
		t.Fatalf("refused exception recorded acceptance: %v %+v", err, decisions)
	}
	restarted, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(t.TempDir(), "restarted"))
	mustProgress(t, err)
	mustProgress(t, restarted.ApplyOwnerException(t.Context(), c))
	if len(base.comments[testIssue]) != 1 {
		t.Fatalf("replay duplicated the refusal: %+v", base.comments[testIssue])
	}
}

// If the case 1 refusal cannot be posted, the comment stays pending, and the
// retry posts exactly one reply.
func TestOwnerExceptionWithoutManagedFeatureStaysPendingUntilReplyPosts(t *testing.T) {
	f, base, forge, _ := noFeatureExceptionFixture(t)
	forge.failPosts = 1
	c := ownerExceptionControl(f.session.TaskRef, "/waive 1 all -- accepted")
	if err := f.opts.StoryProgress.ApplyOwnerException(t.Context(), c); err == nil {
		t.Fatal("unposted refusal was treated as handled")
	}
	if len(base.comments[testIssue]) != 0 {
		t.Fatalf("failed post left a reply: %+v", base.comments[testIssue])
	}
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	if len(base.comments[testIssue]) != 1 {
		t.Fatalf("retry did not post exactly one reply: %+v", base.comments[testIssue])
	}
}

// Temporary store failures are still returned so the comment stays pending,
// and nothing is posted.
func TestOwnerExceptionTemporaryStoreFailuresStayPending(t *testing.T) {
	for _, table := range []string{"managed_features", "owner_exceptions"} {
		t.Run(table, func(t *testing.T) {
			f, base, _, _ := noFeatureExceptionFixture(t)
			probe, err := sql.Open("sqlite", store.DSN(f.db.Path()))
			mustProgress(t, err)
			defer func() { mustProgress(t, probe.Close()) }()
			_, err = probe.ExecContext(t.Context(), fmt.Sprintf("ALTER TABLE %s RENAME TO %s_unavailable", table, table))
			mustProgress(t, err)
			c := ownerExceptionControl(f.session.TaskRef, "/waive 1 all -- accepted")
			err = f.opts.StoryProgress.ApplyOwnerException(t.Context(), c)
			if err == nil || errors.Is(err, store.ErrNotFound) {
				t.Fatalf("store failure was not kept temporary: %v", err)
			}
			if len(base.comments[testIssue]) != 0 {
				t.Fatalf("temporary failure posted a reply: %+v", base.comments[testIssue])
			}
		})
	}
}

func acceptanceSnapshot(t *testing.T, f *fixture, parent string) []string {
	t.Helper()
	var out []string
	for _, key := range []string{"US-1", "US-2"} {
		d, err := f.db.LatestAcceptance(t.Context(), parent, 1, key)
		out = append(out, fmt.Sprintf("%+v|%v", d, err))
	}
	return out
}

func editedExceptionFixture(t *testing.T) (*fixture, *progressForge, *flakyParentForge, store.ValidationAssessment) {
	t.Helper()
	f, base, a := ownerExceptionFixture(t, "unverified")
	retainAcceptanceFixture(t, f, a)
	forge := &flakyParentForge{exceptionTestForge: base}
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "edited-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
	return f, base.progressForge, forge, a
}

// #404 case 2: an edited source comment of a retained exception gets one
// edit-refusal reply and the retained exception stays unchanged.
func TestOwnerExceptionEditedCommentRefusedOnce(t *testing.T) {
	f, base, _, a := editedExceptionFixture(t)
	c := ownerExceptionControl(a.Parent, "/waive 1 story:US-1 -- requirement retired")
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	before, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	decisions := acceptanceSnapshot(t, f, a.Parent)
	posted := len(base.comments[testIssue])
	edited := c
	edited.Body = "/waive 1 all -- everything instead"
	if err := f.opts.StoryProgress.ApplyOwnerException(t.Context(), edited); err != nil {
		t.Fatalf("edited comment stayed pending: %v", err)
	}
	marker := report.OwnerExceptionNotice{Parent: c.Parent, CommentID: c.CommentID, EditRefused: true}.Marker()
	if len(base.comments[testIssue]) != posted+1 || exceptionReplies(base.comments[testIssue], marker) != 1 {
		t.Fatalf("want exactly one edit-refusal reply: %+v", base.comments[testIssue])
	}
	after, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(decisions, acceptanceSnapshot(t, f, a.Parent)) {
		t.Fatalf("edit changed retained authority: before=%+v after=%+v", before, after)
	}
	if base.children[201].State != "open" {
		t.Fatal("edited body widened the retained exception")
	}
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), edited))
	if len(base.comments[testIssue]) != posted+1 {
		t.Fatalf("second edited replay posted again: %+v", base.comments[testIssue])
	}
}

// If the edit refusal cannot be posted, the comment stays pending.
func TestOwnerExceptionEditedCommentStaysPendingUntilReplyPosts(t *testing.T) {
	f, base, forge, a := editedExceptionFixture(t)
	c := ownerExceptionControl(a.Parent, "/waive 1 story:US-1 -- requirement retired")
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), c))
	posted := len(base.comments[testIssue])
	edited := c
	edited.Body = "/waive 1 all -- everything instead"
	forge.failPosts = 1
	if err := f.opts.StoryProgress.ApplyOwnerException(t.Context(), edited); err == nil {
		t.Fatal("unposted edit refusal was treated as handled")
	}
	if len(base.comments[testIssue]) != posted {
		t.Fatalf("failed post left a reply: %+v", base.comments[testIssue])
	}
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), edited))
	marker := report.OwnerExceptionNotice{Parent: c.Parent, CommentID: c.CommentID, EditRefused: true}.Marker()
	if len(base.comments[testIssue]) != posted+1 || exceptionReplies(base.comments[testIssue], marker) != 1 {
		t.Fatalf("retry did not post exactly one edit refusal: %+v", base.comments[testIssue])
	}
}

// An unconfirmed acknowledgement of the retained exception is completed from
// the retained record before an edited replay is treated as handled.
func TestOwnerExceptionEditedCommentCompletesRetainedAcknowledgement(t *testing.T) {
	f, base, forge, a := editedExceptionFixture(t)
	c := ownerExceptionControl(a.Parent, "/waive 1 story:US-1 -- requirement retired")
	forge.failPosts = 1
	if err := f.opts.StoryProgress.ApplyOwnerException(t.Context(), c); err == nil {
		t.Fatal("unposted acknowledgement was treated as handled")
	}
	retained, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	if retained.ReportCommentID != 0 {
		t.Fatalf("fixture acknowledgement unexpectedly confirmed: %+v", retained)
	}
	edited := c
	edited.Body = "/waive 1 all -- everything instead"
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), edited))
	after, err := f.db.GetOwnerException(t.Context(), a.Parent, c.CommentID)
	mustProgress(t, err)
	if after.ReportCommentID < 1 || after.RequestJSON != retained.RequestJSON || after.ExpansionJSON != retained.ExpansionJSON {
		t.Fatalf("retained acknowledgement not completed unchanged: %+v", after)
	}
	accepted := report.OwnerExceptionNotice{Parent: c.Parent, CommentID: c.CommentID}.Marker()
	refused := report.OwnerExceptionNotice{Parent: c.Parent, CommentID: c.CommentID, EditRefused: true}.Marker()
	if exceptionReplies(base.comments[testIssue], accepted) != 1 || exceptionReplies(base.comments[testIssue], refused) != 1 {
		t.Fatalf("want one acknowledgement and one edit refusal: %+v", base.comments[testIssue])
	}
	for _, comment := range base.comments[testIssue] {
		if exactProgressMarker(comment.Body, accepted) && (!strings.Contains(comment.Body, "requirement retired") || strings.Contains(comment.Body, "everything instead")) {
			t.Fatalf("acknowledgement not built from the retained record: %s", comment.Body)
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
