package connector

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

// LifecycleOptions supplies the connector's dependencies without exposing its listener.
type LifecycleOptions struct {
	Options       Options
	ResolveSecret func(context.Context, config.ConnectorSecretRef) (string, error)
	GrantControl  GrantControl
	Launchd       bool
}

// Runtime owns one prepared remote listener and its bounded services.
type Runtime struct {
	server    *Server
	listener  net.Listener
	closeOnce sync.Once
	closeErr  error
}

// Prepare reconciles owner authority before any fallible readiness or secret work.
// Disabled remote access needs none of the connector's dependencies.
func Prepare(ctx context.Context, options LifecycleOptions) (*Runtime, error) {
	opts := options.Options
	if !opts.Config.Enabled {
		return nil, nil
	}
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	if opts.Storage == nil {
		return nil, errors.New("connector: required dependency unavailable")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if _, err := opts.Storage.ReconcileConnectorOwner(ctx, opts.Config.OwnerID, now()); err != nil {
		return nil, err
	}
	if opts.LocalControl == nil && options.GrantControl != nil {
		control, err := NewLocalControl(ctx, opts.Storage, options.GrantControl)
		if err != nil {
			return nil, err
		}
		opts.LocalControl = control
	}
	if opts.LocalControl == nil {
		return nil, errors.New("remote connector requires the factory local grant-control host")
	}
	if err := opts.LocalControl.check(ctx, opts.Storage, opts.Config.OwnerID); err != nil {
		return nil, err
	}
	if options.ResolveSecret == nil {
		return nil, errors.New("connector: secret resolver unavailable")
	}
	resolve := func(ref config.ConnectorSecretRef) (string, error) {
		if options.Launchd && (ref.Keychain.Service == "" || ref.Keychain.Account == "") {
			return "", errors.New("remote connector requires keychain references under launchd")
		}
		return options.ResolveSecret(ctx, ref)
	}
	githubSecret, githubErr := resolve(opts.Config.GitHubSecretRef)
	cookieSecret, cookieErr := resolve(opts.Config.CookieSecretRef)
	if err := errors.Join(githubErr, cookieErr); err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(cookieSecret)
	if err != nil || len(key) != 32 {
		return nil, errors.New("remote connector cookie key must encode exactly 32 random bytes as base64")
	}
	opts.GitHubSecret, opts.CookieKey = githubSecret, key
	server, err := newServer(ctx, opts, true)
	if err != nil {
		return nil, err
	}
	listener, err := server.Listen(ctx)
	if err != nil {
		return nil, err
	}
	return &Runtime{server: server, listener: listener}, nil
}

// Addr reports the prepared listener address without transferring ownership.
func (r *Runtime) Addr() net.Addr { return r.listener.Addr() }

// Serve drains the prepared listener when the shared process context ends.
func (r *Runtime) Serve(ctx context.Context) error { return r.server.Serve(ctx, r.listener) }

// Close releases the prepared listener, including before serving starts.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.closeErr = r.listener.Close()
		if errors.Is(r.closeErr, net.ErrClosed) {
			r.closeErr = nil
		}
	})
	return r.closeErr
}

// SetActions attaches the shared core action service.
func (r *Runtime) SetActions(v OwnerActions) { r.server.SetActions(v) }

// SetWorkflows attaches the shared core workflow service.
func (r *Runtime) SetWorkflows(v Workflows) { r.server.SetWorkflows(v) }

// SetTaskDiscussion attaches the core discussion service.
func (r *Runtime) SetTaskDiscussion(v TaskDiscussion) { r.server.SetTaskDiscussion(v) }

// SetStageDecisions attaches the core stage decision service.
func (r *Runtime) SetStageDecisions(v StageDecisions) { r.server.SetStageDecisions(v) }
