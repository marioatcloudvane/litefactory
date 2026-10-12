package linear

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

type discoveryPosition struct {
	Stream, Since, After string
	Limit                int
}

func (c *Client) position(container, stream string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (discoveryPosition, error) {
	p := discoveryPosition{Stream: stream, Since: since.UTC().Format(time.RFC3339Nano), Limit: limit}
	if container != c.Container() || limit < 1 || limit > 100 {
		return p, failure("discovery", ticket.Refused, "invalid source or page bound")
	}
	if cursor != (ticket.DiscoveryCursor{}) {
		var saved discoveryPosition
		if cursor.Provider != Provider || cursor.Origin != c.origin || cursor.Container != container || cursor.Kind != "linear-discovery/v1" || json.Unmarshal([]byte(cursor.Value), &saved) != nil || saved.Stream != stream || saved.Since != p.Since || saved.Limit != limit || saved.After == "" {
			return p, &ticket.Failure{Operation: "discovery", Kind: ticket.Refused, Cause: ticket.ErrInvalidCursor}
		}
		p = saved
	}
	return p, nil
}
func (c *Client) next(p discoveryPosition, info pageInfo) (ticket.DiscoveryCursor, error) {
	if !info.HasNextPage {
		return ticket.DiscoveryCursor{}, nil
	}
	if info.EndCursor == "" || info.EndCursor == p.After {
		return ticket.DiscoveryCursor{}, failure("discovery", ticket.Unavailable, "provider continuation missing or repeated")
	}
	p.After = info.EndCursor
	raw, _ := json.Marshal(p)
	return ticket.DiscoveryCursor{Provider: Provider, Origin: c.origin, Container: c.Container(), Kind: "linear-discovery/v1", Value: string(raw)}, nil
}
func (c *Client) filter(since time.Time) map[string]any {
	f := map[string]any{"team": map[string]any{"id": map[string]string{"eq": c.config.TeamID}}, "project": map[string]any{"id": map[string]string{"eq": c.config.ProjectID}}}
	if !since.IsZero() {
		f["updatedAt"] = map[string]string{"gte": since.UTC().Format(time.RFC3339Nano)}
	}
	return f
}
func (c *Client) issuePage(ctx context.Context, container, stream string, since time.Time, cursor ticket.DiscoveryCursor, limit int, extra map[string]any) (ticket.ChangePage, error) {
	p, err := c.position(container, stream, since, cursor, limit)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	filter := c.filter(since)
	for key, value := range extra {
		filter[key] = value
	}
	// Nested label pages multiply GraphQL complexity; retain caller cursor
	// metadata while requesting a smaller complete continuation page.
	first := min(limit, 50)
	var data struct {
		Issues struct {
			Nodes    []issue
			PageInfo pageInfo
		}
	}
	err = c.query(ctx, "discovery", "query($filter: IssueFilter!, $after: String, $first: Int!) { issues(filter: $filter, after: $after, first: $first, includeArchived: true, orderBy: updatedAt) { nodes { "+issueFields+" } pageInfo { hasNextPage endCursor } } }", map[string]any{"filter": filter, "after": nullable(p.After), "first": first}, false, &data)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	if len(data.Issues.Nodes) > first {
		return ticket.ChangePage{}, failure("discovery", ticket.Unavailable, "provider exceeded page bound")
	}
	out := ticket.ChangePage{Complete: !data.Issues.PageInfo.HasNextPage}
	out.Next, err = c.next(p, data.Issues.PageInfo)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	for _, i := range data.Issues.Nodes {
		s, err := c.snapshot(i)
		if err != nil {
			return ticket.ChangePage{}, err
		}
		out.Tickets = append(out.Tickets, s)
	}
	return out, nil
}

// ChangesPage reads one bounded all-state issue change page.
func (c *Client) ChangesPage(ctx context.Context, container string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	return c.issuePage(ctx, container, "changes", since, cursor, limit, nil)
}

// ReconcilePage reads one bounded retained-issue reconciliation page.
func (c *Client) ReconcilePage(ctx context.Context, container string, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	return c.issuePage(ctx, container, "reconcile", time.Time{}, cursor, limit, nil)
}

// ReplyChangesPage reads comment changes independently of issue timestamps, including threaded replies.
func (c *Client) ReplyChangesPage(ctx context.Context, container string, since time.Time, cursor ticket.DiscoveryCursor, limit int) (ticket.ChangePage, error) {
	p, err := c.position(container, "replies", since, cursor, limit)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	filter := map[string]any{"issue": c.filter(time.Time{})}
	if !since.IsZero() {
		filter["updatedAt"] = map[string]string{"gte": since.UTC().Format(time.RFC3339Nano)}
	}
	var data struct {
		Comments struct {
			Nodes    []comment
			PageInfo pageInfo
		}
	}
	err = c.query(ctx, "reply discovery", "query($filter: CommentFilter!, $after: String, $first: Int!) { comments(filter: $filter, after: $after, first: $first, includeArchived: true) { nodes { "+commentFields+" } pageInfo { hasNextPage endCursor } } }", map[string]any{"filter": filter, "after": nullable(p.After), "first": limit}, false, &data)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	if len(data.Comments.Nodes) > limit {
		return ticket.ChangePage{}, failure("reply discovery", ticket.Unavailable, "provider exceeded page bound")
	}
	out := ticket.ChangePage{Complete: !data.Comments.PageInfo.HasNextPage}
	out.Next, err = c.next(p, data.Comments.PageInfo)
	if err != nil {
		return ticket.ChangePage{}, err
	}
	seen := map[string]bool{}
	for _, item := range data.Comments.Nodes {
		if item.Issue == nil || item.Issue.ID == "" {
			return ticket.ChangePage{}, failure("reply discovery", ticket.Conflict, "reply issue missing")
		}
		ref := ticket.Ref{Identity: c.identity(item.Issue.ID), Locator: c.locator(item.Issue.ID)}
		if _, err := c.reply(ref, item); err != nil {
			return ticket.ChangePage{}, err
		}
		if !seen[item.Issue.ID] {
			out.Locators = append(out.Locators, ref.Locator)
			seen[item.Issue.ID] = true
		}
	}
	return out, nil
}
func (c *Client) collect(ctx context.Context, container, stream string, since time.Time, extra map[string]any) ([]ticket.Snapshot, error) {
	var out []ticket.Snapshot
	var cursor ticket.DiscoveryCursor
	for range 100 {
		page, err := c.issuePage(ctx, container, stream, since, cursor, 100, extra)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Tickets...)
		if page.Complete {
			return out, nil
		}
		cursor = page.Next
	}
	return nil, failure("discovery", ticket.Unavailable, "discovery page bound exceeded")
}

// Discover completes bounded issue discovery without claiming durable coverage.
func (c *Client) Discover(ctx context.Context, container string, since time.Time) ([]ticket.Snapshot, error) {
	return c.collect(ctx, container, "discover", since, nil)
}

// Followups includes closed issues for creation reconciliation.
func (c *Client) Followups(ctx context.Context, container string) ([]ticket.Snapshot, error) {
	return c.collect(ctx, container, "followups", time.Time{}, nil)
}

// Children reads direct children in the configured team/project.
func (c *Client) Children(ctx context.Context, parent ticket.Ref) ([]ticket.Snapshot, error) {
	if _, err := c.Read(ctx, parent); err != nil {
		return nil, err
	}
	return c.collect(ctx, parent.Locator.Container, "children:"+parent.Identity.RemoteID, time.Time{}, map[string]any{"parent": map[string]any{"id": map[string]string{"eq": parent.Identity.RemoteID}}})
}

// DiscoverCommented returns verified source locators from bounded comment discovery.
func (c *Client) DiscoverCommented(ctx context.Context, container string, since time.Time) ([]ticket.Locator, error) {
	var out []ticket.Locator
	var cursor ticket.DiscoveryCursor
	for range 100 {
		page, err := c.ReplyChangesPage(ctx, container, since, cursor, 100)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Locators...)
		if page.Complete {
			return out, nil
		}
		cursor = page.Next
	}
	return nil, failure("reply discovery", ticket.Unavailable, "discovery page bound exceeded")
}

// IssuesPage keeps the legacy page surface bounded; opaque cursors are resolved
// by replaying at most the requested finite number of pages.
func (c *Client) IssuesPage(ctx context.Context, container, state string, page, limit int) (ticket.SnapshotPage, error) {
	if page < 1 || page > 100 {
		return ticket.SnapshotPage{}, failure("issue page", ticket.Refused, "invalid page")
	}
	extra := map[string]any{}
	switch state {
	case "open":
		extra["state"] = map[string]any{"type": map[string]any{"nin": []string{"completed", "canceled"}}}
	case "closed":
		extra["state"] = map[string]any{"type": map[string]any{"in": []string{"completed", "canceled"}}}
	case "all":
	default:
		return ticket.SnapshotPage{}, failure("issue page", ticket.Refused, "invalid state")
	}
	var cursor ticket.DiscoveryCursor
	for index := 1; index <= page; index++ {
		observed, err := c.issuePage(ctx, container, "issue-page:"+state, time.Time{}, cursor, limit, extra)
		if err != nil {
			return ticket.SnapshotPage{}, err
		}
		if index == page {
			out := ticket.SnapshotPage{Tickets: observed.Tickets, More: !observed.Complete}
			if out.More {
				out.NextPage = page + 1
			}
			return out, nil
		}
		if observed.Complete {
			return ticket.SnapshotPage{Tickets: []ticket.Snapshot{}}, nil
		}
		cursor = observed.Next
	}
	return ticket.SnapshotPage{}, failure("issue page", ticket.Unavailable, strconv.Itoa(page))
}

// OpenContext reads one bounded open-issue context page.
func (c *Client) OpenContext(ctx context.Context, container string, limit int) (ticket.SnapshotPage, error) {
	return c.IssuesPage(ctx, container, "open", 1, limit)
}
