package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

// PutEvidenceManifest retains immutable links before an owner presents a handoff
// as recoverable. It stores no source bytes and grants no read authority. Each
// consumer must still check its current scope against the owning source records.
func (db *DB) PutEvidenceManifest(ctx context.Context, manifest retainedevidence.Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	// Canonicalize empty slices without mutating the caller's backing arrays.
	manifest.Entries = append([]retainedevidence.Entry{}, manifest.Entries...)
	for i := range manifest.Entries {
		if len(manifest.Entries[i].Omissions) == 0 {
			manifest.Entries[i].Omissions = nil
		}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode evidence manifest: %w", err)
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	scope := manifest.Scope
	_, err = tx.ExecContext(ctx, `INSERT INTO retained_evidence_manifests
		(scope_kind,scope_id,repository,task,revision,manifest,created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(scope_kind,scope_id,repository,task,revision) DO NOTHING`,
		scope.Kind, scope.ID, scope.Repository, scope.Task, manifest.Revision, raw, FormatTime(nowUTC()))
	if err != nil {
		return fmt.Errorf("retain evidence manifest: %w", err)
	}
	var retained []byte
	err = tx.QueryRowContext(ctx, `SELECT manifest FROM retained_evidence_manifests
		WHERE scope_kind=? AND scope_id=? AND repository=? AND task=? AND revision=?`,
		scope.Kind, scope.ID, scope.Repository, scope.Task, manifest.Revision).Scan(&retained)
	if err != nil {
		return err
	}
	if !bytes.Equal(retained, raw) {
		return fmt.Errorf("manifest revision: %w", retainedevidence.ErrConflict)
	}
	var errs []error
	for i, entry := range manifest.Entries {
		if err := retainEvidenceSource(ctx, tx, scope, entry); err != nil {
			errs = append(errs, fmt.Errorf("evidence entry %d: %w", i, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return tx.Commit()
}

func retainEvidenceSource(ctx context.Context, tx *sql.Tx, scope retainedevidence.Scope, entry retainedevidence.Entry) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO retained_evidence_sources
		(scope_kind,scope_id,repository,task,source_kind,owner_id,source_revision,channel,digest,byte_extent)
		VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, scope.Kind, scope.ID, scope.Repository, scope.Task,
		entry.Kind, entry.OwnerID, entry.Revision, entry.Channel, entry.Digest, entry.Bytes)
	if err != nil {
		return err
	}
	var digest string
	var extent int64
	err = tx.QueryRowContext(ctx, `SELECT digest,byte_extent FROM retained_evidence_sources
		WHERE scope_kind=? AND scope_id=? AND repository=? AND task=? AND source_kind=? AND owner_id=? AND source_revision=? AND channel=?`,
		scope.Kind, scope.ID, scope.Repository, scope.Task, entry.Kind, entry.OwnerID, entry.Revision, entry.Channel).Scan(&digest, &extent)
	if err != nil {
		return err
	}
	if digest != entry.Digest || extent != entry.Bytes {
		return fmt.Errorf("source revision: %w", retainedevidence.ErrConflict)
	}
	return nil
}

// EvidenceManifest reads exactly the supplied revision, never the newest one.
// This internal metadata lookup is not a disclosure endpoint: the factory
// adapter must establish current owning-record grants before materialization.
func (db *DB) EvidenceManifest(ctx context.Context, scope retainedevidence.Scope, revision string) (retainedevidence.Manifest, error) {
	var raw []byte
	err := db.sql.QueryRowContext(ctx, `SELECT manifest FROM retained_evidence_manifests
		WHERE scope_kind=? AND scope_id=? AND repository=? AND task=? AND revision=?`,
		scope.Kind, scope.ID, scope.Repository, scope.Task, revision).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return retainedevidence.Manifest{}, ErrNotFound
	}
	if err != nil {
		return retainedevidence.Manifest{}, err
	}
	var manifest retainedevidence.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return retainedevidence.Manifest{}, fmt.Errorf("decode retained evidence: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return retainedevidence.Manifest{}, err
	}
	if manifest.Scope != scope || manifest.Revision != revision {
		return retainedevidence.Manifest{}, retainedevidence.ErrIntegrity
	}
	return manifest, nil
}
