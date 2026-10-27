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
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// Scope is the read authority behind the factory_overview tool.
const Scope = "factory:overview"

// DiscussionScope authorizes evidence for exactly one consented task.
const DiscussionScope = "task:discussion"

// ScopeInfo names one scope the connector can issue and says in one plain line
// what it allows. The consent page shows the description next to the scope.
type ScopeInfo struct{ Name, Description string }

// KnownScopes lists every scope the connector can issue, in display order. A
// client may request any non-empty subset; the owner ticks which to grant.
var KnownScopes = []ScopeInfo{
	{Name: DiscussionScope, Description: "Read recorded status, results, questions, research, Plan and approval evidence for the one selected task. Read-only; it cannot change work or grant workflow approval."},
	{Name: Scope, Description: "Read the factory overview: health, current activity, open owner questions and reviews, and last week's publications. Read-only; it cannot change factory work."},
}

func knownScope(name string) bool {
	return slices.ContainsFunc(KnownScopes, func(k ScopeInfo) bool { return k.Name == name })
}

func scopeNames() []string {
	names := make([]string, len(KnownScopes))
	for i, k := range KnownScopes {
		names[i] = k.Name
	}
	return names
}

// normalizeScope validates an RFC 6749 scope string against KnownScopes and
// returns it in catalogue order. Empty, repeated or unknown scopes are refused.
func normalizeScope(raw string) (string, bool) {
	tokens, ok := store.ConnectorScopes(raw)
	if !ok {
		return "", false
	}
	for _, token := range tokens {
		if !knownScope(token) {
			return "", false
		}
	}
	var out []string
	for _, k := range KnownScopes {
		if slices.Contains(tokens, k.Name) {
			out = append(out, k.Name)
		}
	}
	return strings.Join(out, " "), true
}

// Options supplies dependencies resolved by trusted daemon composition.
type Options struct {
	Config       config.RemoteConnector
	DB           *store.DB
	Logger       *slog.Logger
	Now          func() time.Time
	LocalControl *LocalControl
	Overview     OverviewProvider
	TaskEvidence TaskEvidenceProvider
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
	taskEvidence TaskEvidenceProvider
	guidance     fs.FS
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
	return &Server{cfg: opts.Config, db: opts.DB, logger: opts.Logger.With("component", "connector"), now: now, localControl: opts.LocalControl, overview: opts.Overview, taskEvidence: opts.TaskEvidence, guidance: guidanceFiles, github: github, cookie: aead, transport: newTransportState()}, nil
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
