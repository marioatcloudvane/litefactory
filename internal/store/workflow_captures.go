package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// WorkflowProvenance separates installed identity from a graph's declared team.
// A zero value denotes historical capture metadata, never an installed release.
type WorkflowProvenance struct {
	Kind, Source, Slug, Version, ReleaseDigest string
	GraphPath, GraphFile, Team                 string
	TeamConfig                                 []byte
	ConfigDigest                               string
}

// WorkflowCapture is one immutable admitted snapshot, addressed by launch ID.
type WorkflowCapture struct {
	ID, SessionID, TriggerID, InstanceID int64
	ContentDigest, TopologyDigest        string
	Files                                []byte
	Provenance                           WorkflowProvenance
}

func captureBytesDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidateWorkflowProvenance checks captured identity without writing an admission.
func ValidateWorkflowProvenance(p WorkflowProvenance) error {
	return validateWorkflowProvenance(p)
}

func validateWorkflowProvenance(p WorkflowProvenance) error {
	if p.Kind == "" {
		if p.Source != "" || p.Slug != "" || p.Version != "" || p.ReleaseDigest != "" || p.GraphPath != "" || p.GraphFile != "" || p.Team != "" || len(p.TeamConfig) != 0 || p.ConfigDigest != "" {
			return graphInvariant("partial historical workflow provenance")
		}
		return nil
	}
	if !validGraphPath(p.GraphPath) || !validGraphPath(p.GraphFile) || p.Team == "" {
		return graphInvariant("invalid captured workflow path or team")
	}
	if p.Kind == "builtin" {
		if p.Source != "" || p.Slug != "" || p.Version != "" || p.ReleaseDigest != "" || len(p.TeamConfig) != 0 || p.ConfigDigest != "" {
			return graphInvariant("invalid built-in workflow provenance")
		}
		return nil
	}
	if err := validateRepositoryTeamBinding(RepositoryTeamBinding{RepoID: 1, Source: p.Source, Slug: p.Slug, Version: p.Version, Kind: p.Kind, ReleaseDigest: p.ReleaseDigest}); err != nil {
		return err
	}
	if len(p.TeamConfig) == 0 || len(p.TeamConfig) > 1<<20 || p.ConfigDigest != captureBytesDigest(p.TeamConfig) {
		return graphInvariant("invalid captured team.config bytes or digest")
	}
	return nil
}

func serializedWorkflowProvenance(p WorkflowProvenance) ([]byte, error) {
	if err := validateWorkflowProvenance(p); err != nil {
		return nil, err
	}
	if p.Kind == "" {
		return nil, nil
	}
	return json.Marshal(p)
}

// GetWorkflowCapture loads an exact retained launch, with bounded reads and integrity checks.
func (db *DB) GetWorkflowCapture(ctx context.Context, id int64) (WorkflowCapture, error) {
	return scanWorkflowCapture(db.sql.QueryRowContext(ctx, `SELECT id,session_id,trigger_id,instance_id,content_digest,topology_digest,files,provenance,files_digest FROM research_launch_definitions WHERE id=? AND length(files)<=16777216 AND (provenance IS NULL OR length(provenance)<=2097152)`, id))
}

func scanWorkflowCapture(row scanner) (WorkflowCapture, error) {
	var c WorkflowCapture
	var metadata []byte
	var fileDigest sql.NullString
	if err := row.Scan(&c.ID, &c.SessionID, &c.TriggerID, &c.InstanceID, &c.ContentDigest, &c.TopologyDigest, &c.Files, &metadata, &fileDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	if len(c.Files) == 0 || (fileDigest.Valid && fileDigest.String != captureBytesDigest(c.Files)) {
		return WorkflowCapture{}, graphInvariant("retained workflow files digest mismatch")
	}
	if len(metadata) > 0 {
		if !json.Valid(metadata) {
			return WorkflowCapture{}, graphInvariant("invalid retained workflow provenance JSON")
		}
		decoder := json.NewDecoder(bytes.NewReader(metadata))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&c.Provenance); err != nil {
			return WorkflowCapture{}, fmt.Errorf("retained workflow provenance: %w", err)
		}
		if err := validateWorkflowProvenance(c.Provenance); err != nil {
			return WorkflowCapture{}, err
		}
	}
	return c, nil
}

// WorkflowCaptureForAdmission recognizes replay, exact dispatch and admitted running
// work before consulting current repository selection. ErrNotFound means fresh work.
func (db *DB) WorkflowCaptureForAdmission(ctx context.Context, sessionID, triggerID int64, mode string) (WorkflowCapture, error) {
	if mode == "bug" {
		mode = standaloneGraphStorageKey
	}
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT id FROM research_launch_definitions WHERE session_id=? AND trigger_id=?`, sessionID, triggerID).Scan(&id)
	if err == nil {
		return db.requiredWorkflowCapture(ctx, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return WorkflowCapture{}, err
	}
	if dispatchID, ok := GraphTriggerDispatch(triggerID); ok {
		var launch sql.NullInt64
		err = db.sql.QueryRowContext(ctx, `SELECT launch_id FROM graph_dispatches WHERE id=? AND session_id=?`, dispatchID, sessionID).Scan(&launch)
		if err != nil || !launch.Valid {
			return WorkflowCapture{}, fmt.Errorf("retained graph dispatch has no exact capture reference: %w", errors.Join(err, ErrGraphInvariant))
		}
		return db.requiredWorkflowCapture(ctx, launch.Int64)
	}
	instance, err := db.LatestWorkflowInstance(ctx, sessionID, mode)
	if err != nil {
		return WorkflowCapture{}, err
	}
	if instance.Status != GraphRunning && mode != standaloneGraphStorageKey {
		return WorkflowCapture{}, ErrNotFound
	}
	// Replies bind to the attempt that asked the question, even if this instance
	// has several historical launches.
	var launch sql.NullInt64
	err = db.sql.QueryRowContext(ctx, `SELECT a.launch_id FROM graph_owner_waits w JOIN graph_node_attempts a ON a.id=w.source_attempt_id WHERE w.instance_id=? AND w.state IN ('waiting','claimed','allocated') ORDER BY w.rowid DESC LIMIT 1`, instance.ID).Scan(&launch)
	if err == nil && launch.Valid {
		return db.requiredWorkflowCapture(ctx, launch.Int64)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorkflowCapture{}, err
	}
	err = db.sql.QueryRowContext(ctx, `SELECT launch_id FROM graph_node_attempts WHERE instance_id=? AND status IN ('pending','running') ORDER BY id DESC LIMIT 1`, instance.ID).Scan(&launch)
	if err == nil && launch.Valid {
		return db.requiredWorkflowCapture(ctx, launch.Int64)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorkflowCapture{}, err
	}
	// Historical evidence is usable only when one launch can be attributed to
	// this saved instance. Missing or ambiguous evidence must not select anew.
	var count int
	err = db.sql.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MIN(id),0) FROM research_launch_definitions WHERE instance_id=?`, instance.ID).Scan(&count, &id)
	if err != nil {
		return WorkflowCapture{}, err
	}
	if count != 1 {
		return WorkflowCapture{}, graphInvariant(fmt.Sprintf("saved graph requires a unique retained capture; found %d", count))
	}
	return db.requiredWorkflowCapture(ctx, id)
}

// WorkflowCaptureForAttempt resolves historical run instructions without a latest lookup.
func (db *DB) WorkflowCaptureForAttempt(ctx context.Context, attemptID int64) (WorkflowCapture, error) {
	var id sql.NullInt64
	if err := db.sql.QueryRowContext(ctx, `SELECT launch_id FROM graph_node_attempts WHERE id=?`, attemptID).Scan(&id); err != nil {
		return WorkflowCapture{}, err
	}
	if !id.Valid {
		return WorkflowCapture{}, graphInvariant("attempt has no exact retained capture")
	}
	return db.requiredWorkflowCapture(ctx, id.Int64)
}

func (db *DB) requiredWorkflowCapture(ctx context.Context, id int64) (WorkflowCapture, error) {
	capture, err := db.GetWorkflowCapture(ctx, id)
	if err != nil {
		return WorkflowCapture{}, fmt.Errorf("retained capture %d missing or invalid: %w", id, errors.Join(ErrGraphInvariant, err))
	}
	return capture, nil
}
