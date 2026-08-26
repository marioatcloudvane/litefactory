package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
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

	query := url.Values{}
	// Only open issues are listed. This is a payload saving on the common
	// path and *not* the guard AC-2.22 asks for: the daemon reaches an issue a
	// second way, through [Client.GetIssue] for a number that only the comment
	// listing produced, and that read never sees this query string. The skip
	// therefore lives where the two paths meet, in the daemon's processIssue —
	// filtering here as well simply saves carrying issues nobody will act on.
	query.Set("state", StateOpen)
	// Sorting by update time ascending pairs with the since cursor: the last
	// page holds the newest activity, and a truncated walk loses the newest
	// rather than the oldest.
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", strconv.Itoa(perPage))
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339))
	}

	endpoint := c.endpoint(repoPath(repo)+"/issues", query)
	payloads, requests, err := fetchPages[issuePayload](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("github: list issues for %s: %w", repo, err)
	}

	issues := make([]Issue, 0, len(payloads))
	pulls := 0
	for i := range payloads {
		if payloads[i].PullRequest != nil {
			pulls++
			continue
		}
		issues = append(issues, payloads[i].issue())
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

	endpoint := c.endpoint(issuePath(repo, number), nil)
	var payload issuePayload
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &payload); err != nil {
		return Issue{}, fmt.Errorf("github: get issue %s#%d: %w", repo, number, err)
	}
	if payload.PullRequest != nil {
		return Issue{}, fmt.Errorf("github: get issue %s#%d: %w: that number is a pull request",
			repo, number, ErrInvalidArgument)
	}
	return payload.issue(), nil
}

// issuePayload is GitHub's issue shape. Only the fields M0 reads are declared;
// the rest of a very large object is discarded by the decoder.
type issuePayload struct {
	Number int          `json:"number"`
	Title  string       `json:"title"`
	Body   string       `json:"body"`
	State  string       `json:"state"`
	User   *userPayload `json:"user"`
	// Assignees is a list GitHub sends as [] rather than null when an issue is
	// assigned to nobody. It is decoded through pointers anyway, so neither an
	// absent list nor a null element inside one can panic a tick.
	Assignees []*userPayload `json:"assignees"`
	HTMLURL   string         `json:"html_url"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	// PullRequest is present, and non-nil, exactly when this "issue" is really
	// a pull request. Its contents are irrelevant; its existence is the signal.
	PullRequest *pullRequestPayload `json:"pull_request"`
}

type pullRequestPayload struct {
	URL string `json:"url"`
}

func (p *issuePayload) issue() Issue {
	return Issue{
		Number:    p.Number,
		Title:     p.Title,
		Body:      p.Body,
		State:     p.State,
		Author:    p.User.user(),
		Assignees: p.assignees(),
		HTMLURL:   p.HTMLURL,
		CreatedAt: p.CreatedAt.UTC(),
		UpdatedAt: p.UpdatedAt.UTC(),
	}
}

// assignees converts the assignee list.
//
// A null element is dropped rather than carried as an account with id 0: the
// list means "the accounts GitHub named", and a zero id in it would be a
// nobody that [Issue.AssignedTo] has to defend against twice.
func (p *issuePayload) assignees() []User {
	if len(p.Assignees) == 0 {
		return nil
	}
	users := make([]User, 0, len(p.Assignees))
	for _, assignee := range p.Assignees {
		if assignee == nil {
			continue
		}
		users = append(users, assignee.user())
	}
	return users
}
