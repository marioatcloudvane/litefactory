// Package connectorstorage binds narrow connector ports to the existing atomic persistence implementation.
package connectorstorage

import (
	"context"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// Ports contains only connector persistence and scoped access operations.
type Ports interface {
	connectorstate.Storage
	connectorstate.Access
	connectorstate.LocalGrants
}

type persistence struct{ db *store.DB }

// New composes connector-owned persistence without exposing the general store.
func New(db *store.DB) Ports {
	if db == nil {
		return nil
	}
	return &persistence{db: db}
}

func (p *persistence) SameStore(other connectorstate.Storage) bool {
	peer, ok := other.(*persistence)
	return ok && p != nil && peer != nil && p.db == peer.db
}

func (p *persistence) ReconcileConnectorOwner(ctx context.Context, pin int64, now time.Time) (connectorstate.ConnectorOwner, error) {
	return p.db.ReconcileConnectorOwner(ctx, pin, now)
}

func (p *persistence) ConnectorOwner(ctx context.Context, pin int64) (connectorstate.ConnectorOwner, error) {
	return p.db.ConnectorOwner(ctx, pin)
}

func (p *persistence) RegisterConnectorClient(ctx context.Context, client connectorstate.ConnectorClient, now time.Time) error {
	return p.db.RegisterConnectorClient(ctx, client, now)
}

func (p *persistence) GetConnectorClient(ctx context.Context, id string, now time.Time) (connectorstate.ConnectorClient, error) {
	return p.db.GetConnectorClient(ctx, id, now)
}

func (p *persistence) CreateConnectorBrowser(ctx context.Context, browser connectorstate.ConnectorBrowser, pin int64, now time.Time) error {
	return p.db.CreateConnectorBrowser(ctx, browser, pin, now)
}

func (p *persistence) GetConnectorBrowser(ctx context.Context, id string, pin int64, now time.Time) (connectorstate.ConnectorBrowser, error) {
	return p.db.GetConnectorBrowser(ctx, id, pin, now)
}

func (p *persistence) VerifyConnectorBrowser(ctx context.Context, id, nonceDigest, upstreamDigest string, pin int64, now time.Time) error {
	return p.db.VerifyConnectorBrowser(ctx, id, nonceDigest, upstreamDigest, pin, now)
}

func (p *persistence) AbandonConnectorBrowser(ctx context.Context, id, nonceDigest string, pin int64, now time.Time) error {
	return p.db.AbandonConnectorBrowser(ctx, id, nonceDigest, pin, now)
}

func (p *persistence) ConsentConnectorBrowser(ctx context.Context, id, nonceDigest string, pin int64, now time.Time, grant connectorstate.ConnectorGrant, code connectorstate.ConnectorCode, allow bool) error {
	return p.db.ConsentConnectorBrowser(ctx, id, nonceDigest, pin, now, grant, code, allow)
}

func (p *persistence) ExchangeConnectorCode(ctx context.Context, exchange connectorstate.ConnectorExchange) (connectorstate.ConnectorGrant, error) {
	return p.db.ExchangeConnectorCode(ctx, exchange)
}

func (p *persistence) RotateConnectorRefresh(ctx context.Context, exchange connectorstate.ConnectorExchange) (connectorstate.ConnectorGrant, error) {
	return p.db.RotateConnectorRefresh(ctx, exchange)
}

func (p *persistence) CheckConnectorAccess(ctx context.Context, digest string, pin int64, resource, scope string, now time.Time) (connectorstate.ConnectorGrant, error) {
	return p.db.CheckConnectorAccess(ctx, digest, pin, resource, scope, now)
}

func (p *persistence) RevokeConnectorToken(ctx context.Context, clientID, digest string, now time.Time) error {
	return p.db.RevokeConnectorToken(ctx, clientID, digest, now)
}

func (p *persistence) ListConnectorRepositories(ctx context.Context) ([]connectorstate.ConnectorRepository, error) {
	return p.db.ListConnectorRepositories(ctx)
}

func (p *persistence) CheckConnectorDiscussionAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (connectorstate.ConnectorGrant, connectorstate.ConnectorGrantTarget, error) {
	return p.db.CheckConnectorDiscussionAccess(ctx, digest, pin, resource, taskRef, now)
}

func (p *persistence) CheckConnectorCodeAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (connectorstate.ConnectorGrant, connectorstate.ConnectorCodeTarget, error) {
	return p.db.CheckConnectorCodeAccess(ctx, digest, pin, resource, taskRef, now)
}

func (p *persistence) CheckConnectorUsageAccess(ctx context.Context, digest string, pin int64, resource, scope, repositoryRef, taskRef string, now time.Time) (connectorstate.ConnectorGrant, connectorstate.ConnectorUsageTarget, error) {
	return p.db.CheckConnectorUsageAccess(ctx, digest, pin, resource, scope, repositoryRef, taskRef, now)
}

func (p *persistence) CheckLocalConnectorGrantControl(ctx context.Context) error {
	return p.db.CheckLocalConnectorGrantControl(ctx)
}

func (p *persistence) ListLocalConnectorGrants(ctx context.Context, now time.Time) ([]connectorstate.ConnectorLocalGrant, error) {
	return p.db.ListLocalConnectorGrants(ctx, now)
}

func (p *persistence) RevokeLocalConnectorGrants(ctx context.Context, id string, all bool, now time.Time) (int64, error) {
	return p.db.RevokeLocalConnectorGrants(ctx, id, all, now)
}
