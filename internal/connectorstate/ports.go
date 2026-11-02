package connectorstate

import (
	"context"
	"time"
)

// Storage owns credential, browser and consent transitions. Compound consent and token changes are indivisible operations.
type Storage interface {
	// SameStore compares backend ownership without exposing its database.
	SameStore(Storage) bool
	ReconcileConnectorOwner(ctx context.Context, pin int64, now time.Time) (ConnectorOwner, error)
	ConnectorOwner(ctx context.Context, pin int64) (ConnectorOwner, error)
	RegisterConnectorClient(ctx context.Context, client ConnectorClient, now time.Time) error
	GetConnectorClient(ctx context.Context, id string, now time.Time) (ConnectorClient, error)
	CreateConnectorBrowser(ctx context.Context, browser ConnectorBrowser, pin int64, now time.Time) error
	GetConnectorBrowser(ctx context.Context, id string, pin int64, now time.Time) (ConnectorBrowser, error)
	VerifyConnectorBrowser(ctx context.Context, id, nonceDigest, upstreamDigest string, pin int64, now time.Time) error
	AbandonConnectorBrowser(ctx context.Context, id, nonceDigest string, pin int64, now time.Time) error
	ConsentConnectorBrowser(ctx context.Context, id, nonceDigest string, pin int64, now time.Time, grant ConnectorGrant, code ConnectorCode, allow bool) error
	ExchangeConnectorCode(ctx context.Context, exchange ConnectorExchange) (ConnectorGrant, error)
	RotateConnectorRefresh(ctx context.Context, exchange ConnectorExchange) (ConnectorGrant, error)
	CheckConnectorAccess(ctx context.Context, digest string, pin int64, resource, scope string, now time.Time) (ConnectorGrant, error)
	RevokeConnectorToken(ctx context.Context, clientID, digest string, now time.Time) error
}

// Access resolves current grant coverage and retained factory targets. Implementations keep existing authority checks and their transaction fences.
type Access interface {
	// SameStore preserves the authentication and target-resolution backend binding.
	SameStore(Storage) bool
	ListConnectorRepositories(ctx context.Context) ([]ConnectorRepository, error)
	CheckConnectorDiscussionAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (ConnectorGrant, ConnectorGrantTarget, error)
	CheckConnectorCodeAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (ConnectorGrant, ConnectorCodeTarget, error)
	CheckConnectorUsageAccess(ctx context.Context, digest string, pin int64, resource, scope, repositoryRef, taskRef string, now time.Time) (ConnectorGrant, ConnectorUsageTarget, error)
}

// LocalGrants owns credential-free local grant administration while the daemon is stopped.
type LocalGrants interface {
	CheckLocalConnectorGrantControl(ctx context.Context) error
	ListLocalConnectorGrants(ctx context.Context, now time.Time) ([]ConnectorLocalGrant, error)
	RevokeLocalConnectorGrants(ctx context.Context, id string, all bool, now time.Time) (int64, error)
}
