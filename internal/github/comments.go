package github

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/ticket"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/redact"
)

// CommentSizeLimit is the largest comment body GitHub accepts, in bytes.
// Anything longer comes back as a 422, so the reporter budgets against this
// with [redact.Budget] before it assembles a body (AC-4.8).
const CommentSizeLimit = 65536

// Comment is an issue comment.
type Comment struct {
	// ReplyToCommentID is explicit provider parent metadata; zero means absent.
	ReplyToCommentID int64
	// QualifiedReply retains opaque provider evidence at the legacy workflow boundary.
	QualifiedReply *ticket.Reply
	// APIURL and IssueAPIURL retain response provenance for the neutral adapter.
	APIURL      string
	IssueAPIURL string
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
	// Refusing a non-positive number is load-bearing here, not just tidy: the
	// SDK reads an issue number of 0 as "every comment in the repository" and
	// serves it from a different path. Letting one through would compute this
	// issue's watermark against the whole repository's comment stream.
	if err := checkIssueNumber(issue); err != nil {
		return nil, err
	}

	var opts gogithub.IssueListCommentsOptions
	opts.PerPage = perPage

	payloads, requests, err := fetchPages(ctx, c,
		func(ctx context.Context, page int) ([]*gogithub.IssueComment, *gogithub.Response, error) {
			pageOpts := opts
			pageOpts.Page = page
			return c.api.Issues.ListComments(ctx, repo.Owner, repo.Name, issue, &pageOpts)
		})
	if err != nil {
		return nil, fmt.Errorf("github: list comments on %s#%d: %w", repo, issue, err)
	}

	comments := make([]Comment, 0, len(payloads))
	for _, payload := range payloads {
		if payload == nil || payload.GetID() <= afterID {
			continue
		}
		comments = append(comments, convertComment(payload))
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
// # Why discovery asks twice
//
// [Client.ListIssues] finds the issues whose `updated_at` moved. Writing a
// comment does move it — measured on marioatcloudvane/litefactory#4, whose
// comment landed at 19:14:47Z and whose issue read 19:14:48Z a moment later —
// so in principle one listing would do.
//
// In practice the two are indexed separately and lag separately. On issue #3 of
// the same repository, two owner comments were invisible to sixty consecutive
// issue listings across thirty minutes, and only surfaced seven hours later
// when the issue was closed by hand. This endpoint is indexed on the comments
// themselves rather than on a timestamp copied onto the issue, so it is the
// shorter path to a comment and unlikely to lag in step with the other.
//
// It is not the guard against that lag — overlapping the listing window is (see
// the daemon's ListingOverlap). It is a second, independent way for a comment
// to be noticed, at one request per repository per tick.
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

	opts := gogithub.IssueListCommentsOptions{
		Sort:      gogithub.Ptr("updated"),
		Direction: gogithub.Ptr("asc"),
	}
	if !since.IsZero() {
		opts.Since = gogithub.Ptr(since.UTC())
	}
	opts.PerPage = perPage

	// An issue number of 0 is how the SDK addresses the repository-level
	// endpoint. It is the one call in this package that wants it, and the
	// issue-scoped listing refuses 0 for exactly that reason.
	payloads, requests, err := fetchPages(ctx, c,
		func(ctx context.Context, page int) ([]*gogithub.IssueComment, *gogithub.Response, error) {
			pageOpts := opts
			pageOpts.Page = page
			return c.api.Issues.ListComments(ctx, repo.Owner, repo.Name, 0, &pageOpts)
		})
	if err != nil {
		return nil, fmt.Errorf("github: list the comments of %s: %w", repo, err)
	}

	seen := make(map[int]bool, len(payloads))
	numbers := make([]int, 0, len(payloads))
	malformed := 0
	for _, payload := range payloads {
		number, ok := issueNumberFromURL(payload.GetIssueURL())
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

	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.IssueComment, *gogithub.Response, error) {
		// IssueComment is the SDK's request body as well as its response
		// shape, and every field is omitempty — so this marshals to exactly
		// {"body": …} and nothing else travels.
		return c.api.Issues.CreateComment(ctx, repo.Owner, repo.Name, issue,
			&gogithub.IssueComment{Body: gogithub.Ptr(body.String())})
	})
	if err != nil {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w", repo, issue, err)
	}
	if payload.GetID() == 0 {
		return Comment{}, fmt.Errorf("github: post comment on %s#%d: %w: response carried no comment id",
			repo, issue, ErrUnexpectedStatus)
	}
	return convertComment(payload), nil
}

// EditComment replaces the body of a comment litefactory posted, which is how the
// one summary a Plan parent keeps stays current in place. Like [Client.PostComment]
// it takes only a redacted body, so nothing unredacted can reach the forge.
func (c *Client) EditComment(ctx context.Context, repo Repo, commentID int64, body redact.Body) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if err := checkCommentID(commentID); err != nil {
		return err
	}
	if !body.Valid() {
		return fmt.Errorf("github: edit comment %d on %s: %w: body did not come from redact.Apply",
			commentID, repo, ErrInvalidArgument)
	}
	if body.Len() == 0 || body.Len() > CommentSizeLimit {
		return fmt.Errorf("github: edit comment %d on %s: %w: body is %d bytes, the limit is %d",
			commentID, repo, ErrInvalidArgument, body.Len(), CommentSizeLimit)
	}
	_, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.IssueComment, *gogithub.Response, error) {
		return c.api.Issues.EditComment(ctx, repo.Owner, repo.Name, commentID,
			&gogithub.IssueComment{Body: gogithub.Ptr(body.String())})
	})
	if err != nil {
		return fmt.Errorf("github: edit comment %d on %s: %w", commentID, repo, err)
	}
	return nil
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

	if _, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Reaction, *gogithub.Response, error) {
		return c.api.Reactions.CreateIssueCommentReaction(ctx, repo.Owner, repo.Name, commentID, string(content))
	}); err != nil {
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

// convertComment narrows the SDK's issue comment to the fields M0 reads. A nil
// payload converts to the zero Comment rather than panicking, which is what an
// absent or null object has always meant here.
func convertComment(payload *gogithub.IssueComment) Comment {
	if payload == nil {
		return Comment{}
	}
	author := convertUser(payload.User)
	if payload.User != nil {
		author.IsHuman = payload.User.GetType() == "User"
	}
	return Comment{
		APIURL:      payload.GetURL(),
		IssueAPIURL: payload.GetIssueURL(),
		ID:          payload.GetID(),
		Body:        payload.GetBody(),
		Author:      author,
		HTMLURL:     payload.GetHTMLURL(),
		CreatedAt:   payload.GetCreatedAt().UTC(),
		UpdatedAt:   payload.GetUpdatedAt().UTC(),
	}
}
