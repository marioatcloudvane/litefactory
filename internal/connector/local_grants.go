package connector

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// ErrGrantNotFound identifies an exact ID absent from retained grant state.
var ErrGrantNotFound = store.ErrConnectorGrantNotFound

// GrantCoverage describes the scope-specific reach of a connector grant.
type GrantCoverage struct {
	AllRegisteredRepositories bool   `json:"all_registered_repositories"`
	PrivateRepositories       bool   `json:"private_repositories"`
	FutureRepositories        bool   `json:"future_repositories"`
	TaskRef                   string `json:"task_ref,omitempty"`
}

// GrantAccessTokens separates issued token lifetime from renewable grant lifetime.
type GrantAccessTokens struct {
	Count          int        `json:"count"`
	UsableCount    int        `json:"usable_count"`
	EarliestExpiry *time.Time `json:"earliest_expiry"`
	LatestExpiry   *time.Time `json:"latest_expiry"`
	Usable         bool       `json:"usable"`
}

// GrantSummary is an allowlist: no credential or credential digest can be rendered.
type GrantSummary struct {
	ID               string            `json:"grant_id"`
	ClientID         string            `json:"client_id"`
	ClientName       string            `json:"client_name"`
	Scope            string            `json:"scope"`
	Resource         string            `json:"resource"`
	Coverage         GrantCoverage     `json:"coverage"`
	CreatedAt        time.Time         `json:"created_at"`
	ExpiresAt        time.Time         `json:"grant_expires_at"`
	Renewable        bool              `json:"renewable"`
	Status           string            `json:"status"`
	RevokedAt        *time.Time        `json:"revoked_at"`
	RevocationReason string            `json:"revocation_reason,omitempty"`
	AccessTokens     GrantAccessTokens `json:"access_tokens"`
}

// GrantMutation records a confirmed durable operation, including successful no-ops.
type GrantMutation struct {
	GrantID      string    `json:"grant_id,omitempty"`
	Outcome      string    `json:"outcome"`
	ChangedCount int64     `json:"changed_count"`
	CommittedAt  time.Time `json:"committed_at"`
}

// GrantService is the shared local filesystem-owner control implementation.
// It requires neither remote configuration nor an online daemon.
type GrantService struct{ db *store.DB }

// NewGrantService binds local administration to an existing durable store.
func NewGrantService(db *store.DB) *GrantService { return &GrantService{db: db} }

// Ready verifies that the bound store can read retained grant state.
func (s *GrantService) Ready(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("connector grant store unavailable")
	}
	return s.db.CheckLocalConnectorGrantControl(ctx)
}

// ListGrantSummaries returns safe grant metadata in stable creation order.
func (s *GrantService) ListGrantSummaries(ctx context.Context, now time.Time) ([]GrantSummary, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("connector grant store unavailable")
	}
	rows, err := s.db.ListLocalConnectorGrants(ctx, now.UTC())
	if err != nil {
		return nil, err
	}
	out := make([]GrantSummary, 0, len(rows))
	for _, g := range rows {
		out = append(out, GrantSummary{ID: g.ID, ClientID: g.ClientID, ClientName: safeGrantLabel(g.ClientName), Scope: g.Scope, Resource: g.Resource, Coverage: GrantCoverage{AllRegisteredRepositories: store.ConnectorScopeHas(g.Scope, Scope), PrivateRepositories: store.ConnectorScopeHas(g.Scope, Scope), FutureRepositories: store.ConnectorScopeHas(g.Scope, Scope), TaskRef: g.TaskRef}, CreatedAt: g.CreatedAt.UTC(), ExpiresAt: g.ExpiresAt.UTC(), Renewable: g.Status == "active", Status: g.Status, RevokedAt: grantTime(g.RevokedAt), RevocationReason: safeGrantLabel(g.RevocationReason), AccessTokens: GrantAccessTokens{Count: g.AccessTokenCount, UsableCount: usableTokenCount(g), EarliestExpiry: grantTime(g.EarliestAccessExpiry), LatestExpiry: grantTime(g.LatestAccessExpiry), Usable: usableTokenCount(g) > 0}})
	}
	return out, nil
}

// InspectGrant selects a retained grant by its exact stable ID.
func (s *GrantService) InspectGrant(ctx context.Context, id string, now time.Time) (GrantSummary, error) {
	rows, err := s.ListGrantSummaries(ctx, now)
	if err != nil {
		return GrantSummary{}, err
	}
	for _, g := range rows {
		if g.ID == id {
			return g, nil
		}
	}
	return GrantSummary{}, ErrGrantNotFound
}

// RevokeGrant durably disables one retained grant and all its token families.
func (s *GrantService) RevokeGrant(ctx context.Context, id string, now time.Time) (GrantMutation, error) {
	return s.revoke(ctx, id, false, now)
}

// RevokeAllGrants atomically disables existing grants across all generations.
func (s *GrantService) RevokeAllGrants(ctx context.Context, now time.Time) (GrantMutation, error) {
	return s.revoke(ctx, "", true, now)
}
func (s *GrantService) revoke(ctx context.Context, id string, all bool, now time.Time) (GrantMutation, error) {
	if s == nil || s.db == nil {
		return GrantMutation{}, errors.New("connector grant store unavailable")
	}
	n, err := s.db.RevokeLocalConnectorGrants(ctx, id, all, now.UTC())
	if err != nil {
		return GrantMutation{}, err
	}
	outcome := "revoked"
	if n == 0 {
		outcome = "already_revoked"
	}
	return GrantMutation{GrantID: id, Outcome: outcome, ChangedCount: n, CommittedAt: time.Now().UTC()}, nil
}

// List adapts safe summaries to the trusted daemon readiness surface.
func (s *GrantService) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	rows, err := s.ListGrantSummaries(ctx, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	out := make([]store.ConnectorGrantSummary, 0, len(rows))
	for _, g := range rows {
		r := store.ConnectorGrantSummary{ID: g.ID, ClientID: g.ClientID, ClientName: g.ClientName, Resource: g.Resource, Scope: g.Scope, CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt}
		if g.RevokedAt != nil {
			r.RevokedAt = *g.RevokedAt
		}
		out = append(out, r)
	}
	return out, nil
}

// Revoke adapts exact local revocation to the trusted readiness surface.
func (s *GrantService) Revoke(ctx context.Context, id string) error {
	_, err := s.RevokeGrant(ctx, id, time.Now().UTC())
	return err
}

// RevokeAll adapts atomic local revocation to the trusted readiness surface.
func (s *GrantService) RevokeAll(ctx context.Context) error {
	_, err := s.RevokeAllGrants(ctx, time.Now().UTC())
	return err
}
func grantTime(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	v = v.UTC()
	return &v
}
func safeGrantLabel(v string) string {
	const maxRunes = 200
	r := []rune(v)
	if len(r) > maxRunes {
		v = string(r[:maxRunes]) + "…"
	}
	quoted := strconv.QuoteToASCII(v)
	return strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
}

func usableTokenCount(g store.ConnectorLocalGrant) int {
	if g.Status != "active" {
		return 0
	}
	return g.UsableAccessTokenCount
}
