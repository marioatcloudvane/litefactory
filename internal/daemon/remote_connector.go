package daemon

import (
	"context"
	"sync"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/secrets"
)

type remoteConnectorServices interface {
	SetActions(connector.OwnerActions)
	SetWorkflows(connector.Workflows)
	SetTaskDiscussion(connector.TaskDiscussion)
	SetBrainstorm(connector.Brainstorms)
}

type remoteConnectorRuntime struct {
	config    config.RemoteConnector
	server    remoteConnectorServices
	lifecycle *connector.Runtime
	actions   *ownerActionRuntime
}

func prepareRemoteConnector(ctx context.Context, s Setup) (*remoteConnectorRuntime, error) {
	return prepareRemoteConnectorWithControl(ctx, s, nil)
}

func prepareRemoteConnectorWithControl(ctx context.Context, s Setup, control connector.GrantControl) (*remoteConnectorRuntime, error) {
	runtime, err := connector.Prepare(ctx, connector.LifecycleOptions{
		Options:      connector.Options{Config: s.Config.RemoteConnector, Storage: connectorstorage.New(s.DB), Access: connectorstorage.New(s.DB), Logger: s.Logger, LocalControl: s.ConnectorControl, Overview: s.ConnectorOverview, TaskEvidence: s.ConnectorTaskEvidence, Usage: s.ConnectorUsage, CodeEvidence: s.ConnectorCodeEvidence},
		GrantControl: control,
		Launchd:      s.Launchd,
		ResolveSecret: func(ctx context.Context, ref config.ConnectorSecretRef) (string, error) {
			source, _ := secrets.ReferenceFrom(ref.Keychain.Service, ref.Keychain.Account, ref.Env)
			token, err := s.ConnectionSecrets.Resolve(ctx, source)
			if err != nil {
				return "", err
			}
			return token.Reveal(), nil
		},
	})
	if err != nil || runtime == nil {
		return nil, err
	}
	return &remoteConnectorRuntime{config: s.Config.RemoteConnector, server: runtime, lifecycle: runtime}, nil
}

func (d *Daemon) serveRemoteConnector(ctx context.Context, serving *sync.WaitGroup) {
	if d.remoteConnector == nil {
		return
	}
	serving.Add(1)
	// Exactly one serving goroutine ends on shared-context cancellation; Run
	// joins it after the connector drains and before the shared database closes.
	go func() {
		defer serving.Done()
		if err := d.remoteConnector.lifecycle.Serve(ctx); err != nil {
			d.log.ErrorContext(ctx, "remote connector stopped", "component", "connector", "reason", "listener")
		}
	}()
}

func (d *Daemon) closeRemoteConnector() error {
	if d.remoteConnector == nil {
		return nil
	}
	return d.remoteConnector.lifecycle.Close()
}
