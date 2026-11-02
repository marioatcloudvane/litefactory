package localadmin

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// StartupOptions supplies trusted local credential and transport dependencies.
type StartupOptions struct {
	Keychain  func(context.Context, string, string) (string, error)
	Transport http.RoundTripper
	UserAgent string
}

// StartupConnection is the verified origin of a retained bootstrap credential.
type StartupConnection struct {
	ID     int64
	Origin ticket.Origin
}

// ResolveProjectStartupToken lets independently configured projects start when
// the obsolete global credential is unavailable. The selected immutable version
// binds the bootstrap token to its original trusted API origin.
func ResolveProjectStartupToken(ctx context.Context, options StartupOptions, db *store.DB, legacyErr error) (secrets.Token, StartupConnection, error) {
	if legacyErr == nil {
		legacyErr = errors.New("no usable project startup credential")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	identity, err := db.GetIdentity(ctx, store.ForgeGitHub, store.RoleBot)
	if err != nil || identity.UserID <= 0 {
		return secrets.Token{}, StartupConnection{}, legacyErr
	}
	projects, err := db.ListRepos(ctx)
	if err != nil {
		return secrets.Token{}, StartupConnection{}, legacyErr
	}
	background := os.Getenv("XPC_SERVICE_NAME") == launchd.DefaultLabel
	resolver := secrets.Resolver{Keychain: options.Keychain}
	for _, project := range projects {
		version, err := db.CurrentConnection(ctx, project.ID, store.TicketConnection)
		if err != nil || !version.Enabled || version.Provider != ticket.GitHub {
			continue
		}
		reference := secrets.Reference{Source: secrets.Source(version.CredentialSource), Service: version.CredentialService, Account: version.CredentialAccount, EnvVar: version.CredentialEnv}
		if reference.Source != secrets.SourceKeychain && reference.Source != secrets.SourceEnv {
			continue
		}
		if background && reference.Source != secrets.SourceKeychain {
			continue
		}
		token, err := resolver.Resolve(ctx, reference)
		if err != nil {
			continue
		}
		client, err := github.New(github.Options{APIBase: string(version.Origin), Token: token, HTTPClient: &http.Client{Timeout: 30 * time.Second, Transport: options.Transport}, UserAgent: options.UserAgent})
		if err != nil {
			continue
		}
		actor, err := client.AuthenticatedUser(ctx)
		if err != nil || actor.ID != identity.UserID {
			continue
		}
		return token, StartupConnection{ID: version.ID, Origin: version.Origin}, nil
	}
	return secrets.Token{}, StartupConnection{}, legacyErr
}
