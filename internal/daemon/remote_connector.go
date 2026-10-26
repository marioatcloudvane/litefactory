package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/secrets"
)

type remoteConnectorRuntime struct {
	server   *connector.Server
	listener net.Listener
}

func prepareRemoteConnector(ctx context.Context, s Setup) (*remoteConnectorRuntime, error) {
	cfg := s.Config.RemoteConnector
	if !cfg.Enabled {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// Reconcile even when the sibling command host is not installed: a changed
	// configured pin must never revive an earlier grant when changed back.
	if _, err := s.DB.ReconcileConnectorOwner(ctx, cfg.OwnerID, time.Now()); err != nil {
		return nil, err
	}
	if s.ConnectorControl == nil {
		return nil, errors.New("remote connector requires the factory local grant-control host")
	}
	resolve := func(ref config.ConnectorSecretRef) (secrets.Token, error) {
		source, _ := secrets.ReferenceFrom(ref.Keychain.Service, ref.Keychain.Account, ref.Env)
		if s.Launchd && source.Source != secrets.SourceKeychain {
			return secrets.Token{}, errors.New("remote connector requires keychain references under launchd")
		}
		return s.ConnectionSecrets.Resolve(ctx, source)
	}
	githubSecret, githubErr := resolve(cfg.GitHubSecretRef)
	cookieSecret, cookieErr := resolve(cfg.CookieSecretRef)
	if err := errors.Join(githubErr, cookieErr); err != nil {
		return nil, err
	}
	// These dedicated secrets are revealed only to the connector's identity and
	// cookie encryption boundaries, never to a factory worker or a log attribute.
	key, err := base64.StdEncoding.DecodeString(cookieSecret.Reveal())
	if err != nil || len(key) != 32 {
		return nil, errors.New("remote connector cookie key must encode exactly 32 random bytes as base64")
	}
	server, err := connector.New(ctx, connector.Options{Config: cfg, DB: s.DB, Logger: s.Logger, LocalControl: s.ConnectorControl, Overview: s.ConnectorOverview, GitHubSecret: githubSecret.Reveal(), CookieKey: key})
	if err != nil {
		return nil, err
	}
	listener, err := server.Listen(ctx)
	if err != nil {
		return nil, err
	}
	return &remoteConnectorRuntime{server: server, listener: listener}, nil
}

func (d *Daemon) serveRemoteConnector(ctx context.Context, serving *sync.WaitGroup) {
	if d.remoteConnector == nil {
		return
	}
	serving.Add(1)
	// One serve goroutine is joined by Run; its listener and bounded handlers are
	// canceled and drained when the shared serving context ends.
	go func() {
		defer serving.Done()
		if err := d.remoteConnector.server.Serve(ctx, d.remoteConnector.listener); err != nil {
			d.log.ErrorContext(ctx, "remote connector stopped", "component", "connector", "reason", "listener")
		}
	}()
}

func (d *Daemon) closeRemoteConnector() error {
	if d.remoteConnector == nil {
		return nil
	}
	err := d.remoteConnector.listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
