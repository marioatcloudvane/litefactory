package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type ticketDiscovery interface {
	Discover(context.Context, string, time.Time) ([]ticket.Snapshot, error)
	RepliesAfter(context.Context, ticket.Ref, ticket.Cursor) (ticket.ReplyPage, error)
}

// These conversions preserve the persisted GitHub workflow representation;
// all observations and identities are verified by the ticket adapter first.
func legacyTicketSnapshot(s ticket.Snapshot) (github.Issue, error) {
	if err := s.Ref.Validate(); err != nil {
		return github.Issue{}, err
	}
	if s.Ref.Identity.Provider != ticket.GitHub {
		return github.Issue{}, fmt.Errorf("daemon: unsupported legacy ticket provider")
	}
	for _, a := range append([]ticket.Actor{s.Author}, s.Assignees...) {
		if a.Identity.RemoteID != "" && (a.Identity.Provider != s.Ref.Identity.Provider || a.Identity.Origin != s.Ref.Identity.Origin) {
			return github.Issue{}, fmt.Errorf("daemon: ticket actor belongs to another instance")
		}
	}

	number, err := strconv.Atoi(s.Ref.Locator.Key)
	if err != nil {
		return github.Issue{}, err
	}
	id, err := strconv.ParseInt(s.Ref.Identity.RemoteID, 10, 64)
	if err != nil {
		return github.Issue{}, err
	}
	user := func(a ticket.Actor) github.User {
		id, _ := strconv.ParseInt(a.Identity.RemoteID, 10, 64)
		return github.User{ID: id, Login: a.Handle, IsBot: a.IsBot}
	}
	out := github.Issue{APIURL: string(s.Ref.Identity.Origin) + "/repos/" + s.Ref.Locator.Container + "/issues/" + s.Ref.Locator.Key, ID: id, Number: number, Title: s.Title, Body: s.Body, State: s.State, StateReason: s.StateReason, Author: user(s.Author), Labels: s.Labels, HTMLURL: s.BrowserURL, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	for _, a := range s.Assignees {
		out.Assignees = append(out.Assignees, user(a))
	}
	return out, nil
}

func locatorAt(origin ticket.Origin, repo github.Repo, number int) ticket.Locator {
	return ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.String(), Key: strconv.Itoa(number)}
}
func (d *Daemon) readIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	ingress, origin, selectionErr := d.connectionIngress(ctx, repo, number)
	if selectionErr != nil {
		return github.Issue{}, selectionErr
	}
	if ingress == nil {
		return d.forge.GetIssue(ctx, repo, number)
	}
	s, err := d.resolvedTicket(ctx, repo, number)
	if err != nil {
		return github.Issue{}, err
	}
	expected, e := d.ticketLocator(ctx, ingress, origin, repo, number)
	if e != nil {
		return github.Issue{}, e
	}
	if s.Ref.Locator != expected {
		return github.Issue{}, fmt.Errorf("daemon: resolved ticket destination differs")
	}
	return d.legacySnapshot(ctx, s)
}
func (d *Daemon) discoverIssues(ctx context.Context, repo github.Repo, since time.Time) ([]github.Issue, error) {
	ingress, origin, selectionErr := d.connectionIngress(ctx, repo, 0)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if ingress == nil {
		return d.forge.ListIssues(ctx, repo, since)
	}
	adapter, ok := ingress.(ticketDiscovery)
	if !ok {
		return nil, fmt.Errorf("daemon: ticket discovery contract is required")
	}
	snapshots, err := adapter.Discover(ctx, repo.String(), since)
	if err != nil {
		return nil, err
	}
	out := make([]github.Issue, 0, len(snapshots))
	for _, s := range snapshots {
		if s.Ref.Locator.Origin != origin || s.Ref.Locator.Container != repo.String() {
			return nil, fmt.Errorf("daemon: discovered ticket destination differs")
		}
		i, err := d.legacySnapshot(ctx, s)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}
func (d *Daemon) readComments(ctx context.Context, repo github.Repo, number int, after int64) ([]github.Comment, error) {
	ingress, _, selectionErr := d.connectionIngress(ctx, repo, number)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if ingress == nil {
		return d.forge.ListCommentsAfter(ctx, repo, number, after)
	}
	adapter, ok := ingress.(ticketDiscovery)
	if !ok {
		return nil, fmt.Errorf("daemon: ticket reply contract is required")
	}
	s, err := d.resolvedTicket(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if s.Ref.Identity.Provider == ticket.Linear {
		page, e := adapter.RepliesAfter(ctx, s.Ref, ticket.Cursor{})
		if e != nil {
			return nil, e
		}
		if !page.Complete {
			return nil, fmt.Errorf("daemon: incomplete Linear reply traversal")
		}
		var out []github.Comment
		for _, r := range page.Replies {
			if r.Ticket != s.Ref {
				return nil, store.ErrTicketIdentityConflict
			}
			c, e := d.legacyReply(ctx, r)
			if e != nil {
				return nil, e
			}
			if c.ID > after {
				out = append(out, c)
			}
		}
		return out, nil
	}
	cursor, err := github.ReplyCursor(s.Ref, after)
	if err != nil {
		return nil, err
	}
	page, err := adapter.RepliesAfter(ctx, s.Ref, cursor)
	if err != nil {
		return nil, err
	}
	if !page.Complete {
		return nil, fmt.Errorf("daemon: incomplete ticket reply traversal")
	}
	out := make([]github.Comment, 0, len(page.Replies))
	for _, r := range page.Replies {
		if !r.Ticket.Equal(s.Ref) || r.Identity.Provider != s.Ref.Identity.Provider || r.Identity.Origin != s.Ref.Identity.Origin || (r.Author.Identity.RemoteID != "" && (r.Author.Identity.Provider != s.Ref.Identity.Provider || r.Author.Identity.Origin != s.Ref.Identity.Origin)) {
			return nil, fmt.Errorf("daemon: reply belongs to another ticket instance")
		}
		id, err := strconv.ParseInt(r.Identity.RemoteID, 10, 64)
		if err != nil {
			return nil, err
		}
		authorID, _ := strconv.ParseInt(r.Author.Identity.RemoteID, 10, 64)
		out = append(out, github.Comment{ID: id, Author: github.User{ID: authorID, Login: r.Author.Handle, IsBot: r.Author.IsBot}, Body: r.Body, HTMLURL: r.BrowserURL, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, APIURL: string(s.Ref.Identity.Origin) + "/repos/" + repo.String() + "/issues/comments/" + strconv.FormatInt(id, 10), IssueAPIURL: string(s.Ref.Identity.Origin) + "/repos/" + repo.String() + "/issues/" + strconv.Itoa(number)})
	}
	return out, nil
}

func (d *Daemon) bindObservedIssue(ctx context.Context, sessionID int64, repo github.Repo, issue github.Issue) error {
	ingress, origin, selectionErr := d.connectionIngress(ctx, repo, issue.Number)
	if selectionErr != nil {
		return selectionErr
	}
	if ingress == nil {
		return nil
	}
	ref, e := d.bindingRef(ctx, ingress, origin, repo, issue)
	if e != nil {
		return e
	}
	locator := ref.Locator
	var err error
	if locator.Provider == ticket.GitHub {
		_, err = d.db.BindLegacySessionOrigin(ctx, sessionID, locator.Origin)
	}
	if err != nil {
		return err
	}

	_, err = d.db.BindSessionTicket(ctx, sessionID, ref)
	return err
}

type ticketCommentDiscovery interface {
	DiscoverCommented(context.Context, string, time.Time) ([]ticket.Locator, error)
}

func (d *Daemon) discoverCommented(ctx context.Context, repo github.Repo, since time.Time) ([]int, error) {
	ingress, origin, selectionErr := d.connectionIngress(ctx, repo, 0)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if ingress == nil {
		return d.forge.ListCommentedIssues(ctx, repo, since)
	}
	adapter, ok := ingress.(ticketCommentDiscovery)
	if !ok {
		return nil, fmt.Errorf("daemon: commented ticket discovery contract is required")
	}
	locators, err := adapter.DiscoverCommented(ctx, repo.String(), since)
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(locators))
	for _, l := range locators {
		if err := l.Validate(); err != nil {
			return nil, err
		}
		expected := locatorAt(origin, repo, 1)
		expected.Provider = ingressProvider(ingress)
		if l.Provider != expected.Provider || l.Origin != expected.Origin || l.Container != expected.Container {
			return nil, fmt.Errorf("daemon: commented ticket destination differs")
		}
		n, err := strconv.Atoi(l.Key)
		if l.Provider == ticket.Linear {
			snapshot, e := ingress.Resolve(ctx, l)
			if e != nil {
				return nil, e
			}
			slot, e := d.db.LocalTicketSlot(ctx, "issue", snapshot.Ref.Identity)
			n = int(slot)
			err = e
		}
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("daemon: invalid legacy ticket number")
		}
		out = append(out, n)
	}
	return out, nil
}

func (d *Daemon) resolvedTicket(ctx context.Context, repo github.Repo, number int) (ticket.Snapshot, error) {
	ingress, origin, selectionErr := d.connectionIngress(ctx, repo, number)
	if selectionErr != nil {
		return ticket.Snapshot{}, selectionErr
	}
	locator, e := d.ticketLocator(ctx, ingress, origin, repo, number)
	if e != nil {
		return ticket.Snapshot{}, e
	}
	snapshot, err := ingress.Resolve(ctx, locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if snapshot.Ref.Locator != locator {
		return ticket.Snapshot{}, fmt.Errorf("daemon: resolved ticket destination differs")
	}
	session, err := d.db.GetSessionByTicketLocator(ctx, snapshot.Ref.Locator)
	if errors.Is(err, store.ErrNotFound) {
		return snapshot, nil
	}
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if snapshot.Ref.Identity.Provider == ticket.GitHub {
		_, err = d.db.BindLegacySessionOrigin(ctx, session.ID, snapshot.Ref.Identity.Origin)
	}
	if err != nil {
		return ticket.Snapshot{}, err
	}
	_, err = d.db.BindSessionTicket(ctx, session.ID, snapshot.Ref)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	return snapshot, nil
}
