package store

import (
	"context"
	"database/sql"
	"errors"
)

// Owner publication refusal reasons. They are stored, so a replayed approval
// reports the same outcome it reported the first time.
const (
	OwnerPublicationExpired  = "expired"
	OwnerPublicationStale    = "stale"
	OwnerPublicationQuota    = "quota"
	OwnerPublicationBound    = "bound"
	OwnerPublicationDisabled = "disabled"
)

// OwnerPublication is the factory's own publication of one approved proposal.
type OwnerPublication struct {
	IntentID     int64
	Repository   string
	ProposalHash string
	Artifacts    int
	State        string
	Reason       string
}

// OwnerPublicationArtifact is one artifact's known outcome. A row with no issue
// id was begun but never confirmed: the remote write may or may not exist.
type OwnerPublicationArtifact struct {
	Ordinal      int
	IssueID      int64
	IssueNumber  int
	Relationship string
}

// ClaimOwnerPublication elects the factory as the single publisher of an
// approval intent, or returns the publication an earlier call already elected.
// New claims recheck expiry, currency, a competing agent grant and quota in one
// transaction; a refusal is stored so every replay reports the same reason.
// enabled is the loaded creation gate and matters only for a new claim.
func (c *CreationStore) ClaimOwnerPublication(ctx context.Context, intentID int64, enabled bool) (OwnerPublication, CreationProposal, error) {
	var pub OwnerPublication
	var p CreationProposal
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		var task, hash string
		var expiry, approvedRevision int64
		if err := tx.QueryRowContext(ctx, `SELECT source_task,proposal_hash,expires_ns,proposal_revision FROM mcp_creation_intents WHERE id=?`, intentID).Scan(&task, &hash, &expiry, &approvedRevision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCreationAuthority
			}
			return err
		}
		var err error
		p, _, err = creationProposalTx(ctx, tx, hash)
		if err != nil {
			return err
		}
		pub, err = ownerPublicationTx(ctx, tx, intentID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := c.now()
		reason := ""
		var currentHash string
		var currentRevision, bound int64
		if err := tx.QueryRowContext(ctx, `SELECT proposal_hash,revision FROM mcp_creation_current WHERE source_task=?`, task).Scan(&currentHash, &currentRevision); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_creation_grants WHERE intent_id=?`, intentID).Scan(&bound); err != nil {
			return err
		}
		switch {
		case !enabled:
			reason = OwnerPublicationDisabled
		case now.UnixNano() >= expiry:
			reason = OwnerPublicationExpired
		case currentHash != hash || currentRevision != approvedRevision:
			reason = OwnerPublicationStale
		case bound > 0:
			reason = OwnerPublicationBound
		default:
			charged, err := chargedCreationUnits(ctx, tx, p.Repository, now.Add(-MCPCreationWindow).UnixNano())
			if err != nil {
				return err
			}
			if charged+int64(p.Artifacts) > MCPCreationRepositoryLimit {
				reason = OwnerPublicationQuota
			}
		}
		state := "dispatched"
		if reason != "" {
			state = "refused"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO creation_owner_publications(intent_id,repository,proposal_hash,artifacts,state,reason,created_ns) VALUES(?,?,?,?,?,?,?)`, intentID, p.Repository, hash, p.Artifacts, state, reason, now.UnixNano()); err != nil {
			return err
		}
		pub = OwnerPublication{IntentID: intentID, Repository: p.Repository, ProposalHash: hash, Artifacts: p.Artifacts, State: state, Reason: reason}
		return nil
	})
	return pub, p, err
}

// chargedCreationUnits counts artifacts against the repository's rolling quota
// across agent-bound operations and factory owner publications alike.
func chargedCreationUnits(ctx context.Context, tx *sql.Tx, repository string, since int64) (int64, error) {
	var agent, owner int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(artifacts),0) FROM mcp_creation_operations WHERE repository=? AND state<>'released' AND (state<>'confirmed' OR confirmed_ns>?)`, repository, since).Scan(&agent); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(artifacts),0) FROM creation_owner_publications WHERE repository=? AND state<>'refused' AND (state<>'confirmed' OR confirmed_ns>?)`, repository, since).Scan(&owner); err != nil {
		return 0, err
	}
	return agent + owner, nil
}

func ownerPublicationTx(ctx context.Context, tx *sql.Tx, intentID int64) (OwnerPublication, error) {
	pub := OwnerPublication{IntentID: intentID}
	err := tx.QueryRowContext(ctx, `SELECT repository,proposal_hash,artifacts,state,reason FROM creation_owner_publications WHERE intent_id=?`, intentID).Scan(&pub.Repository, &pub.ProposalHash, &pub.Artifacts, &pub.State, &pub.Reason)
	return pub, err
}

// BeginOwnerArtifact records intent before the remote write. It reports false
// when the artifact was already begun, so a resumed publication never repeats
// a write whose outcome it does not know.
func (db *DB) BeginOwnerArtifact(ctx context.Context, intentID int64, ordinal int) (bool, error) {
	result, err := db.sql.ExecContext(ctx, `INSERT INTO creation_owner_artifacts(intent_id,ordinal) SELECT intent_id,? FROM creation_owner_publications WHERE intent_id=? AND state='dispatched' AND artifacts>? ON CONFLICT DO NOTHING`, ordinal, intentID, ordinal)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// SaveOwnerArtifact retains a created issue's identity and relationship.
func (db *DB) SaveOwnerArtifact(ctx context.Context, intentID int64, a OwnerPublicationArtifact) error {
	if a.IssueID < 1 || a.IssueNumber < 1 || !submissionState(a.Relationship, "uncertain", "linked", "attached") {
		return ErrCreationConflict
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE creation_owner_artifacts SET issue_id=?,issue_number=?,relationship=? WHERE intent_id=? AND ordinal=? AND (issue_id=0 OR issue_id=?)`, a.IssueID, a.IssueNumber, a.Relationship, intentID, a.Ordinal, a.IssueID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrCreationConflict
	}
	return nil
}

// OwnerArtifacts returns the begun artifacts in ordinal order.
func (db *DB) OwnerArtifacts(ctx context.Context, intentID int64) ([]OwnerPublicationArtifact, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT ordinal,issue_id,issue_number,relationship FROM creation_owner_artifacts WHERE intent_id=? ORDER BY ordinal LIMIT 5`, intentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnerPublicationArtifact
	for rows.Next() {
		var a OwnerPublicationArtifact
		if err := rows.Scan(&a.Ordinal, &a.IssueID, &a.IssueNumber, &a.Relationship); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FinishOwnerPublication records the final outcome of a dispatched publication.
func (c *CreationStore) FinishOwnerPublication(ctx context.Context, intentID int64, state string) error {
	if !submissionState(state, "confirmed", "uncertain") {
		return ErrCreationAuthority
	}
	var confirmed int64
	if state == "confirmed" {
		confirmed = c.now().UnixNano()
	}
	_, err := c.db.sql.ExecContext(ctx, `UPDATE creation_owner_publications SET state=?,confirmed_ns=? WHERE intent_id=? AND state IN ('dispatched','uncertain')`, state, confirmed, intentID)
	return err
}
