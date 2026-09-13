package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/redact"
)

// StateOpen is the one issue state litefactory acts on.
//
// Closing an issue is how the factory is told to stop working on it: a comment
// on an issue that is not open starts nothing (AC-2.22). The skip itself is the
// daemon's, not this package's — see [Client.ListIssues] for why the query
// filter is not the guard.
const StateOpen = "open"

// Issue is a GitHub issue. Pull requests never appear here — see [Client.ListIssues].
type Issue struct {
	// APIURL binds individual observations to their repository and issue endpoint.
	APIURL string
	// ID is the issue's database id: the identifier GitHub mints for the
	// object, which is not its number.
	//
	// The two are separate fields because GitHub's sub-issue API addresses the
	// parent by its *number*, in the path, and the child by its *id*, in the
	// body — both int64, both integers on the wire, and passing one where the
	// other belongs links the wrong issue with no error at all (AC-C1.1a).
	// Keeping both on this struct is what lets [Client.CreateSubIssues] hand
	// each of the two calls the identifier it actually wants, and
	// TestTheSubIssueRequestCarriesTheChildsDatabaseID is what proves it does.
	//
	// Nothing outside sub-issue creation reads it; it rides on the payload
	// every other field here comes from, so it costs no extra request.
	ID int64
	// Number is the issue number, the one that appears in the task ref.
	Number int
	// Title is the issue title. The feature slug is derived from it.
	Title string
	// Body is the issue body, verbatim and untrusted: it is written by whoever
	// opened the issue, who is not necessarily the person whose comment
	// triggered the run, which is why the prompt frames it as a third-party
	// note (D-N).
	Body string
	// State is "open" or "closed"; only [StateOpen] is acted on (AC-2.22).
	State string
	// StateReason distinguishes completed from an unrelated human closure.
	StateReason string
	// Author is who opened the issue. It is the attribution the prompt carries
	// — "opened by X" — and, on an issue whose body is run as its first
	// instruction, the author of that trigger too (AC-2.16b).
	//
	// It is not the authorisation. Anyone can open an issue, so nothing is
	// started on the strength of this field: what unlocks a body is
	// [Issue.Assignees] containing the owner. That the two can differ is the
	// point, and the attribution is why it is safe — a run started from a
	// stranger's body still tells the model who wrote it. The blanket ban this
	// comment used to cite (AC-2.16) is reversed in `.aiteam/AC_AMENDMENTS.md`.
	Author User
	// Assignees is everyone the issue is assigned to, empty when it is
	// assigned to nobody. It rides on the same payload as every other field
	// here — no extra request and no eighth endpoint (AC-2.24) — and is what
	// makes "this one is for you" expressible. Match it with
	// [Issue.AssignedTo], never by comparing logins.
	Assignees []User
	// Labels is every label name on the issue, in the order GitHub sent them,
	// empty when it carries none. Like [Issue.Assignees] it rides on the issue
	// payload that is already fetched, so it costs no extra request on either
	// the listing or the single-issue path.
	//
	// A label is a *lookup key* and never a value (C2): the only thing
	// litefactory does with one of these strings is match it against
	// operator-authored configuration. No label text may be rendered into a
	// prompt, echoed into a comment, or joined into a child process's argv —
	// nobody's write access to this repository is a permission grant (C3).
	Labels []string
	// HTMLURL is the browser URL of the issue.
	HTMLURL string
	// CreatedAt and UpdatedAt are GitHub's timestamps. UpdatedAt is what the
	// since filter matches on.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AssignedTo reports whether the account with this numeric id is one of the
// issue's assignees.
//
// The comparison is on the id and never on a login (D-I), for the same reason
// every other identity decision in litefactory is: a login is a label GitHub
// lets people change, and the id is the account. A zero id matches nothing —
// that is what GitHub reports for a deleted account, and an unknown owner id
// must not quietly match one.
func (i Issue) AssignedTo(userID int64) bool {
	if userID == 0 {
		return false
	}
	for _, assignee := range i.Assignees {
		if assignee.ID == userID {
			return true
		}
	}
	return false
}

// ListIssues returns the issues in repo touched at or after since, oldest
// update first. A zero since lists every issue in the repository.
//
// # Why this is per-repo and not a search
//
// Issue *search* was evaluated and dropped (D-A). Do not reintroduce it as an
// optimisation. Search would collapse the tick into one call, and it is the
// wrong substrate for "notices within a tick" on three counts:
//
//   - its query string caps at 256 characters, which is roughly ten repos
//     before it starts failing *silently*;
//   - it is limited to 30 requests a minute, against the 5,000 an hour the
//     core API gives this listing;
//   - it is eventually consistent, so a comment can be minutes old before the
//     index knows about it.
//
// Per-repo listing is strongly consistent and sits on the core budget. The
// criterion this serves is "tick cost is O(registered repos), bounded and
// logged" (AC-2.8) — not "one API call".
//
// # The since boundary
//
// GitHub's since filter is *inclusive*, so the issue that sits exactly on the
// cursor comes back again on the next tick. That is correct and must not be
// "fixed" by nudging the timestamp forward: an exclusive offset would drop any
// issue updated in the same second as the cursor. The per-task comment
// watermark absorbs the repeat — a re-seen issue whose comments are all at or
// below the watermark produces no work.
//
// # Pull requests
//
// GitHub's issues endpoint returns pull requests as well: every PR is an issue
// with a "pull_request" member. Those entries are dropped here, so no caller
// can forget to (AC-2.8). PR review comments are out of scope for M0 entirely
// (D-6).
func (c *Client) ListIssues(ctx context.Context, repo Repo, since time.Time) ([]Issue, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}

	opts := gogithub.IssueListByRepoOptions{
		// Only open issues are listed. This is a payload saving on the common
		// path and *not* the guard AC-2.22 asks for: the daemon reaches an
		// issue a second way, through [Client.GetIssue] for a number that only
		// the comment listing produced, and that read never sees this query
		// string. The skip therefore lives where the two paths meet, in the
		// daemon's processIssue — filtering here as well simply saves carrying
		// issues nobody will act on.
		State: StateOpen,
		// Sorting by update time ascending pairs with the since cursor: the
		// last page holds the newest activity, and a truncated walk loses the
		// newest rather than the oldest.
		Sort:      "updated",
		Direction: "asc",
	}
	if !since.IsZero() {
		opts.Since = since.UTC()
	}
	// IssueListByRepoOptions embeds both ListOptions and ListCursorOptions, and
	// both declare Page and PerPage, so the selector has to be written out.
	// ListOptions is the offset-paginated one, which is what this endpoint
	// uses; leaving ListCursorOptions at its zero value keeps its fields out of
	// the query string.
	opts.ListOptions.PerPage = perPage

	payloads, requests, err := fetchPages(ctx, c,
		func(ctx context.Context, page int) ([]*gogithub.Issue, *gogithub.Response, error) {
			// A copy per page: the loop must not mutate the options the caller
			// can still see, and a shared pointer would make the page number of
			// one request depend on the last.
			pageOpts := opts
			pageOpts.ListOptions.Page = page
			return c.api.Issues.ListByRepo(ctx, repo.Owner, repo.Name, &pageOpts)
		})
	if err != nil {
		return nil, fmt.Errorf("github: list issues for %s: %w", repo, err)
	}

	issues := make([]Issue, 0, len(payloads))
	pulls := 0
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		if payload.IsPullRequest() {
			pulls++
			continue
		}
		issues = append(issues, convertIssue(payload))
	}

	// This is the per-repo cost line AC-2.8 asks for: one record per repo per
	// tick, naming what it cost and what it found.
	c.logger().InfoContext(ctx, "github: listed repository issues",
		slog.String("repo", repo.String()),
		slog.Time("since", since.UTC()),
		slog.Int("requests", requests),
		slog.Int("issues", len(issues)),
		slog.Int("pull_requests_skipped", pulls),
	)
	return issues, nil
}

// GetIssue reads one issue.
//
// A pull request number resolves here too — GitHub serves it from the issues
// endpoint — and is refused, so a caller cannot reach a PR by number through a
// door the listing closes.
func (c *Client) GetIssue(ctx context.Context, repo Repo, number int) (Issue, error) {
	if err := repo.Validate(); err != nil {
		return Issue{}, err
	}
	if err := checkIssueNumber(number); err != nil {
		return Issue{}, err
	}

	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		return c.api.Issues.Get(ctx, repo.Owner, repo.Name, number)
	})
	if err != nil {
		return Issue{}, fmt.Errorf("github: get issue %s#%d: %w", repo, number, err)
	}
	if payload == nil {
		// A 2xx whose body was literally null. There is no issue to convert and
		// nothing that could be a pull request; the zero value is what the
		// hand-rolled decoder produced here too.
		return Issue{}, nil
	}
	if payload.IsPullRequest() {
		return Issue{}, fmt.Errorf("github: get issue %s#%d: %w: that number is a pull request",
			repo, number, ErrInvalidArgument)
	}
	return convertIssue(payload), nil
}

// AddIssueLabel adds one exact label without replacing any other label.
func (c *Client) AddIssueLabel(ctx context.Context, repo Repo, number int, label string) error {
	if err := repo.Validate(); err != nil || number <= 0 || label == "" {
		return fmt.Errorf("github: add issue label: %w", ErrInvalidArgument)
	}
	_, _, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.Label, *gogithub.Response, error) {
		return c.api.Issues.AddLabelsToIssue(ctx, repo.Owner, repo.Name, number, []string{label})
	})
	return err
}

// RemoveIssueLabel removes one exact label without replacing any other label.
func (c *Client) RemoveIssueLabel(ctx context.Context, repo Repo, number int, label string) error {
	if err := repo.Validate(); err != nil || number <= 0 || label == "" {
		return fmt.Errorf("github: remove issue label: %w", ErrInvalidArgument)
	}
	_, _, err := do(ctx, c, func(ctx context.Context) (struct{}, *gogithub.Response, error) {
		resp, err := c.api.Issues.RemoveLabelForIssue(ctx, repo.Owner, repo.Name, number, label)
		return struct{}{}, resp, err
	})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// convertIssue narrows the SDK's issue to the fields M0 reads.
//
// The boundary type is kept even though it looks like duplication: the SDK's
// Issue is a hundred optional pointers, and callers of this package compare
// numeric ids and read plain strings. A nil payload converts to the zero Issue
// rather than panicking, which is what an absent or null object has always
// meant here.
func convertIssue(payload *gogithub.Issue) Issue {
	if payload == nil {
		return Issue{}
	}
	return Issue{
		APIURL:      payload.GetURL(),
		ID:          payload.GetID(),
		Number:      payload.GetNumber(),
		Title:       payload.GetTitle(),
		Body:        payload.GetBody(),
		State:       payload.GetState(),
		StateReason: payload.GetStateReason(),
		Author:      convertUser(payload.User),
		Assignees:   convertUsers(payload.Assignees),
		Labels:      labelNames(payload.Labels),
		HTMLURL:     payload.GetHTMLURL(),
		CreatedAt:   payload.GetCreatedAt().UTC(),
		UpdatedAt:   payload.GetUpdatedAt().UTC(),
	}
}

// convertUsers converts an assignee list.
//
// A null element is dropped rather than carried as an account with id 0: the
// list means "the accounts GitHub named", and a zero id in it would be a
// nobody that [Issue.AssignedTo] has to defend against twice.
func convertUsers(payloads []*gogithub.User) []User {
	if len(payloads) == 0 {
		return nil
	}
	users := make([]User, 0, len(payloads))
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		users = append(users, convertUser(payload))
	}
	return users
}

// labelNames converts the label list to its names.
//
// Only the name is read: the colour and the description are presentation, and
// the id is of no use to a lookup that matches on the name an operator typed. A
// null element is dropped on the same reasoning as [convertUsers] — the list
// means "the labels GitHub named", and an empty string in it is a key every
// caller would have to defend against.
func labelNames(payloads []*gogithub.Label) []string {
	if len(payloads) == 0 {
		return nil
	}
	names := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		names = append(names, payload.Name)
	}
	return names
}

// IssueTitleSizeLimit is the largest issue title this package will send, in
// bytes.
//
// It is litefactory's own ceiling rather than a documented GitHub one: the
// create-issue endpoint does not publish a maximum title length, and inventing
// a number and calling it GitHub's would be a claim nobody here verified. What
// this does is refuse locally what would otherwise come back as a 422 with no
// explanation, and bound what a title can be before it stops being a title.
//
// internal/report cuts a story title to a quarter of this before it ever
// reaches here. The headroom between the two is deliberate: redaction runs
// after that cut and replaces a matched token with a longer literal, so a title
// can only grow on its way to this check.
const IssueTitleSizeLimit = 1024

// CreateIssue opens an issue and returns it, carrying both the number GitHub
// gave it and its database id.
//
// # Both identifiers, on purpose
//
// The returned [Issue.ID] is what [Client.AddSubIssue] needs and the returned
// [Issue.Number] is what a link and a task ref need. They are different
// identifiers for the same object, and the sub-issue API asks for one of each
// in a single call, so this returns both rather than making a caller fetch the
// issue a second time to learn the other (AC-C1.1a).
//
// A response carrying neither is refused as an unexpected status rather than
// returned as a zero-valued issue: an id of 0 handed to the attach step would
// address no issue at all, and "the sub-issue was linked to nothing" is not a
// failure that should have to be discovered by looking at the forge.
//
// # The parameter types are the point
//
// Both the title and the body are [redact.Body], for the reason
// [Client.PostComment]'s body is: that type has no exported constructor, so the
// only way to obtain one carrying text is [redact.Redactor.Apply], and "create
// an issue whose title or body has not been through the S-7 pass" is not an
// expressible program (AC-4.6). A title is as public as a body and is quoted in
// every notification GitHub sends about the issue.
func (c *Client) CreateIssue(ctx context.Context, repo Repo, title, body redact.Body) (Issue, error) {
	if err := repo.Validate(); err != nil {
		return Issue{}, err
	}
	if err := checkIssueTitle(title); err != nil {
		return Issue{}, fmt.Errorf("github: create issue in %s: %w", repo, err)
	}
	if err := checkIssueBody(body); err != nil {
		return Issue{}, fmt.Errorf("github: create issue in %s: %w", repo, err)
	}

	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.Issue, *gogithub.Response, error) {
		// Every other field of CreateIssueRequest is omitempty, so this
		// marshals to exactly {"title": …, "body": …}: no labels, no
		// assignees, no milestone. A sub-issue is filed by litefactory and
		// belongs to whoever the plan is for, and assigning or labelling it
		// here would be litefactory deciding something nobody asked it to.
		return c.api.Issues.Create(ctx, repo.Owner, repo.Name, gogithub.CreateIssueRequest{
			Title: title.String(),
			Body:  gogithub.Ptr(body.String()),
		})
	})
	if err != nil {
		return Issue{}, fmt.Errorf("github: create issue in %s: %w", repo, err)
	}
	if payload.GetID() == 0 || payload.GetNumber() == 0 {
		return Issue{}, fmt.Errorf("github: create issue in %s: %w: response carried no issue id or number",
			repo, ErrUnexpectedStatus)
	}

	issue := convertIssue(payload)
	// Info, not debug: this is the one call in this package that creates
	// something durable on the forge, and the record of what it created is
	// worth having in daemon.log without turning debug on.
	c.logger().InfoContext(ctx, "github: created issue",
		slog.String("repo", repo.String()),
		slog.Int("issue", issue.Number),
		slog.Int64("issue_id", issue.ID),
	)
	return issue, nil
}

// checkIssueTitle refuses a title that cannot be sent as one.
//
// A newline is refused rather than stripped: this package does not silently
// rewrite what a caller asked it to publish, and internal/report already
// one-lines a title before it gets here, so a multi-line one arriving means the
// caller skipped the renderer — which is exactly what should fail loudly.
func checkIssueTitle(title redact.Body) error {
	switch {
	case !title.Valid():
		return fmt.Errorf("%w: title did not come from redact.Apply", ErrInvalidArgument)
	case title.Len() == 0:
		return fmt.Errorf("%w: title is empty", ErrInvalidArgument)
	case title.Len() > IssueTitleSizeLimit:
		return fmt.Errorf("%w: title is %d bytes, the limit is %d",
			ErrInvalidArgument, title.Len(), IssueTitleSizeLimit)
	case strings.ContainsAny(title.String(), "\r\n"):
		return fmt.Errorf("%w: title contains a line break", ErrInvalidArgument)
	}
	return nil
}

// checkIssueBody refuses a body that cannot be sent as one. An issue body is
// held to the same size limit as a comment, which is the limit internal/report
// assembles against.
func checkIssueBody(body redact.Body) error {
	switch {
	case !body.Valid():
		return fmt.Errorf("%w: body did not come from redact.Apply", ErrInvalidArgument)
	case body.Len() == 0:
		return fmt.Errorf("%w: body is empty", ErrInvalidArgument)
	case body.Len() > CommentSizeLimit:
		return fmt.Errorf("%w: body is %d bytes, the limit is %d",
			ErrInvalidArgument, body.Len(), CommentSizeLimit)
	}
	return nil
}
