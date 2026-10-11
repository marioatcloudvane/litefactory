package ticket_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestCanonicalOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ raw, want string }{
		{"https://API.GITHUB.COM:443/", "https://api.github.com"},
		{"https://GHE.EXAMPLE/api/v3///", "https://ghe.example/api/v3"},
		{"http://[::1]:80/", "http://[::1]"},
	} {
		got, err := ticket.CanonicalOrigin(tc.raw)
		if err != nil || string(got) != tc.want {
			t.Errorf("origin %q: got %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{"", "file:///tmp/api", "https://user:secret@example.com", "https://example.com?q=x", "https://example.com#x", "https://example.com/api/../v3", "https://example.com/api/%2e%2e/v3"} {
		if _, err := ticket.CanonicalOrigin(raw); err == nil {
			t.Errorf("accepted invalid origin %q", raw)
		}
	}
}

func TestQualifiedIdentityNeverMatchesAnotherInstance(t *testing.T) {
	t.Parallel()
	id := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "42"}
	copyID := id
	if !id.Equal(copyID) {
		t.Fatal("valid identity does not match itself")
	}
	for _, other := range []ticket.Identity{
		{}, {Provider: "other", Origin: id.Origin, RemoteID: id.RemoteID},
		{Provider: id.Provider, Origin: "https://enterprise.example/api/v3", RemoteID: id.RemoteID},
		{Provider: id.Provider, Origin: id.Origin, RemoteID: "43"},
		{Provider: id.Provider, Origin: "https://API.GITHUB.COM", RemoteID: id.RemoteID},
	} {
		if id.Equal(other) {
			t.Fatalf("identity collision: %+v", other)
		}
	}
	if (ticket.Identity{}).Equal(ticket.Identity{}) {
		t.Fatal("zero identities match")
	}
	snapshot := ticket.Snapshot{Assignees: []ticket.Actor{{Identity: id, Handle: "renamed"}}}
	if !snapshot.AssignedTo(id) || snapshot.AssignedTo(ticket.Identity{Provider: id.Provider, Origin: "https://other.example", RemoteID: "42"}) {
		t.Fatal("assignment ignores qualified identity")
	}
}

func TestReferenceIdentityAndLocatorAreSeparate(t *testing.T) {
	t.Parallel()
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "1234"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: "https://api.github.com", Container: "owner/repo", Key: "42"}}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	moved := ref
	moved.Locator.Container = "owner/renamed"
	if !ref.Equal(moved) {
		t.Fatal("container rename changes immutable identity")
	}
	moved.Locator.Origin = "https://other.example"
	if moved.Validate() == nil || ref.Equal(moved) {
		t.Fatal("contradictory identity and locator accepted")
	}
}
