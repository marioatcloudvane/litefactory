package daemon

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// RetirementIssues is the narrow consumer interface shared by preview and sweep.
type RetirementIssues interface {
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
}

// RetirementOptions supplies time and observations, keeping preview independent
// of daemon startup, admission and scheduling state.
type RetirementOptions struct {
	Now        time.Time
	RetainDays int
	Issues     RetirementIssues
	ObserveGit func(context.Context, store.RetirementOwner) (git.RetirementObservation, error)
}

// RetirementDecision is an exact resource identity and advisory disposition.
type RetirementDecision struct {
	Resource    string
	Kind        string
	Action      string
	Reason      string
	ExpectedSHA string
}

// RetirementEvaluation retains structured evidence suitable for normal logs.
// Error is a typed cause for request backoff; it must never be treated as closure.
type RetirementEvaluation struct {
	Owner           store.RetirementOwner
	ObservedAt      time.Time
	IssueState      string
	IssueReason     string
	IssueError      string
	RetentionSource string
	RetentionAt     time.Time
	ExpiresAt       time.Time
	Git             git.RetirementObservation
	Decisions       []RetirementDecision
	Error           error
}

// ObserveRetirementGit opens only the registered clone. Fetch and read-only
// probes may update remote tracking refs, never its checkout/index/HEAD.
func ObserveRetirementGit(ctx context.Context, owner store.RetirementOwner) (git.RetirementObservation, error) {
	binary, err := git.Resolve(ctx)
	if err != nil {
		return git.RetirementObservation{}, err
	}
	clone, err := binary.Open(ctx, owner.Repo.Path)
	if err != nil {
		return git.RetirementObservation{}, err
	}
	remote, err := clone.RemoteURL(ctx, "origin")
	if err != nil {
		return git.RetirementObservation{}, err
	}
	if remote != owner.Repo.RemoteURL {
		return git.RetirementObservation{}, fmt.Errorf("registered origin changed")
	}
	return git.ObserveRetirement(ctx, clone, owner.Session.WorktreePath, owner.Session.Branch, owner.Repo.BaseBranch)
}

// RetirementIssueClosed requires an individual successful response identifying
// the expected repository/issue. A URL's path is checked, supporting enterprise
// API prefixes without confusing a redirect to another repository with closure.
func RetirementIssueClosed(repo store.Repo, number int, issue github.Issue) bool {
	if issue.ID <= 0 || issue.Number != number || issue.State != "closed" {
		return false
	}
	parsed, err := url.Parse(issue.APIURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	want := fmt.Sprintf("/repos/%s/%s/issues/%d", repo.Owner, repo.Name, number)
	return strings.HasSuffix(parsed.Path, want) && parsed.RawQuery == "" && parsed.Fragment == ""
}

// EvaluateRetirement is advisory and side-effect free except fresh Git fetches
// and issue reads. Its result is never an authorization token for later deletion.
func EvaluateRetirement(ctx context.Context, opts RetirementOptions, owner store.RetirementOwner) RetirementEvaluation {
	e := RetirementEvaluation{Owner: owner, ObservedAt: opts.Now}
	if owner.Session.WorktreePath != "" {
		e.Decisions = append(e.Decisions, RetirementDecision{Kind: "worktree", Resource: owner.Session.WorktreePath})
	}
	if owner.Session.Branch != "" {
		e.Decisions = append(e.Decisions, RetirementDecision{Kind: "local_branch", Resource: "refs/heads/" + owner.Session.Branch}, RetirementDecision{Kind: "remote_branch", Resource: "refs/heads/" + owner.Session.Branch})
	}
	keep := func(action, reason string) RetirementEvaluation {
		for i := range e.Decisions {
			e.Decisions[i].Action = action
			e.Decisions[i].Reason = reason
		}
		return e
	}
	if owner.ObservationError != nil {
		e.Error = owner.ObservationError
		return keep("unknown", "owner_observation_failed")
	}
	if opts.Issues == nil || opts.ObserveGit == nil || opts.Now.IsZero() || opts.RetainDays < config.MinWorktreeRetainDays || opts.RetainDays > config.MaxWorktreeRetainDays {
		return keep("unknown", "invalid_evaluator_configuration")
	}
	s, r := owner.Session, owner.Repo
	branch, err := naming.Branch(r.FeaturePrefix, s.IssueNumber, s.FeatureSlug)
	if err != nil || !owner.Canonical || r.Forge != store.ForgeGitHub || s.TaskRef != store.TaskRef(r.Forge, r.Owner, r.Name, s.IssueNumber) || s.Branch == "" || branch != s.Branch {
		return keep("kept", "uncertain_ownership")
	}
	if s.WorktreePath != "" {
		path, err := naming.WorktreePath(r.WorktreeRoot, s.IssueNumber)
		if err != nil || path != s.WorktreePath {
			return keep("kept", "uncertain_path_ownership")
		}
	}
	issue, err := opts.Issues.GetIssue(github.WithoutConditionalCache(ctx), github.Repo{Owner: r.Owner, Name: r.Name}, s.IssueNumber)
	e.IssueState = issue.State
	e.IssueReason = issue.StateReason
	if err != nil {
		e.Error = err
		e.IssueError = "issue_lookup_failed"
		return keep("unknown", "issue_lookup_failed")
	}
	if issue.State == github.StateOpen && issue.Number == s.IssueNumber {
		return keep("kept", "issue_open")
	}
	if !RetirementIssueClosed(r, s.IssueNumber, issue) {
		e.IssueError = "unverified_issue_identity_or_state"
		return keep("unknown", e.IssueError)
	}
	if owner.Active {
		return keep("kept", "active_work")
	}
	if owner.Pending {
		return keep("kept", "pending_effects")
	}
	e.RetentionAt = owner.LastFinishedAt
	e.RetentionSource = "last_completed_run"
	if owner.RunCount == 0 {
		e.RetentionAt = s.CreatedAt
		e.RetentionSource = "creation"
	}
	validRetention := !e.RetentionAt.IsZero() && !e.RetentionAt.After(opts.Now)
	if validRetention {
		e.ExpiresAt = e.RetentionAt.Add(time.Duration(opts.RetainDays) * 24 * time.Hour)
	}
	e.Git, err = opts.ObserveGit(ctx, owner)
	if err != nil {
		e.Error = err
		return keep("unknown", "git_verification_failed")
	}
	g := e.Git
	if g.Protected {
		return keep("kept", "protected_branch")
	}
	if !g.WorktreeSafe {
		return keep("kept", g.WorktreeReason)
	}
	if g.Dirty {
		return keep("kept", "local_files")
	}
	if (g.LocalSHA != "" && !g.LocalPreserved) || (g.WorktreeExists && g.LocalSHA == "") {
		return keep("kept", "local_commits_unpreserved")
	}
	for i := range e.Decisions {
		d := &e.Decisions[i]
		d.Action = "kept"
		switch d.Kind {
		case "worktree":
			d.ExpectedSHA = g.WorktreeSHA
			switch {
			case !g.WorktreeExists:
				d.Action = "already_absent"
				d.Reason = "already_absent"
			case !validRetention:
				d.Reason = "unknown_retention"
			case opts.Now.Before(e.ExpiresAt):
				d.Reason = "retention_not_expired"
			default:
				d.Action = "would_remove"
				d.Reason = "expired_and_preserved"
			}
		case "local_branch":
			d.ExpectedSHA = g.LocalSHA
			switch {
			case g.LocalSHA == "":
				d.Action = "already_absent"
				d.Reason = "already_absent"
			case !g.LocalMerged:
				d.Reason = "not_ancestor_of_base"
			case g.OtherWorktreeUsesBranch:
				d.Reason = "branch_in_use"
			case g.BranchInUse && (!validRetention || opts.Now.Before(e.ExpiresAt)):
				d.Reason = "branch_in_use"
			default:
				d.Action = "would_remove"
				d.Reason = "merged_into_base"
			}
		case "remote_branch":
			d.ExpectedSHA = g.RemoteSHA
			switch {
			case g.RemoteSHA == "":
				d.Action = "already_absent"
				d.Reason = "already_absent"
			case !g.RemoteMerged:
				d.Reason = "not_ancestor_of_base"
			default:
				d.Action = "would_remove"
				d.Reason = "merged_into_base"
			}
		}
	}
	return e
}
