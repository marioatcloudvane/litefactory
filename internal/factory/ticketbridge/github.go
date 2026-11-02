// Package ticketbridge owns compatibility translations into the retained workflow representation.
// It confers no ticket or code authority. Remove it when workflow serializers use qualified ticket values.
package ticketbridge

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/ticket"
	"strconv"
)

// GitHubSnapshot preserves the persisted GitHub workflow representation;
// all observations and identities are verified by the ticket adapter first.
func GitHubSnapshot(s ticket.Snapshot) (github.Issue, error) {
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

// GitHubLocator translates the legacy numeric workflow locator.
func GitHubLocator(origin ticket.Origin, repo github.Repo, number int) ticket.Locator {
	return ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.String(), Key: strconv.Itoa(number)}
}
