package loganalysis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// IssueHit is one existing issue the analyst can compare a finding with.
type IssueHit struct {
	Reference string   `json:"reference"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Labels    []string `json:"labels,omitempty"`
	URL       string   `json:"url,omitempty"`
}

// Tracker is one issue tracker an analysis may search and file Bugs in.
type Tracker interface {
	// Name identifies the destination, such as github:owner/name.
	Name() string
	Search(ctx context.Context, text string, limit int) ([]IssueHit, error)
	// FileBug creates an issue labelled bug whose body carries marker, unless
	// an issue updated since `since` already carries it; that one is returned.
	FileBug(ctx context.Context, title, body redact.Body, marker string, since time.Time) (IssueHit, error)
}

// NotFiled is a destination's plain reason for not filing at all.
type NotFiled string

func (n NotFiled) Error() string { return string(n) }

// GitHubTracker searches and files issues in one GitHub repository.
type GitHubTracker struct {
	Client *github.Client
	Repo   github.Repo
}

// Name implements Tracker.
func (t GitHubTracker) Name() string { return "github:" + t.Repo.String() }

func gitHubHit(repo github.Repo, i github.Issue) IssueHit {
	return IssueHit{Reference: repo.String() + "#" + strconv.Itoa(i.Number), Title: i.Title, State: i.State, Labels: i.Labels, URL: i.HTMLURL}
}

// Search implements Tracker.
func (t GitHubTracker) Search(ctx context.Context, text string, limit int) ([]IssueHit, error) {
	found, err := t.Client.SearchIssues(ctx, t.Repo, text, limit)
	if err != nil {
		return nil, err
	}
	out := []IssueHit{}
	for _, i := range found {
		out = append(out, gitHubHit(t.Repo, i))
	}
	return out, nil
}

// FileBug implements Tracker. The listing it checks first is the strongly
// consistent issue listing, not search, so an issue created moments ago by an
// interrupted attempt is found.
func (t GitHubTracker) FileBug(ctx context.Context, title, body redact.Body, marker string, since time.Time) (IssueHit, error) {
	recent, err := t.Client.ListIssues(ctx, t.Repo, since.Add(-time.Minute))
	if err != nil {
		return IssueHit{}, err
	}
	for _, i := range recent {
		if hasMarkerLine(i.Body, marker) {
			if !containsLabel(i.Labels, "bug") {
				if err = t.Client.AddIssueLabel(ctx, t.Repo, i.Number, "bug"); err != nil {
					return IssueHit{}, err
				}
			}
			return gitHubHit(t.Repo, i), nil
		}
	}
	created, err := t.Client.CreateIssue(ctx, t.Repo, title, body)
	if err != nil {
		return IssueHit{}, err
	}
	hit := gitHubHit(t.Repo, created)
	if err = t.Client.AddIssueLabel(ctx, t.Repo, created.Number, "bug"); err != nil {
		return hit, fmt.Errorf("created %s but could not label it bug: %w", hit.Reference, err)
	}
	return hit, nil
}

// LinearTracker searches one Linear project. Filing is not supported: a new
// Linear issue cannot yet be labelled as a Bug by litefactory.
type LinearTracker struct{ Client *linear.Client }

// Name implements Tracker.
func (t LinearTracker) Name() string { return "linear:" + t.Client.Container() }

// Search implements Tracker.
func (t LinearTracker) Search(ctx context.Context, text string, limit int) ([]IssueHit, error) {
	found, err := t.Client.SearchIssues(ctx, text, limit)
	if err != nil {
		return nil, err
	}
	out := []IssueHit{}
	for _, s := range found {
		out = append(out, ticketHit(s))
	}
	return out, nil
}

// FileBug implements Tracker.
func (t LinearTracker) FileBug(context.Context, redact.Body, redact.Body, string, time.Time) (IssueHit, error) {
	return IssueHit{}, NotFiled("litefactory cannot yet label a new Linear issue as a Bug; file it by hand")
}

func ticketHit(s ticket.Snapshot) IssueHit {
	return IssueHit{Reference: s.DisplayKey, Title: s.Title, State: s.State, Labels: s.Labels, URL: s.BrowserURL}
}

func hasMarkerLine(body, marker string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, want) {
			return true
		}
	}
	return false
}
