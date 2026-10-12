// Package projectconnection builds isolated clients from immutable project versions.
package projectconnection

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

var (
	// ErrActorChanged rejects credentials that belong to another publisher.
	ErrActorChanged = errors.New("connection publishing actor differs from retained identity")
	// ErrCredentialPolicy enforces keychain-only background resolution.
	ErrCredentialPolicy = errors.New("background connections require a keychain credential")
	// ErrInvalidConnection rejects untrusted or unsupported version values.
	ErrInvalidConnection = errors.New("invalid project connection")
)

// DefaultTimeout bounds an entire credential and provider check.
const DefaultTimeout = 30 * time.Second

// Options configures external credential resolution and the existing transport.
type Options struct {
	DB       *store.DB
	Resolver secrets.Resolver
	// LegacyReference and LegacyToken reuse the already resolved startup token
	// only for matching imported legacy versions. Explicit settings always resolve.
	LegacyReference secrets.Reference
	LegacyToken     secrets.Token
	HTTPClient      *http.Client
	Logger          *slog.Logger
	UserAgent       string
	Redactor        *redact.Redactor
	ExpectedActorID int64
	Launchd         bool
	Timeout         time.Duration
}

// Clients contains separate protocol surfaces for one version and verified actor.
type Clients struct {
	Linear    *linear.Client
	Publisher ticket.Actor
	Code      *github.Client
	Tickets   TicketClient
	Actor     github.User
	Redactor  *redact.Redactor
}

type cacheKey struct{ version, actor int64 }

// Factory resolves once per loaded version. Refresh explicitly reloads a credential.
// No token is returned to a runner or stored in a project record.
type Factory struct {
	options        Options
	mu             sync.Mutex
	clients        map[cacheKey]*Clients
	loading        map[cacheKey]chan struct{}
	requestContext func(context.Context, store.ConnectionVersion) (context.Context, func(error), error)
}

// SetRequestContext installs daemon-owned admission and accounting for every
// request, including requests made by clients already in the cache. The callback
// receives the immutable version actually used by the client.
func (f *Factory) SetRequestContext(bind func(context.Context, store.ConnectionVersion) (context.Context, func(error), error)) {
	f.mu.Lock()
	f.requestContext = bind
	f.mu.Unlock()
}

func (f *Factory) bindRequest(v store.ConnectionVersion) func(context.Context) (context.Context, func(error), error) {
	return func(ctx context.Context) (context.Context, func(error), error) {
		f.mu.Lock()
		bind := f.requestContext
		f.mu.Unlock()
		if bind == nil {
			return ctx, nil, nil
		}
		return bind(ctx, v)
	}
}

// NewFactory creates a version-scoped client cache with shared redaction.
func NewFactory(options Options) *Factory {
	if options.Redactor == nil {
		options.Redactor = redact.New(redact.Options{Logger: options.Logger})
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	return &Factory{options: options, clients: make(map[cacheKey]*Clients), loading: make(map[cacheKey]chan struct{})}
}

func credential(v store.ConnectionVersion) secrets.Reference {
	return secrets.Reference{Source: secrets.Source(v.CredentialSource), Service: v.CredentialService, Account: v.CredentialAccount, EnvVar: v.CredentialEnv}
}

// Clients loads or reuses clients for an immutable version and verified actor.
func (f *Factory) Clients(ctx context.Context, v store.ConnectionVersion, expectedActorID int64) (*Clients, error) {
	return f.load(ctx, v, expectedActorID, false)
}

// Refresh retains the old cache if actor verification or resolution fails.
func (f *Factory) Refresh(ctx context.Context, v store.ConnectionVersion, expectedActorID int64) (*Clients, error) {
	if expectedActorID <= 0 {
		return nil, ErrActorChanged
	}
	return f.load(ctx, v, expectedActorID, true)
}

func (f *Factory) load(ctx context.Context, v store.ConnectionVersion, expectedActorID int64, refresh bool) (*Clients, error) {
	if v.ID > 0 && v.Provider == ticket.Linear {
		return f.loadLinear(ctx, v, expectedActorID, refresh)
	}
	if v.ID <= 0 || v.Provider != ticket.GitHub {
		return nil, ErrInvalidConnection
	}
	origin, err := ticket.CanonicalOrigin(string(v.Origin))
	if err != nil || origin != v.Origin {
		return nil, ErrInvalidConnection
	}
	if expectedActorID == 0 {
		expectedActorID = f.options.ExpectedActorID
	}
	ref := credential(v)
	if f.options.Launchd && ref.Source != secrets.SourceKeychain {
		return nil, ErrCredentialPolicy
	}
	key := cacheKey{v.ID, expectedActorID}
	ctx, cancel := context.WithTimeout(ctx, f.options.Timeout)
	defer cancel()
	for {
		f.mu.Lock()
		if !refresh {
			if clients := f.clients[key]; clients != nil {
				f.mu.Unlock()
				return clients, nil
			}
		}
		loading := f.loading[key]
		if loading == nil {
			f.loading[key] = make(chan struct{})
			f.mu.Unlock()
			break
		}
		f.mu.Unlock()
		select {
		case <-loading:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() {
		f.mu.Lock()
		close(f.loading[key])
		delete(f.loading, key)
		f.mu.Unlock()
	}()
	var token secrets.Token
	if v.Provenance == "legacy" && ref == f.options.LegacyReference && !f.options.LegacyToken.Empty() && !refresh {
		token = f.options.LegacyToken
	} else {
		token, err = f.options.Resolver.Resolve(ctx, ref)
		if err != nil {
			return nil, safeFailure(err)
		}
	}
	// Register before any authenticated response can contain the credential.
	f.options.Redactor.AddToken(token)
	code, err := github.New(github.Options{RequestContext: f.bindRequest(v), Redactor: f.options.Redactor, APIBase: string(v.Origin), Token: token, HTTPClient: f.options.HTTPClient, Logger: f.options.Logger, UserAgent: f.options.UserAgent})
	if err != nil {
		return nil, ErrInvalidConnection
	}
	actor, err := code.AuthenticatedUser(ctx)
	if err != nil {
		return nil, safeFailure(err)
	}
	if expectedActorID > 0 && actor.ID != expectedActorID {
		return nil, ErrActorChanged
	}
	tickets, err := github.NewTickets(code)
	if err != nil {
		return nil, ErrInvalidConnection
	}
	clients := &Clients{Code: code, Tickets: tickets, Actor: actor, Redactor: f.options.Redactor}
	f.mu.Lock()
	f.clients[key] = clients
	f.mu.Unlock()
	return clients, nil
}

// safeFailure keeps status classification while discarding provider response bodies,
// URLs, credential resolver diagnostics and nested raw errors.
func safeFailure(err error) error {
	for _, known := range []error{secrets.ErrNotConfigured, secrets.ErrNotFound, secrets.ErrEmpty, secrets.ErrIncomplete, secrets.ErrInteractionNotAllowed, github.ErrUnauthorized, github.ErrRateLimited, github.ErrForbidden, github.ErrNotFound, context.DeadlineExceeded, context.Canceled} {
		if errors.Is(err, known) {
			return known
		}
	}
	return errors.New("connection could not be checked; retry or inspect external access")
}
