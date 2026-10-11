package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
	gogithub "github.com/google/go-github/v90/github"
)

const recoveryCursorVersion = "github-discovery/v1"

type discoveryPosition struct {
	Stream string `json:"stream"`
	Since  string `json:"since"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

func (a *Tickets) discoveryAddress(container, stream string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (Repo, discoveryPosition, error) {
	repo, _, err := a.address(ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"})
	if err != nil {
		return Repo{}, discoveryPosition{}, err
	}
	position := discoveryPosition{Stream: stream, Since: since.UTC().Format(time.RFC3339Nano), Page: 1, Limit: limit}
	if limit < 1 || limit > 100 {
		return Repo{}, position, ticketError("discovery page", ErrInvalidArgument, false)
	}
	if cursor != (ticket.DiscoveryCursor{}) {
		var saved discoveryPosition
		if cursor.Provider != ticket.GitHub || cursor.Origin != a.origin || cursor.Container != container || cursor.Kind != recoveryCursorVersion || json.Unmarshal([]byte(cursor.Value), &saved) != nil || saved.Stream != stream || saved.Since != position.Since || saved.Limit != limit || saved.Page < 1 {
			return Repo{}, position, ticketError("discovery continuation", ErrInvalidArgument, false)
		}
		position = saved
	}
	return repo, position, nil
}

func (a *Tickets) discoveryNext(container string, position discoveryPosition, next int) (ticket.DiscoveryCursor, error) {
	if next == 0 {
		return ticket.DiscoveryCursor{}, nil
	}
	if next <= position.Page {
		return ticket.DiscoveryCursor{}, ticketError("discovery continuation", ErrTooManyPages, false)
	}
	position.Page = next
	value, err := json.Marshal(position)
	if err != nil {
		return ticket.DiscoveryCursor{}, err
	}
	return ticket.DiscoveryCursor{Provider: ticket.GitHub, Origin: a.origin, Container: container, Kind: recoveryCursorVersion, Value: string(value)}, nil
}

// ChangesPage reads one all-state updated-ticket page. A completed page is a
// transport completion, not proof against provider indexing lag; reconciliation
// separately revisits retained tickets. The daemon owns overlap and checkpointing.
func (a *Tickets) ChangesPage(ctx context.Context, container string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	return a.issueChangesPage(ctx, container, "changes", since, cursor, limit)
}

// ReconcilePage reads one stable creation-ordered retained-ticket page, including
// closed tickets. The daemon paces traversal and retains its continuation.
func (a *Tickets) ReconcilePage(ctx context.Context, container string, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	return a.issueChangesPage(ctx, container, "reconcile", time.Time{}, cursor, limit)
}

func (a *Tickets) issueChangesPage(ctx context.Context, container, stream string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	var out ticket.ChangePage
	repo, position, err := a.discoveryAddress(container, stream, since, cursor, limit)
	if err != nil {
		return out, err
	}
	opts := &gogithub.IssueListByRepoOptions{State: "all", Sort: "updated", Direction: "asc", Since: since.UTC(), ListOptions: gogithub.ListOptions{Page: position.Page, PerPage: limit}}
	if stream == "reconcile" {
		opts.Sort = "created"
	}
	payloads, response, err := do(ctx, a.client, func(ctx context.Context) ([]*gogithub.Issue, *gogithub.Response, error) {
		return a.client.api.Issues.ListByRepo(ctx, repo.Owner, repo.Name, opts)
	})
	if err != nil {
		return out, ticketError("discover changes", err, false)
	}
	if len(payloads) > limit {
		return out, ticketError("discover changes", ErrUnexpectedStatus, false)
	}
	for _, payload := range payloads {
		if payload == nil || payload.IsPullRequest() {
			continue
		}
		snapshot, err := a.snapshot(repo, convertIssue(payload))
		if err != nil {
			return ticket.ChangePage{}, err
		}
		out.Tickets = append(out.Tickets, snapshot)
	}
	next := 0
	if response != nil {
		next = response.NextPage
	}
	out.Next, err = a.discoveryNext(container, position, next)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	out.Complete = next == 0
	return out, nil
}

// ReplyChangesPage reads one bounded repository-reply page and returns verified
// source-local locators. Reply identities and author authority are reread through
// the ordinary ticket consumption path rather than inferred from this hint.
func (a *Tickets) ReplyChangesPage(ctx context.Context, container string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	var out ticket.ChangePage
	repo, position, err := a.discoveryAddress(container, "replies", since, cursor, limit)
	if err != nil {
		return out, err
	}
	page, err := a.client.ListRepositoryCommentsPage(ctx, repo, since, position.Page, limit)
	if err != nil {
		return out, ticketError("discover replies", err, false)
	}
	seen := make(map[int]bool)
	for _, item := range page.Comments {
		if !a.matchesAPIURL(item.Comment.IssueAPIURL, repo, "issues/"+strconv.Itoa(item.IssueNumber)) || !a.matchesAPIURL(item.Comment.APIURL, repo, "issues/comments/"+strconv.FormatInt(item.Comment.ID, 10)) || item.Comment.ID < 1 {
			return ticket.ChangePage{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "discover replies", Cause: fmt.Errorf("reply source identity differs")}
		}
		if seen[item.IssueNumber] {
			continue
		}
		seen[item.IssueNumber] = true
		out.Locators = append(out.Locators, ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: strconv.Itoa(item.IssueNumber)})
	}
	out.Next, err = a.discoveryNext(container, position, page.NextPage)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	out.Complete = page.NextPage == 0
	return out, nil
}
