package ticket

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Provider identifies a ticket service, independently of a code host.
type Provider string

// GitHub is the GitHub ticket provider.
const GitHub Provider = "github"

// Linear is the Linear ticket provider.
const Linear Provider = "linear"

// Origin is a canonical API instance root, obtained from trusted configuration.
// Enterprise API path prefixes are part of the origin.
type Origin string

// CanonicalOrigin normalizes a configured API root. It does not establish trust
// in a URL found in ticket text or a provider response.
func CanonicalOrigin(raw string) (Origin, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ticket: invalid API origin: %w", err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("ticket: API origin requires an http(s) root without credentials, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host := u.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("ticket: API origin contains a relative path segment")
		}
	}
	return Origin(u.String()), nil
}

// Identity identifies an immutable remote object. Zero identities never match.
type Identity struct {
	Provider Provider `json:"provider"`
	Origin   Origin   `json:"origin"`
	RemoteID string   `json:"remote_id"`
}

// Validate rejects unresolved identities and noncanonical origins.
func (i Identity) Validate() error {
	if i.Provider == "" || strings.TrimSpace(string(i.Provider)) != string(i.Provider) || strings.TrimSpace(i.RemoteID) == "" || strings.TrimSpace(i.RemoteID) != i.RemoteID {
		return errors.New("ticket: provider and immutable remote ID are required")
	}
	origin, err := CanonicalOrigin(string(i.Origin))
	if err != nil {
		return err
	}
	if origin != i.Origin {
		return errors.New("ticket: API origin is not canonical")
	}
	return nil
}

// Equal compares valid immutable identities across provider and API origin.
func (i Identity) Equal(other Identity) bool {
	return i.Validate() == nil && other.Validate() == nil && i == other
}

// Locator is an address for a verified read, never an immutable identity.
type Locator struct {
	Provider  Provider `json:"provider"`
	Origin    Origin   `json:"origin"`
	Container string   `json:"container"`
	Key       string   `json:"key"`
}

// Validate checks that the locator has a canonical qualified address.
func (l Locator) Validate() error {
	if err := (Identity{Provider: l.Provider, Origin: l.Origin, RemoteID: l.Key}).Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(l.Container) == "" || strings.TrimSpace(l.Container) != l.Container {
		return errors.New("ticket: container is required")
	}
	return nil
}

// Ref binds an immutable ticket identity to a verified locator. Adapters must
// recheck the binding before using the locator for a mutation.
type Ref struct {
	Identity Identity `json:"identity"`
	Locator  Locator  `json:"locator"`
}

// Validate requires the identity and locator to belong to the same instance.
func (r Ref) Validate() error {
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	if err := r.Locator.Validate(); err != nil {
		return err
	}
	if r.Identity.Provider != r.Locator.Provider || r.Identity.Origin != r.Locator.Origin {
		return errors.New("ticket: identity and locator belong to different instances")
	}
	return nil
}

// Equal compares immutable identity, not a mutable display key or container.
func (r Ref) Equal(other Ref) bool {
	return r.Validate() == nil && other.Validate() == nil && r.Identity.Equal(other.Identity)
}
