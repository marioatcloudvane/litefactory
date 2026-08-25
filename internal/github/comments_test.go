package github_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
)

// TestListCommentsAfterFiltersAndOrders is AC-2.11. The watermark is a comment
// id, so "everything above the watermark, oldest first" is the whole contract:
// a comment below it has already been acted on, and processing two comments out
// of order would run a superseded instruction.
func TestListCommentsAfterFiltersAndOrders(t *testing.T) {
	t.Parallel()

	// Deliberately shuffled, including ids on both sides of the watermark.
	f := newFixture(t, respondJSON(http.StatusOK, `[
		{"id": 500, "body": "newest"},
		{"id": 100, "body": "already consumed"},
		{"id": 300, "body": "middle"},
		{"id": 200, "body": "the watermark itself"},
		{"id": 400, "body": "later"}
	]`))

	comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 200)
	if err != nil {
		t.Fatalf("ListCommentsAfter: %v", err)
	}

	var ids []int64
	for _, comment := range comments {
		ids = append(ids, comment.ID)
	}
	if fmt.Sprint(ids) != "[300 400 500]" {
		t.Fatalf("ids = %v, want [300 400 500]: ascending, and strictly above the watermark", ids)
	}
	for i := 1; i < len(comments); i++ {
		if comments[i].ID <= comments[i-1].ID {
			t.Errorf("comment %d (id %d) does not follow id %d", i, comments[i].ID, comments[i-1].ID)
		}
	}
}

// TestListCommentsAfterZeroReturnsEverything is what first-sight bootstrap
// needs (AC-2.10): the daemon reads the highest existing id and stores it as
// the watermark without processing any of them.
func TestListCommentsAfterZeroReturnsEverything(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[{"id": 1}, {"id": 2}, {"id": 3}]`))

	comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
	if err != nil {
		t.Fatalf("ListCommentsAfter: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("got %d comments, want all 3", len(comments))
	}
	if got := comments[len(comments)-1].ID; got != 3 {
		t.Errorf("highest id = %d, want 3", got)
	}
}

// TestListCommentsAfterOrdersAcrossPages is the case a server-side sort would
// hide: page one holds the low ids only if the API sorts, and this package does
// not depend on that.
func TestListCommentsAfterOrdersAcrossPages(t *testing.T) {
	t.Parallel()

	f := newFixture(t, pagedHandler([]string{
		`[{"id": 900}, {"id": 100}]`,
		`[{"id": 500}, {"id": 300}]`,
	}))

	comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 200)
	if err != nil {
		t.Fatalf("ListCommentsAfter: %v", err)
	}

	var ids []int64
	for _, comment := range comments {
		ids = append(ids, comment.ID)
	}
	if fmt.Sprint(ids) != "[300 500 900]" {
		t.Errorf("ids = %v, want [300 500 900]", ids)
	}
	if got := len(f.Requests()); got != 2 {
		t.Errorf("made %d requests, want 2", got)
	}
}

// TestCommentFields covers the decode, including the author identity the
// self-filter compares.
func TestCommentFields(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[{
		"id": 987654321,
		"body": "/spike add a rate limiter",
		"html_url": "https://github.com/acme/acme-api/issues/7#issuecomment-987654321",
		"user": {"id": 99, "login": "mario", "type": "User"},
		"created_at": "2026-08-23T11:00:00Z",
		"updated_at": "2026-08-23T11:05:00Z"
	}]`))

	comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
	if err != nil {
		t.Fatalf("ListCommentsAfter: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}

	comment := comments[0]
	if comment.ID != 987654321 {
		t.Errorf("ID = %d", comment.ID)
	}
	if comment.Body != "/spike add a rate limiter" {
		t.Errorf("Body = %q — comment bodies are stored verbatim, unredacted", comment.Body)
	}
	if comment.Author.ID != 99 || comment.Author.Login != "mario" {
		t.Errorf("Author = %+v, want id 99 / mario", comment.Author)
	}
	if comment.HTMLURL == "" {
		t.Error("HTMLURL is empty")
	}
	if comment.CreatedAt.IsZero() || comment.UpdatedAt.IsZero() {
		t.Errorf("timestamps = %s / %s, want both set", comment.CreatedAt, comment.UpdatedAt)
	}
}

// TestListCommentsPathAndQuery pins the request.
func TestListCommentsPathAndQuery(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[]`))
	if _, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0); err != nil {
		t.Fatalf("ListCommentsAfter: %v", err)
	}

	req := f.Only(t)
	if got, want := req.Path, "/repos/acme/acme-api/issues/7/comments"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := req.Query.Get("per_page"), "100"; got != want {
		t.Errorf("per_page = %q, want %q", got, want)
	}
}

// TestPostCommentSendsTheRedactedBody is AC-4.6 at this boundary. The body that
// goes on the wire is the string the redaction pass produced — not the caller's
// original text, which PostComment never sees.
func TestPostCommentSendsTheRedactedBody(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{
		"id": 555, "body": "posted",
		"user": {"id": 4242, "login": "litefactory-bot", "type": "Bot"},
		"html_url": "https://github.com/acme/acme-api/issues/7#issuecomment-555",
		"created_at": "2026-08-23T12:00:00Z", "updated_at": "2026-08-23T12:00:00Z"
	}`))

	const marker = "<!-- lf:status task=github:acme/acme-api#7 run=1 -->"
	report := marker + "\nRun finished. The token was ghp_0123456789abcdefghijklmnopqrstuvwxyz.\n"
	redacted := body(t, report)

	comment, err := f.Client.PostComment(t.Context(), testRepo, 7, redacted)
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if comment.ID != 555 {
		t.Errorf("returned comment id = %d, want 555 — this becomes runs.result_comment_id", comment.ID)
	}
	if !comment.Author.IsBot || comment.Author.ID != 4242 {
		t.Errorf("Author = %+v, want the bot account", comment.Author)
	}

	req := f.Only(t)
	if got, want := req.Method, http.MethodPost; got != want {
		t.Errorf("method = %s, want %s", got, want)
	}
	if got, want := req.Path, "/repos/acme/acme-api/issues/7/comments"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	var sent struct {
		Body string `json:"body"`
	}
	req.JSON(t, &sent)
	if sent.Body != redacted.String() {
		t.Errorf("sent body = %q, want the redacted body %q", sent.Body, redacted.String())
	}
	if !strings.HasPrefix(sent.Body, marker) {
		t.Errorf("sent body does not begin with the lf: marker: %q", sent.Body)
	}
	if strings.Contains(sent.Body, "ghp_0123456789") {
		t.Errorf("a token-shaped string reached the outbound body: %q", sent.Body)
	}
}

// TestPostCommentUsesTheBotCredential is AC-4.11. There is one posting path and
// it carries the bot token; `gh` is identity discovery only and Mario's SSH key
// pushes git. A report must be visibly from the bot.
func TestPostCommentUsesTheBotCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1}`))
	if _, err := f.Client.PostComment(t.Context(), testRepo, 7, body(t, "report")); err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if got, want := f.Only(t).Header.Get("Authorization"), "Bearer "+testToken; got != want {
		t.Errorf("Authorization = %q, want the bot credential", got)
	}
}

// TestPostCommentRefusesABodyThatDidNotComeFromApply is the runtime half of the
// compile-time guarantee. redact.Body cannot be constructed outside
// internal/redact, but redact.Body{} still compiles — and that zero value must
// be refused rather than posted as an empty comment.
func TestPostCommentRefusesABodyThatDidNotComeFromApply(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1}`))

	_, err := f.Client.PostComment(t.Context(), testRepo, 7, redact.Body{})
	if !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("PostComment error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "redact.Apply") {
		t.Errorf("error %q does not name what is missing", err)
	}
	if got := len(f.Requests()); got != 0 {
		t.Errorf("made %d requests with an unredacted body; want none", got)
	}
}

// TestPostCommentRefusesAnEmptyBody keeps silence from being posted as a
// comment: AC-4.1 wants exactly one report per run, and an empty one is not it.
func TestPostCommentRefusesAnEmptyBody(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1}`))

	_, err := f.Client.PostComment(t.Context(), testRepo, 7, body(t, ""))
	if !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("PostComment error = %v, want ErrInvalidArgument", err)
	}
	if got := len(f.Requests()); got != 0 {
		t.Errorf("made %d requests with an empty body; want none", got)
	}
}

// TestPostCommentRefusesAnOversizeBody catches the 422 before it is spent. The
// reporter budgets against CommentSizeLimit, so a body over it means the
// truncation ordering went wrong upstream and the run should be reported as
// failed rather than retried against a limit that will not move.
func TestPostCommentRefusesAnOversizeBody(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1}`))

	// Built with a limit above GitHub's, which is the only way to obtain a
	// valid Body that PostComment must still refuse.
	oversize := bodyWithLimit(t, strings.Repeat("a", github.CommentSizeLimit+1_000), github.CommentSizeLimit*4)
	if oversize.Len() <= github.CommentSizeLimit {
		t.Fatalf("the fixture body is %d bytes, which is not over the limit", oversize.Len())
	}

	_, err := f.Client.PostComment(t.Context(), testRepo, 7, oversize)
	if !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("PostComment error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "65536") {
		t.Errorf("error %q does not name the limit", err)
	}
	if got := len(f.Requests()); got != 0 {
		t.Errorf("made %d requests with an oversize body; want none", got)
	}
}

// TestPostCommentRejectsAResponseWithoutAnID guards the transaction in AC-4.10:
// result_comment_id and the watermark advance together, so a comment whose id
// is unknown must be an error rather than a zero written to the database.
func TestPostCommentRejectsAResponseWithoutAnID(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"body": "posted, apparently"}`))

	_, err := f.Client.PostComment(t.Context(), testRepo, 7, body(t, "report"))
	if !errors.Is(err, github.ErrUnexpectedStatus) {
		t.Fatalf("PostComment error = %v, want ErrUnexpectedStatus", err)
	}
}

// TestAddCommentReaction is AC-2.17's acknowledgement: 👀 on the instruction
// comment, before the run starts.
func TestAddCommentReaction(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1, "content": "eyes"}`))

	if err := f.Client.AddCommentReaction(t.Context(), testRepo, 987654321, github.ReactionEyes); err != nil {
		t.Fatalf("AddCommentReaction: %v", err)
	}

	req := f.Only(t)
	if got, want := req.Method, http.MethodPost; got != want {
		t.Errorf("method = %s, want %s", got, want)
	}
	if got, want := req.Path, "/repos/acme/acme-api/issues/comments/987654321/reactions"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}

	var sent struct {
		Content string `json:"content"`
	}
	req.JSON(t, &sent)
	if sent.Content != "eyes" {
		t.Errorf("content = %q, want eyes", sent.Content)
	}
}

// TestAddCommentReactionIsIdempotent covers a replayed tick. GitHub answers 200
// rather than 201 when the reaction is already there, and treating that as a
// failure would put a spurious error in every log.
func TestAddCommentReactionIsIdempotent(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `{"id": 1, "content": "eyes"}`))
	if err := f.Client.AddCommentReaction(t.Context(), testRepo, 99, github.ReactionEyes); err != nil {
		t.Fatalf("AddCommentReaction on an existing reaction: %v", err)
	}
}

// TestReactionFailureDoesNotPoisonTheClient is the half of AC-2.17 this package
// owns: "reaction failure is logged and does not abort the turn". The failure
// is reported as a plain error the caller can log and drop, it carries no state
// the turn would need, and the very next call on the same client works.
func TestReactionFailureDoesNotPoisonTheClient(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/reactions") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message": "Resource not accessible by personal access token"}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id": 555}`)
	})

	err := f.Client.AddCommentReaction(t.Context(), testRepo, 99, github.ReactionEyes)
	if err == nil {
		t.Fatal("a refused reaction returned no error")
	}
	if !errors.Is(err, github.ErrForbidden) {
		t.Errorf("errors.Is(%v, ErrForbidden) = false", err)
	}

	// The turn carries on: the report still posts, on the same client.
	comment, postErr := f.Client.PostComment(t.Context(), testRepo, 7, body(t, "report"))
	if postErr != nil {
		t.Fatalf("PostComment after a failed reaction: %v", postErr)
	}
	if comment.ID != 555 {
		t.Errorf("comment id = %d, want 555", comment.ID)
	}

	requests := f.Requests()
	if len(requests) != 2 || !strings.HasSuffix(requests[0].Path, "/reactions") {
		t.Fatalf("requests = %+v, want the reaction attempt followed by the report", requests)
	}
}

// TestAddCommentReactionValidatesItsArguments keeps a bad reaction name from
// becoming a 422 that looks like a permissions problem.
func TestAddCommentReactionValidatesItsArguments(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1}`))

	if err := f.Client.AddCommentReaction(t.Context(), testRepo, 99, "thumbsup"); !errors.Is(err, github.ErrInvalidArgument) {
		t.Errorf("an unknown reaction: error = %v, want ErrInvalidArgument", err)
	}
	if err := f.Client.AddCommentReaction(t.Context(), testRepo, 0, github.ReactionEyes); !errors.Is(err, github.ErrInvalidArgument) {
		t.Errorf("a zero comment id: error = %v, want ErrInvalidArgument", err)
	}
	if err := f.Client.AddCommentReaction(t.Context(), testRepo, -5, github.ReactionEyes); !errors.Is(err, github.ErrInvalidArgument) {
		t.Errorf("a negative comment id: error = %v, want ErrInvalidArgument", err)
	}
	if got := len(f.Requests()); got != 0 {
		t.Errorf("made %d requests with refused arguments; want none", got)
	}
}

// TestEveryReactionNameIsAccepted keeps the allowlist honest: only M0's 👀 is
// used, but a name GitHub accepts must not be refused here.
func TestEveryReactionNameIsAccepted(t *testing.T) {
	t.Parallel()

	reactions := []github.ReactionContent{
		github.ReactionEyes, github.ReactionPlusOne, github.ReactionMinusOne,
		github.ReactionLaugh, github.ReactionConfused, github.ReactionHeart,
		github.ReactionHooray, github.ReactionRocket,
	}

	f := newFixture(t, respondJSON(http.StatusCreated, `{"id": 1}`))
	for _, reaction := range reactions {
		if err := f.Client.AddCommentReaction(t.Context(), testRepo, 99, reaction); err != nil {
			t.Errorf("AddCommentReaction(%q): %v", reaction, err)
		}
	}
	if got, want := len(f.Requests()), len(reactions); got != want {
		t.Errorf("made %d requests, want %d", got, want)
	}
}

// TestListCommentedIssuesDedupesAndSorts covers what the daemon actually needs
// from the repo-level listing: which issues to look at, once each, in a stable
// order. Several comments on one issue are one issue's worth of work.
func TestListCommentedIssuesDedupesAndSorts(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[
		{"id": 1, "issue_url": "https://api.github.com/repos/acme/api/issues/9"},
		{"id": 2, "issue_url": "https://api.github.com/repos/acme/api/issues/3"},
		{"id": 3, "issue_url": "https://api.github.com/repos/acme/api/issues/9"},
		{"id": 4, "issue_url": "https://api.github.com/repos/acme/api/issues/3"}
	]`))

	numbers, err := f.Client.ListCommentedIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListCommentedIssues: %v", err)
	}
	if fmt.Sprint(numbers) != "[3 9]" {
		t.Fatalf("numbers = %v, want [3 9]", numbers)
	}
}

// TestListCommentedIssuesDropsUnusableIssueURLs: the issue number is the only
// thing tying a comment to work. A URL that does not end in one is dropped,
// because the alternative — guessing — attributes a run to the wrong issue.
func TestListCommentedIssuesDropsUnusableIssueURLs(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[
		{"id": 1, "issue_url": ""},
		{"id": 2, "issue_url": "https://api.github.com/repos/acme/api/issues/not-a-number"},
		{"id": 3, "issue_url": "https://api.github.com/repos/acme/api/issues/0"},
		{"id": 4, "issue_url": "https://api.github.com/repos/acme/api/issues/12"}
	]`))

	numbers, err := f.Client.ListCommentedIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListCommentedIssues: %v", err)
	}
	if fmt.Sprint(numbers) != "[12]" {
		t.Fatalf("numbers = %v, want [12]", numbers)
	}
}

// TestListCommentedIssuesPairsTheSortWithTheCursor: `since` on this endpoint
// filters on the comment's update time, so a truncated walk has to lose the
// newest rather than the oldest.
func TestListCommentedIssuesPairsTheSortWithTheCursor(t *testing.T) {
	t.Parallel()

	var query url.Values
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		respondJSON(http.StatusOK, `[]`)(w, r)
	})

	since := time.Date(2026, 8, 25, 11, 40, 0, 0, time.UTC)
	if _, err := f.Client.ListCommentedIssues(t.Context(), testRepo, since); err != nil {
		t.Fatalf("ListCommentedIssues: %v", err)
	}
	for key, want := range map[string]string{
		"sort":      "updated",
		"direction": "asc",
		"since":     "2026-08-25T11:40:00Z",
	} {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
