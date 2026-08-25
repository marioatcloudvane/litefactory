package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
)

// CommentSizeLimit is the largest comment body GitHub accepts, in bytes.
// Anything longer comes back as a 422, so the reporter budgets against this
// with [redact.Budget] before it assembles a body (AC-4.8).
const CommentSizeLimit = 65536

// Comment is an issue comment.
type Comment struct {
	// ID is the forge's comment id. It is monotonic per issue, which is what
	// makes it usable as the watermark: a comment id greater than the
	// watermark is a comment litefactory has not consumed (AC-2.11).
	ID int64
	// Body is the comment text, verbatim and unredacted. S-7 redaction is
	// outbound-only; what arrives is stored and logged exactly as written.
	Body string
	// Author is who wrote it. Classification compares Author.ID, never
	// Author.Login (D-I).
	Author User
	// HTMLURL is the browser URL of the comment.
	HTMLURL string
	// CreatedAt and UpdatedAt are GitHub's timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListCommentsAfter returns the comments on an issue whose id is greater than
// afterID, in ascending id order (AC-2.11). afterID of 0 returns every comment,
// which is what first-sight bootstrap needs to find the highest existing id
// without processing anything (AC-2.10).
//
// The filter is applied here rather than by the API because GitHub's issue
// comments endpoint has no after-id cursor: its only filter is a `since`
// timestamp, and litefactory's watermark is an id. Ids are used deliberately —
// a timestamp cursor cannot separate two comments written in the same second,
// and an edited comment moves its timestamp but not its id.
//
// The ordering is imposed here rather than asked of the API. The per-issue
// comments endpoint takes no sort or direction parameter — only `since` — and
// returns comments oldest first by convention. Sorting the result explicitly
// costs nothing and removes the dependency on that convention: processing a
// `/spike` before the comment that supersedes it would be a silent, expensive
// bug.
func (c *Client) ListCommentsAfter(ctx context.Context, repo Repo, issue int, afterID int64) ([]Comment, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if err := checkIssueNumber(issue); err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("per_page", strconv.Itoa(perPage))

	endpoint := c.endpoint(issuePath(repo, issue)+"/comments", query)
	payloads, requests, err := fetchPages[commentPayload](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("github: list comments on %s#%d: %w", repo, issue, err)
	}

	comments := make([]Comment, 0, len(payloads))
	for i := range payloads {
		if payloads[i].ID <= afterID {
			continue
		}
		comments = append(comments, payloads[i].comment())
	}
	slices.SortFunc(comments, func(a, b Comment) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	c.logger().DebugContext(ctx, "github: listed issue comments",
		slog.String("repo", repo.String()),
		slog.Int("issue", issue),
		slog.Int64("after_id", afterID),
		slog.Int("requests", requests),
		slog.Int("comments", len(comments)),
	)
	return comments, nil
}

// ListCommentedIssues returns the numbers of the issues in repo carrying a
// comment at or after since, ascending and without duplicates.
//
// # Why discovery cannot be issue listing alone
//
// [Client.ListIssues] finds the issues whose `updated_at` moved, on the
// assumption that writing a comment moves it. Measured against
// marioatcloudvane/litefactory on 2026-08-25, it does not — not reliably, and
// not within any useful window:
//
//	13:39:26  daemon: first sight of #3, no comments yet
//	13:40:37  owner comments /research
//	13:42:25  owner comments /spike
//	13:42:26  tick since=11:41:56Z -> issues=0   (both comments are inside it)
//	   … 60 further ticks, every one issues=0 …
//	18:40:23  the issue is closed by hand; updated_at finally moves
//	18:40:49  tick -> issues=3, and the seven-hour-old comments are drained
//
// The single-issue endpoint does reflect comments in `updated_at`. The listing
// index evidently does not, so an owner comment on an otherwise untouched issue
// is invisible to a `since` walk over issues.
//
// This endpoint filters on the comments themselves, so it cannot miss one. It
// is one request per repository per tick, the same cost as the issue listing,
// and it carries no issue detail — only the issue each comment hangs off, which
// is all the caller needs to know where to look.
//
// The sort is paired with the cursor for the reason [Client.ListIssues] gives:
// `since` on this endpoint filters on the comment's update time, so ordering by
// the same field keeps a truncated walk losing the newest rather than the
// oldest. Pull request comments arrive here too — every pull request is an
// issue — and are not filtered out, because the payload cannot tell them apart.
// The caller drops them when it fetches the issue.
func (c *Client) ListCommentedIssues(ctx context.Context, repo Repo, since time.Time) ([]int, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", strconv.Itoa(perPage))
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339))
	}

	endpoint := c.endpoint(repoPath(repo)+"/issues/comments", query)
	payloads, requests, err := fetchPages[commentPayload](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("github: list the comments of %s: %w", repo, err)
	}

	seen := make(map[int]bool, len(payloads))
	numbers := make([]int, 0, len(payloads))
	malformed := 0
	for i := range payloads {
		number, ok := issueNumberFromURL(payloads[i].IssueURL)
		if !ok {
			malformed++
			continue
		}
		if seen[number] {
			continue
		}
		seen[number] = true
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)

	c.logger().InfoContext(ctx, "github: listed repository issue comments",
		slog.String("repo", repo.String()),
		slog.Time("since", since.UTC()),
		slog.Int("requests", requests),
		slog.Int("comments", len(payloads)),
		slog.Int("issues", len(numbers)),
		slog.Int("malformed_issue_urls", malformed),
	)
	return numbers, nil
}

// issueNumberFromURL pulls the issue number out of a comment's issue_url,
// which is the only place the repo-level comments endpoint records which issue
// a comment belongs to.
//
// A URL that does not end in a positive integer is reported rather than
// guessed at: dropping one comment is recoverable, attributing it to the wrong
// issue is not.
func issueNumberFromURL(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	last := raw[strings.LastIndexByte(raw, '/')+1:]
	number, err := strconv.Atoi(last)
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}

// PostComment posts a comment on an issue and returns the created comment,
// whose id becomes runs.result_comment_id.
//
// # The parameter type is the point
//
// The body is a [redact.Body] and never a string. That type has no exported
// constructor and no exported field, so the only way to obtain one carrying
// text is [redact.Redactor.Apply] — which means "post a body that has not been
// through the S-7 redaction pass" is not an expressible program, rather than a
// rule a reviewer has to catch (AC-4.6). A zero redact.Body did not come from
// Apply and is refused here rather than posted as an empty comment.
//
// # One credential
//
// The credential used is the bot token this client was built with, and this is
// litefactory's only posting path: `gh` is identity discovery only, and Mario's
// SSH key pushes git. A report is therefore always visibly from the bot and
// never from Mario (AC-4.11).
func (c *Client) PostComment(ctx context.Context, repo Repo, issue int, body redact.Body) (Comment, error) {
	if err := repo.Validate(); err != nil {
		return Comment{}, err
	}
	if err := checkIssueNumber(issue); err != nil {
		return Comment{}, err
	}
	if !body.Valid() {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w: body did not come from redact.Apply",
			repo, issue, ErrInvalidArgument)
	}
	if body.Len() == 0 {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w: body is empty",
			repo, issue, ErrInvalidArgument)
	}
	if body.Len() > CommentSizeLimit {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w: body is %d bytes, the limit is %d",
			repo, issue, ErrInvalidArgument, body.Len(), CommentSizeLimit)
	}

	endpoint := c.endpoint(issuePath(repo, issue)+"/comments", nil)
	request := commentRequest{Body: body.String()}
	var payload commentPayload
	if _, err := c.do(ctx, http.MethodPost, endpoint, request, &payload); err != nil {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w", repo, issue, err)
	}
	if payload.ID == 0 {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w: response carried no comment id",
			repo, issue, ErrUnexpectedStatus)
	}
	return payload.comment(), nil
}

// ReactionContent is one of GitHub's fixed reaction names.
type ReactionContent string

// The reactions GitHub accepts. Only [ReactionEyes] is used at M0.
const (
	// ReactionEyes is 👀 — the acknowledgement litefactory adds to an
	// instruction comment before the run starts (AC-2.17).
	ReactionEyes ReactionContent = "eyes"

	ReactionPlusOne  ReactionContent = "+1"
	ReactionMinusOne ReactionContent = "-1"
	ReactionLaugh    ReactionContent = "laugh"
	ReactionConfused ReactionContent = "confused"
	ReactionHeart    ReactionContent = "heart"
	ReactionHooray   ReactionContent = "hooray"
	ReactionRocket   ReactionContent = "rocket"
)

// AddCommentReaction reacts to an issue comment.
//
// Reacting is an acknowledgement, not a step of the work: the caller logs a
// failure here and carries on with the turn (AC-2.17). That is why this returns
// only an error and nothing the turn could come to depend on — there is no
// reaction id to record and no state to thread onwards.
//
// Adding a reaction that is already there is not an error; GitHub answers 200
// instead of 201 and this treats both as success, so a replayed tick is
// harmless.
func (c *Client) AddCommentReaction(ctx context.Context, repo Repo, commentID int64, content ReactionContent) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if err := checkCommentID(commentID); err != nil {
		return err
	}
	if !content.valid() {
		return fmt.Errorf("github: reaction %q: %w: not a GitHub reaction", content, ErrInvalidArgument)
	}

	endpoint := c.endpoint(repoPath(repo)+"/issues/comments/"+
		strconv.FormatInt(commentID, 10)+"/reactions", nil)
	if _, err := c.do(ctx, http.MethodPost, endpoint, reactionRequest{Content: string(content)}, nil); err != nil {
		return fmt.Errorf("github: react %s to comment %d on %s: %w", content, commentID, repo, err)
	}
	return nil
}

func (r ReactionContent) valid() bool {
	switch r {
	case ReactionEyes, ReactionPlusOne, ReactionMinusOne, ReactionLaugh,
		ReactionConfused, ReactionHeart, ReactionHooray, ReactionRocket:
		return true
	default:
		return false
	}
}

// commentRequest is the POST body for a new comment.
type commentRequest struct {
	Body string `json:"body"`
}

// reactionRequest is the POST body for a new reaction.
type reactionRequest struct {
	Content string `json:"content"`
}

// commentPayload is GitHub's issue-comment shape.
type commentPayload struct {
	ID        int64        `json:"id"`
	IssueURL  string       `json:"issue_url"`
	Body      string       `json:"body"`
	User      *userPayload `json:"user"`
	HTMLURL   string       `json:"html_url"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (p *commentPayload) comment() Comment {
	return Comment{
		ID:        p.ID,
		Body:      p.Body,
		Author:    p.User.user(),
		HTMLURL:   p.HTMLURL,
		CreatedAt: p.CreatedAt.UTC(),
		UpdatedAt: p.UpdatedAt.UTC(),
	}
}
