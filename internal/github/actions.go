package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v90/github"
)

// ActionsTiming contains only server timing facts, independent of the shared
// client's existing classified errors and retry policy.
type ActionsTiming struct {
	RetryAfter, RateLimitReset time.Time
	PollInterval               time.Duration
	RateLimited                bool
}

// ActionsRepository is repository identity observed for the CI target.
type ActionsRepository struct {
	ID       int64
	FullName string
}

// ActionsWorkflow is exact workflow identity and readiness metadata.
type ActionsWorkflow struct {
	ID                int64
	Path, Name, State string
}

// ActionsRun retains rerun identity without exposing SDK objects.
type ActionsRun struct {
	ID, WorkflowID, RunNumber, Attempt       int64
	HeadSHA, Branch, Status, Conclusion, URL string
	CreatedAt, UpdatedAt                     time.Time
}

// ActionsRunPage is a single bounded page; callers establish completeness.
type ActionsRunPage struct {
	Runs            []ActionsRun
	Total, NextPage int
}

func actionsTiming(resp *gogithub.Response, err error, now time.Time) ActionsTiming {
	out := ActionsTiming{RateLimited: errors.Is(err, ErrRateLimited)}
	if resp == nil || resp.Response == nil {
		return out
	}
	h := resp.Header
	if s := h.Get("Retry-After"); s != "" {
		if seconds, e := strconv.ParseInt(s, 10, 64); e == nil && seconds >= 0 && seconds <= (1<<63-1)/int64(time.Second) {
			out.RetryAfter = now.Add(time.Duration(seconds) * time.Second)
		} else if at, e := http.ParseTime(s); e == nil {
			out.RetryAfter = at
		}
	}
	if seconds, e := strconv.ParseInt(h.Get("X-Poll-Interval"), 10, 64); e == nil && seconds > 0 && seconds <= (1<<63-1)/int64(time.Second) {
		out.PollInterval = time.Duration(seconds) * time.Second
	}
	// A reset on an ordinary response with remaining quota is informational.
	if h.Get("X-RateLimit-Remaining") == "0" || out.RateLimited {
		if seconds, e := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); e == nil && seconds > 0 {
			out.RateLimitReset = time.Unix(seconds, 0).UTC()
		}
	}
	return out
}

// InspectActionsRepository reads exact repository identity and safe timing facts.
func (c *Client) InspectActionsRepository(ctx context.Context, repo Repo) (ActionsRepository, ActionsTiming, error) {
	if err := repo.Validate(); err != nil {
		return ActionsRepository{}, ActionsTiming{}, err
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) (*gogithub.Repository, *gogithub.Response, error) {
		return c.api.Repositories.Get(ctx, repo.Owner, repo.Name)
	})
	timing := actionsTiming(resp, err, c.actionNow())
	if err != nil {
		return ActionsRepository{}, timing, err
	}
	if raw == nil || raw.GetID() <= 0 || !strings.EqualFold(raw.GetFullName(), repo.String()) {
		return ActionsRepository{}, timing, fmt.Errorf("github: invalid Actions repository identity: %w", ErrUnexpectedStatus)
	}
	return ActionsRepository{ID: raw.GetID(), FullName: raw.GetFullName()}, timing, nil
}

// InspectActionsWorkflow resolves the configured workflow file without title discovery.
func (c *Client) InspectActionsWorkflow(ctx context.Context, repo Repo, workflow string) (ActionsWorkflow, ActionsTiming, error) {
	if err := repo.Validate(); err != nil {
		return ActionsWorkflow{}, ActionsTiming{}, err
	}
	if !strings.HasPrefix(workflow, ".github/workflows/") || path.Clean(workflow) != workflow || strings.Contains(workflow, "\\") || strings.Count(workflow, "/") != 2 {
		return ActionsWorkflow{}, ActionsTiming{}, ErrInvalidArgument
	}
	file := path.Base(workflow)
	if err := checkPathSegment("workflow filename", file); err != nil {
		return ActionsWorkflow{}, ActionsTiming{}, err
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) (*gogithub.Workflow, *gogithub.Response, error) {
		return c.api.Actions.GetWorkflowByFileName(ctx, repo.Owner, repo.Name, file)
	})
	timing := actionsTiming(resp, err, c.actionNow())
	if err != nil {
		return ActionsWorkflow{}, timing, err
	}
	if raw == nil || raw.GetID() <= 0 || raw.GetPath() != workflow {
		return ActionsWorkflow{}, timing, fmt.Errorf("github: invalid Actions workflow identity: %w", ErrUnexpectedStatus)
	}
	return ActionsWorkflow{ID: raw.GetID(), Path: raw.GetPath(), Name: raw.GetName(), State: raw.GetState()}, timing, nil
}

// ListActionsRuns reads one branch-filtered page without assuming completeness.
func (c *Client) ListActionsRuns(ctx context.Context, repo Repo, workflowID int64, branch, created string, page int) (ActionsRunPage, ActionsTiming, error) {
	if err := repo.Validate(); err != nil {
		return ActionsRunPage{}, ActionsTiming{}, err
	}
	if workflowID <= 0 || branch == "" || page < 1 {
		return ActionsRunPage{}, ActionsTiming{}, ErrInvalidArgument
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) (*gogithub.WorkflowRuns, *gogithub.Response, error) {
		return c.api.Actions.ListWorkflowRunsByID(ctx, repo.Owner, repo.Name, workflowID, &gogithub.ListWorkflowRunsOptions{Branch: branch, Created: created, ListOptions: gogithub.ListOptions{Page: page, PerPage: 100}})
	})
	timing := actionsTiming(resp, err, c.actionNow())
	if err != nil {
		return ActionsRunPage{}, timing, err
	}
	if raw == nil || raw.TotalCount == nil || raw.WorkflowRuns == nil || raw.GetTotalCount() < 0 || len(raw.WorkflowRuns) > 100 || resp == nil {
		return ActionsRunPage{}, timing, ErrUnexpectedStatus
	}
	out := ActionsRunPage{Total: raw.GetTotalCount(), NextPage: resp.NextPage, Runs: make([]ActionsRun, 0, len(raw.WorkflowRuns))}
	for _, r := range raw.WorkflowRuns {
		run, e := actionsRun(r)
		if e != nil {
			return ActionsRunPage{}, timing, e
		}
		if run.WorkflowID != workflowID || run.Branch != branch {
			return ActionsRunPage{}, timing, ErrUnexpectedStatus
		}
		out.Runs = append(out.Runs, run)
	}
	return out, timing, nil
}

// GetActionsRun refreshes an exact run and its current rerun attempt.
func (c *Client) GetActionsRun(ctx context.Context, repo Repo, id int64) (ActionsRun, ActionsTiming, error) {
	if err := repo.Validate(); err != nil {
		return ActionsRun{}, ActionsTiming{}, err
	}
	if id <= 0 {
		return ActionsRun{}, ActionsTiming{}, ErrInvalidArgument
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) (*gogithub.WorkflowRun, *gogithub.Response, error) {
		return c.api.Actions.GetWorkflowRunByID(ctx, repo.Owner, repo.Name, id)
	})
	timing := actionsTiming(resp, err, c.actionNow())
	if err != nil {
		return ActionsRun{}, timing, err
	}
	out, err := actionsRun(raw)
	if err == nil && out.ID != id {
		err = ErrUnexpectedStatus
	}
	return out, timing, err
}
func actionsRun(r *gogithub.WorkflowRun) (ActionsRun, error) {
	if r == nil || r.GetID() <= 0 || r.GetWorkflowID() <= 0 || r.GetRunNumber() <= 0 || r.GetRunAttempt() <= 0 || r.GetCreatedAt().IsZero() || r.GetHeadSHA() == "" || r.GetHeadBranch() == "" || r.GetStatus() == "" {
		return ActionsRun{}, ErrUnexpectedStatus
	}
	return ActionsRun{ID: r.GetID(), WorkflowID: r.GetWorkflowID(), RunNumber: int64(r.GetRunNumber()), Attempt: int64(r.GetRunAttempt()), HeadSHA: r.GetHeadSHA(), Branch: r.GetHeadBranch(), Status: r.GetStatus(), Conclusion: r.GetConclusion(), URL: r.GetHTMLURL(), CreatedAt: r.GetCreatedAt().Time, UpdatedAt: r.GetUpdatedAt().Time}, nil
}
func (c *Client) actionNow() time.Time {
	if c != nil {
		return c.clock()
	}
	return time.Now()
}
