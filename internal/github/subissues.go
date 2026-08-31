package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/redact"
)

// Sub-issues are how a plan reaches GitHub: one issue per user story, each
// attached to the issue the plan was asked for on (C1).
//
// # Two identifiers, one call
//
// The add endpoint is POST /repos/{owner}/{repo}/issues/{issue_number}/sub_issues
// with a body of {"sub_issue_id": N}. The path carries the *parent's number*.
// The body carries the *child's database id*. They are different identifiers,
// both integers, and both int64 by the time they reach the SDK — so nothing
// but a test can tell you when they have been swapped, and a swap links a real
// issue to the wrong parent and returns 201 (AC-C1.1a, confirmed against
// GitHub's own documentation on 2026-08-31 and against go-github v90's source).
//
// That is why [Issue] carries both and why [Client.CreateIssue] refuses a
// response missing either: the id the attach step needs comes from the create
// step's response, never from a number.
//
// # The response of the add call is deliberately not read
//
// go-github hands back a *SubIssue for the add, and this package throws it
// away. The documentation does not say whether the issue in that response is
// the parent or the child, and reading the wrong one would put the parent's
// number and URL in a story index row — the exact silent mislink this file
// spends the paragraph above avoiding. Everything the caller needs is already
// known from the create call, so nothing is lost by not guessing.
//
// # Failure halfway is answered by resumability, not by waiting
//
// GitHub documents a secondary rate limit on the add endpoint — "creating
// content too quickly using this endpoint may result in secondary rate
// limiting" — and a plan is a loop of creations and attachments. This package
// still retries nothing and sleeps nowhere: backoff is the daemon's, because
// only the daemon knows what else is queued (see the package documentation, and
// the client-side rate-limit bookkeeping [New] switches off for the same
// reason).
//
// So [Client.CreateSubIssues] answers a partial failure by being re-runnable
// instead. It stops at the first failure, reports what it created, and finds
// its own earlier work on the next attempt through the marker each body carries
// (AC-C1.5). That is stronger than a sleep would have been: it survives the
// process being killed, which is the failure mode a sleep is least able to
// help with.

// SubIssueSpec is one user story's sub-issue, rendered and ready to create.
//
// It is built from internal/report's [report.SubIssue] by the caller rather
// than by this package: internal/report renders and internal/github posts, and
// keeping the two apart is why neither imports the other.
type SubIssueSpec struct {
	// StoryID identifies the story in the result and in the log — "C1",
	// "US-3". It is not sent to GitHub; the marker is what the forge sees.
	StoryID string

	// Title and Body are the issue to create, already through the S-7 pass.
	Title redact.Body
	Body  redact.Body

	// Marker is the line that identifies this story's sub-issue on a later
	// run: report.StoryMarker, carrying the parent ref and the story id.
	//
	// It must be what Body begins with, and that is checked rather than
	// trusted. A marker naming one story attached to a body describing another
	// would make every re-run create a fresh issue and every idempotency
	// guarantee here vacuous, so the mismatch is refused before the first
	// request rather than discovered as a pile of duplicates.
	Marker string

	// AttachID resumes a story whose issue was created but could not be
	// attached — the [SubIssueUnattached] outcome of an earlier run, whose
	// [SubIssueResult.ID] is the value to put here.
	//
	// It exists because that outcome is the one gap in the marker check: an
	// issue that was never attached is not among its parent's sub-issues, so
	// listing them cannot find it, and a plain re-run would file the story a
	// second time. Zero means "create the issue" and is the ordinary case.
	AttachID int64
}

// SubIssueOutcome says what happened to one story.
//
// A bad outcome is a value here, not an error: [Client.CreateSubIssues]
// reserves its error return for a batch that could not be attempted at all, so
// that a run which got halfway can say what it did.
type SubIssueOutcome int

const (
	// SubIssueNotAttempted is the zero value: an earlier story in the batch
	// failed and this one was never reached. It is the zero value on purpose —
	// an unfilled result is truthfully "nothing was done about this".
	SubIssueNotAttempted SubIssueOutcome = iota

	// SubIssueCreated means this run created the issue and attached it.
	SubIssueCreated

	// SubIssueAttached means this run attached an issue an earlier run had
	// created — the [SubIssueSpec.AttachID] path. The result carries the id it
	// was given and no number or URL, because this run never saw the issue
	// itself; the run that created it reported both.
	SubIssueAttached

	// SubIssueExisted means the story already had a sub-issue on the parent
	// and nothing was done. This is what makes a re-run safe (AC-C1.4).
	SubIssueExisted

	// SubIssueUnattached means the issue was created and the attach failed.
	// The issue exists on the forge, orphaned; the result carries its number
	// and id, and [SubIssueSpec.AttachID] is how a later run finishes the job.
	SubIssueUnattached

	// SubIssueFailed means the issue was not created. Nothing exists on the
	// forge for this story, so a later run simply creates it.
	SubIssueFailed
)

// String renders the outcome for logs and test failures.
func (o SubIssueOutcome) String() string {
	switch o {
	case SubIssueCreated:
		return "created"
	case SubIssueAttached:
		return "attached"
	case SubIssueExisted:
		return "existed"
	case SubIssueUnattached:
		return "unattached"
	case SubIssueFailed:
		return "failed"
	case SubIssueNotAttempted:
		return "not_attempted"
	default:
		return "not_attempted"
	}
}

// SubIssueResult is what became of one story, in the order its spec was given.
type SubIssueResult struct {
	// StoryID is the spec's story id, so a result can be read without
	// counting positions.
	StoryID string

	// Outcome says what happened.
	Outcome SubIssueOutcome

	// Number, ID and HTMLURL identify the sub-issue when there is one. Number
	// and HTMLURL are empty on the [SubIssueAttached] path and on every
	// outcome where no issue exists.
	Number  int
	ID      int64
	HTMLURL string

	// Err is why this story failed, on [SubIssueFailed] and
	// [SubIssueUnattached] and nowhere else. It is the typed error this
	// package raises — a *RateLimitError still carries its RetryAfter — so the
	// caller decides what to do about it with the same information it would
	// have had from a single call.
	Err error
}

// ListSubIssues returns the sub-issues attached to an issue, in the order
// GitHub lists them.
//
// It exists for the idempotency check: the bodies come back with the issues, so
// one request per page answers "which of these stories has already been filed"
// without a search and without any local state (AC-C1.4). Pull requests are not
// filtered out — one cannot be a sub-issue — and would in any case carry no
// marker.
func (c *Client) ListSubIssues(ctx context.Context, repo Repo, parent int) ([]Issue, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if err := checkIssueNumber(parent); err != nil {
		return nil, err
	}

	var opts gogithub.ListOptions
	opts.PerPage = perPage

	payloads, requests, err := fetchPages(ctx, c,
		func(ctx context.Context, page int) ([]*gogithub.SubIssue, *gogithub.Response, error) {
			pageOpts := opts
			pageOpts.Page = page
			// The parent is addressed by its number here, as it is for the
			// add: this endpoint's {issue_number} is a number in both.
			return c.api.SubIssue.ListByIssue(ctx, repo.Owner, repo.Name, int64(parent), &pageOpts)
		})
	if err != nil {
		return nil, fmt.Errorf("github: list the sub-issues of %s#%d: %w", repo, parent, err)
	}

	issues := make([]Issue, 0, len(payloads))
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		// SubIssue is a defined type over the SDK's Issue, so this is a
		// conversion rather than a second narrowing function to keep in step.
		issues = append(issues, convertIssue((*gogithub.Issue)(payload)))
	}

	c.logger().DebugContext(ctx, "github: listed sub-issues",
		slog.String("repo", repo.String()),
		slog.Int("parent", parent),
		slog.Int("requests", requests),
		slog.Int("sub_issues", len(issues)),
	)
	return issues, nil
}

// AddSubIssue attaches an existing issue to a parent as one of its sub-issues.
//
// parent is the parent's issue *number*. subIssueID is the child's database
// *id* — [Issue.ID], which [Client.CreateIssue] returns, and never
// [Issue.Number]. The two are checked by different validators here for no
// reason other than to make the difference visible at the call site; the
// forge's only defence against a swap is that it links whatever it was told to.
//
// Attaching an issue that is already this parent's sub-issue is not an error
// GitHub raises, so a replayed batch is harmless — but the batch does not rely
// on that: [Client.CreateSubIssues] checks the parent's sub-issues first, which
// also avoids the secondary rate limit that repeated writes here attract.
func (c *Client) AddSubIssue(ctx context.Context, repo Repo, parent int, subIssueID int64) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if err := checkIssueNumber(parent); err != nil {
		return err
	}
	if err := checkIssueID(subIssueID); err != nil {
		return err
	}

	if _, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.SubIssue, *gogithub.Response, error) {
		// ReplaceParent is deliberately left unset. An issue that already has a
		// parent is one somebody arranged, and moving it would be litefactory
		// rearranging a board it was not asked to touch; the 422 that comes
		// back instead is the honest answer.
		return c.api.SubIssue.Add(ctx, repo.Owner, repo.Name, int64(parent),
			gogithub.SubIssueRequest{SubIssueID: subIssueID})
	}); err != nil {
		return fmt.Errorf("github: attach issue id %d to %s#%d as a sub-issue: %w",
			subIssueID, repo, parent, err)
	}

	c.logger().InfoContext(ctx, "github: attached a sub-issue",
		slog.String("repo", repo.String()),
		slog.Int("parent", parent),
		slog.Int64("sub_issue_id", subIssueID),
	)
	return nil
}

// CreateSubIssues files one sub-issue per story on parent, skipping the stories
// that already have one, and reports what happened to each.
//
// # What the two returns mean
//
// The error return is for a batch that could not be attempted at all: a
// malformed repository or parent, a spec this package will not send, or a
// failure to list the parent's existing sub-issues — without that listing there
// is no way to tell a re-run from a first run, and creating on that basis is
// how a plan ends up filed twice.
//
// Everything that happened *while* filing is in the results, one per spec in
// the order given. A failure is a [SubIssueResult.Err] on the story it happened
// to, not an error return, because the stories before it are real issues on the
// forge and the caller has to be able to say so (AC-C1.5).
//
// # It stops at the first failure
//
// The remaining stories are left [SubIssueNotAttempted] rather than tried. The
// first failure is usually a fact about the batch and not about the story — a
// rate limit, a revoked token, an archived repository — and the endpoint that
// creates content is the one GitHub documents a secondary rate limit on, so
// carrying on is the behaviour most likely to turn a pause into a ban. The rest
// of the plan is filed by running this again.
func (c *Client) CreateSubIssues(ctx context.Context, repo Repo, parent int, specs []SubIssueSpec) ([]SubIssueResult, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if err := checkIssueNumber(parent); err != nil {
		return nil, err
	}
	if err := checkSubIssueSpecs(specs); err != nil {
		return nil, fmt.Errorf("github: create the sub-issues of %s#%d: %w", repo, parent, err)
	}

	existing, err := c.ListSubIssues(ctx, repo, parent)
	if err != nil {
		return nil, err
	}

	results := make([]SubIssueResult, len(specs))
	for i, spec := range specs {
		results[i] = SubIssueResult{StoryID: spec.StoryID}
	}

	stopped := false
	for i, spec := range specs {
		if issue, ok := findSubIssue(existing, spec.Marker); ok {
			results[i] = SubIssueResult{
				StoryID: spec.StoryID,
				Outcome: SubIssueExisted,
				Number:  issue.Number,
				ID:      issue.ID,
				HTMLURL: issue.HTMLURL,
			}
			continue
		}

		if spec.AttachID != 0 {
			if err := c.AddSubIssue(ctx, repo, parent, spec.AttachID); err != nil {
				results[i].Outcome = SubIssueUnattached
				results[i].ID = spec.AttachID
				results[i].Err = err
				stopped = true
				break
			}
			results[i].Outcome = SubIssueAttached
			results[i].ID = spec.AttachID
			continue
		}

		issue, err := c.CreateIssue(ctx, repo, spec.Title, spec.Body)
		if err != nil {
			results[i].Outcome = SubIssueFailed
			results[i].Err = err
			stopped = true
			break
		}
		results[i].Number = issue.Number
		results[i].ID = issue.ID
		results[i].HTMLURL = issue.HTMLURL

		// From here the issue exists whatever happens next, which is the whole
		// reason the attach failure has an outcome of its own: reporting it as
		// a plain failure would send the next run to create it again.
		if err := c.AddSubIssue(ctx, repo, parent, issue.ID); err != nil {
			results[i].Outcome = SubIssueUnattached
			results[i].Err = err
			stopped = true
			break
		}
		results[i].Outcome = SubIssueCreated
	}

	created, attached, existedCount, failed := countOutcomes(results)
	c.logger().InfoContext(ctx, "github: filed the plan's sub-issues",
		slog.String("repo", repo.String()),
		slog.Int("parent", parent),
		slog.Int("stories", len(specs)),
		slog.Int("created", created),
		slog.Int("attached", attached),
		slog.Int("existed", existedCount),
		slog.Int("failed", failed),
		slog.Bool("stopped_early", stopped),
	)
	return results, nil
}

// countOutcomes summarises the results for the one log line this batch writes.
func countOutcomes(results []SubIssueResult) (created, attached, existed, failed int) {
	for _, result := range results {
		switch result.Outcome {
		case SubIssueCreated:
			created++
		case SubIssueAttached:
			attached++
		case SubIssueExisted:
			existed++
		case SubIssueFailed, SubIssueUnattached:
			failed++
		case SubIssueNotAttempted:
		}
	}
	return created, attached, existed, failed
}

// findSubIssue returns the existing sub-issue carrying marker.
//
// The first match wins. Two sub-issues of one parent carrying the same story
// marker cannot arise from this code — the check that finds them is what stops
// the second being made — so a duplicate means somebody filed one by hand, and
// picking the first keeps a re-run idempotent rather than making it a third.
func findSubIssue(issues []Issue, marker string) (Issue, bool) {
	for _, issue := range issues {
		if hasMarkerLine(issue.Body, marker) {
			return issue, true
		}
	}
	return Issue{}, false
}

// hasMarkerLine reports whether any line of body begins with marker.
//
// Line-anchored rather than a substring search, exactly as turn.HasMarker is
// and for the same reason: GitHub's quote-reply puts "> " in front of every
// quoted line, so a marker somebody quoted into a comment on the issue is not
// the issue's own marker. Over-matching here would mean a story silently never
// gets filed, which is the failure that looks like nothing happening.
func hasMarkerLine(body, marker string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	return false
}

// checkSubIssueSpecs refuses a batch before any of it is sent.
//
// Everything checkable is checked here rather than story by story inside the
// loop, because a spec this package will not send is a caller bug, and finding
// the eighth one after seven issues have been filed makes a programming mistake
// into forge state somebody has to clean up.
func checkSubIssueSpecs(specs []SubIssueSpec) error {
	if len(specs) == 0 {
		return fmt.Errorf("%w: no stories to file", ErrInvalidArgument)
	}

	stories := make(map[string]bool, len(specs))
	markers := make(map[string]bool, len(specs))
	for i, spec := range specs {
		switch {
		case spec.StoryID == "":
			return fmt.Errorf("%w: story %d has no id", ErrInvalidArgument, i)
		case stories[spec.StoryID]:
			return fmt.Errorf("%w: story %q appears twice", ErrInvalidArgument, spec.StoryID)
		case spec.Marker == "":
			return fmt.Errorf("%w: story %q has no marker", ErrInvalidArgument, spec.StoryID)
		case markers[spec.Marker]:
			// Two stories sharing a marker would make the second
			// indistinguishable from the first on the next run: one of them
			// would be found as "already filed" forever and never exist.
			return fmt.Errorf("%w: stories %q and another share a marker", ErrInvalidArgument, spec.StoryID)
		}
		if err := checkIssueTitle(spec.Title); err != nil {
			return fmt.Errorf("story %q: %w", spec.StoryID, err)
		}
		if err := checkIssueBody(spec.Body); err != nil {
			return fmt.Errorf("story %q: %w", spec.StoryID, err)
		}
		// Byte-zero, not merely any-line: this is what SubIssueSpec.Marker's
		// doc comment promises ("what Body begins with"), and it is a
		// tighter check than findSubIssue's own hasMarkerLine deliberately —
		// findSubIssue reads real GitHub bodies a human could have edited
		// anywhere in the text, but a spec this package is about to send was
		// built by this same run and has no excuse for the marker sitting
		// anywhere but the first byte (CR-A-010).
		if !strings.HasPrefix(spec.Body.String(), spec.Marker) {
			return fmt.Errorf("%w: story %q's body does not begin with its marker", ErrInvalidArgument, spec.StoryID)
		}
		if spec.AttachID != 0 {
			if err := checkIssueID(spec.AttachID); err != nil {
				return fmt.Errorf("story %q: %w", spec.StoryID, err)
			}
		}
		stories[spec.StoryID] = true
		markers[spec.Marker] = true
	}
	return nil
}
