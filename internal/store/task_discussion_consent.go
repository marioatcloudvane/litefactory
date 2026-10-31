package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

type discussionConsentRepository struct {
	RepositoryID  int64  `json:"repository_id"`
	RepositoryRef string `json:"repository_ref"`
}
type discussionConsentEvidence struct {
	Scope           string                        `json:"scope"`
	RequiredScope   string                        `json:"required_scope"`
	OwnerID         int64                         `json:"owner_id"`
	OwnerGeneration int64                         `json:"owner_generation"`
	GrantID         string                        `json:"grant_id"`
	ClientID        string                        `json:"client_id"`
	Resource        string                        `json:"resource"`
	CreatedAt       string                        `json:"created_at"`
	ExpiresAt       string                        `json:"expires_at"`
	CapturedAt      string                        `json:"captured_at"`
	AllRepositories bool                          `json:"all_repositories"`
	Repositories    []discussionConsentRepository `json:"repositories"`
	CoverageDigest  string                        `json:"coverage_digest"`
}

// captureDiscussionConsentTx keeps immutable consent provenance after OAuth
// expiry cleanup removes its mutable grant. It never turns that history into
// authority: dispatch and reads still check the current live grant separately.
func captureDiscussionConsentTx(ctx context.Context, tx *sql.Tx, actor OwnerActionPrincipal, s DiscussionScope, grant, scope, raw string, at time.Time) (string, error) {
	if actor.Transport != "mcp" || actor.OwnerID != strconv.FormatInt(s.OwnerID, 10) || actor.OwnerGeneration != s.OwnerGeneration || actor.GrantID != grant {
		return "", ErrConnectorOwner
	}
	target, err := resolveConnectorDiscussionTarget(ctx, tx, s.TaskRef)
	if err != nil {
		return "", err
	}
	if target.RepositoryRef != s.RepositoryRef {
		return "", ErrConnectorScope
	}
	if err = ownerActionAuthority(ctx, tx, actor, OwnerAction{Principal: actor, RepositoryID: target.RepoID, RepositoryRef: target.RepositoryRef, Scope: scope}, at); err != nil {
		return "", err
	}
	var evidence discussionConsentEvidence
	evidence.RequiredScope = scope
	evidence.GrantID = grant
	evidence.CapturedAt = ownerActionTime(at)
	err = tx.QueryRowContext(ctx, `SELECT scope,owner_id,generation,client_id,resource,created_at,expires_at,all_repositories FROM connector_grants WHERE id=?`, grant).Scan(&evidence.Scope, &evidence.OwnerID, &evidence.OwnerGeneration, &evidence.ClientID, &evidence.Resource, &evidence.CreatedAt, &evidence.ExpiresAt, &evidence.AllRepositories)
	if err != nil {
		return "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT repo_id,repository_ref FROM connector_grant_repositories WHERE grant_id=? ORDER BY repo_id,repository_ref LIMIT 1001`, grant)
	if err != nil {
		return "", err
	}
	evidence.Repositories = []discussionConsentRepository{}
	for rows.Next() {
		var repository discussionConsentRepository
		if err = rows.Scan(&repository.RepositoryID, &repository.RepositoryRef); err != nil {
			_ = rows.Close()
			return "", err
		}
		if len(evidence.Repositories) >= 1000 || len(repository.RepositoryRef) > 1024 {
			_ = rows.Close()
			return "", ErrConnectorCapacity
		}
		evidence.Repositories = append(evidence.Repositories, repository)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return "", err
	}
	if err = rows.Close(); err != nil {
		return "", err
	}
	coverage, err := json.Marshal(struct {
		All          bool                          `json:"all_repositories"`
		Repositories []discussionConsentRepository `json:"repositories"`
	}{evidence.AllRepositories, evidence.Repositories})
	if err != nil {
		return "", err
	}
	evidence.CoverageDigest = submissionDigest(coverage)
	// Preserve the principal's flat contract: execution readers ignore the new
	// evidence property while retaining the authenticated fields they already use.
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &envelope) != nil || envelope == nil {
		envelope = map[string]json.RawMessage{}
	}
	principal, err := json.Marshal(actor)
	if err != nil {
		return "", err
	}
	var trusted map[string]json.RawMessage
	if err = json.Unmarshal(principal, &trusted); err != nil {
		return "", err
	}
	for key, value := range trusted {
		envelope[key] = value
	}
	envelope["_consent_evidence"], err = json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(envelope)
	return string(encoded), err
}
