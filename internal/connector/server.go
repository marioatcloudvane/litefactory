// Package connector provides a separate, owner-authorized read-only MCP boundary.
// It never exposes the factory's agent or submission backends.
package connector

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// Scope is the only authority this connector can issue.
const Scope = "factory:overview"

// Options supplies dependencies resolved by trusted daemon composition.
type Options struct {
	Config       config.RemoteConnector
	DB           *store.DB
	Logger       *slog.Logger
	Now          func() time.Time
	LocalControl *LocalControl
	Overview     OverviewProvider
	GitHub       GitHubIdentityProvider
	CookieKey    []byte
	GitHubSecret string
}

// Server owns remote authentication independently of local factory authority.
type Server struct {
	cfg          config.RemoteConnector
	db           *store.DB
	logger       *slog.Logger
	now          func() time.Time
	localControl *LocalControl
	overview     OverviewProvider
	github       GitHubIdentityProvider
	cookie       cipher.AEAD
	transport    *transportState
}

// New refuses construction until security prerequisites and owner binding work.
func New(ctx context.Context, opts Options) (*Server, error) {
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	if !opts.Config.Enabled {
		return nil, errors.New("connector: remote access disabled")
	}
	if opts.DB == nil || opts.Logger == nil || opts.LocalControl == nil {
		return nil, errors.New("connector: required dependency unavailable")
	}
	if len(opts.CookieKey) != 32 {
		return nil, errors.New("connector: cookie key must be 32 bytes")
	}
	if opts.GitHub == nil && opts.GitHubSecret == "" {
		return nil, errors.New("connector: GitHub credential unavailable")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if _, err := opts.DB.ReconcileConnectorOwner(ctx, opts.Config.OwnerID, now()); err != nil {
		return nil, err
	}
	if err := opts.LocalControl.check(ctx, opts.DB, opts.Config.OwnerID); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(opts.CookieKey)
	if err != nil {
		return nil, errors.New("connector: invalid cookie key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("connector: cookie encryption unavailable")
	}
	github := opts.GitHub
	if github == nil {
		github = &GitHubProvider{ClientID: opts.Config.GitHubClientID, ClientSecret: opts.GitHubSecret}
	}
	opts.Config.AllowedOrigins = slices.Clone(opts.Config.AllowedOrigins)
	return &Server{cfg: opts.Config, db: opts.DB, logger: opts.Logger.With("component", "connector"), now: now, localControl: opts.LocalControl, overview: opts.Overview, github: github, cookie: aead, transport: newTransportState()}, nil
}

func randomCredential() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("connector: secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func digest(value string) string {
	b := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(b[:])
}
