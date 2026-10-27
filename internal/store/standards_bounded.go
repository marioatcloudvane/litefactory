package store

import (
	"context"
	"database/sql"
	"errors"
)

// ErrStandardsReadTooLarge refuses evidence before variable-size stored fields
// are copied or decoded. Existing unrestricted authoring/runtime reads retain
// their contracts.
var ErrStandardsReadTooLarge = errors.New("standards: source exceeds acquisition bound")

// GetStandardsRevisionBounded checks byte lengths and row overhead in the same
// transaction as the immutable read. Its budget covers normalized snapshot
// fields, publication/approval metadata and the proposal decoded by the reader.
func (db *DB) GetStandardsRevisionBounded(ctx context.Context, id int64, maxBytes int) (StandardsRevision, error) {
	var out StandardsRevision
	if maxBytes < 1 {
		return out, ErrStandardsReadTooLarge
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if id == 0 {
			var err error
			id, err = standardsHead(ctx, tx)
			if err != nil {
				return err
			}
			if id == 0 {
				return ErrStandardsUnpublished
			}
		}
		var bytes int64
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0) FROM (
   SELECT 256+length(CAST(digest AS BLOB))+length(CAST(created_at AS BLOB)) AS bytes FROM standards_revisions WHERE id=?1
   UNION ALL SELECT 128+length(CAST(id AS BLOB))+length(CAST(name AS BLOB))+length(CAST(description AS BLOB)) FROM standards_categories WHERE revision_id=?1
   UNION ALL SELECT 256+length(CAST(id AS BLOB))+length(CAST(category_id AS BLOB))+length(CAST(rule AS BLOB))+length(CAST(description AS BLOB))+length(CAST(evidence AS BLOB))+length(CAST(scope AS BLOB))+length(CAST(check_kind AS BLOB)) FROM standards_standards WHERE revision_id=?1
   UNION ALL SELECT 128+length(CAST(standard_id AS BLOB))+length(CAST(role AS BLOB)) FROM standards_applies_to WHERE revision_id=?1
   UNION ALL SELECT 128+length(CAST(standard_id AS BLOB))+length(CAST(policy_json AS BLOB)) FROM standards_exception_policies WHERE revision_id=?1
   UNION ALL SELECT 256+length(CAST(proposal_id AS BLOB))+length(CAST(change_digest AS BLOB))+length(CAST(result_digest AS BLOB))+length(CAST(owner AS BLOB))+length(CAST(provenance AS BLOB))+length(CAST(created_at AS BLOB)) FROM standards_publications WHERE revision_id=?1
   UNION ALL SELECT 256+length(CAST(a.proposal_id AS BLOB))+length(CAST(a.change_digest AS BLOB))+length(CAST(a.result_digest AS BLOB))+length(CAST(a.owner AS BLOB))+length(CAST(a.provenance AS BLOB))+length(CAST(a.created_at AS BLOB)) FROM standards_approvals a JOIN standards_publications p ON p.proposal_id=a.proposal_id AND p.version=a.version WHERE p.revision_id=?1
   UNION ALL SELECT 256+length(CAST(v.proposal_id AS BLOB))+length(CAST(v.operations_json AS BLOB))+length(CAST(v.change_digest AS BLOB))+length(CAST(v.result_digest AS BLOB))+length(CAST(v.author AS BLOB))+length(CAST(v.created_at AS BLOB)) FROM standards_proposal_versions v JOIN standards_publications p ON p.proposal_id=v.proposal_id AND p.version=v.version WHERE p.revision_id=?1
  )`, id).Scan(&bytes)
		if err != nil {
			return err
		}
		if bytes > int64(maxBytes) {
			return ErrStandardsReadTooLarge
		}
		out, err = readStandardsRevision(ctx, tx, id)
		return err
	})
	return out, err
}

// GetStandardsProposalBounded protects the verification reread as well as the
// proposal read internal to GetStandardsRevisionBounded.
func (db *DB) GetStandardsProposalBounded(ctx context.Context, id string, version int64, maxBytes int) (StandardsProposalVersion, error) {
	var out StandardsProposalVersion
	if maxBytes < 1 {
		return out, ErrStandardsReadTooLarge
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if version == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT current_version FROM standards_proposals WHERE id=?`, id).Scan(&version); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrNotFound
				}
				return err
			}
		}
		var bytes int64
		err := tx.QueryRowContext(ctx, `SELECT 256+length(CAST(proposal_id AS BLOB))+length(CAST(operations_json AS BLOB))+length(CAST(change_digest AS BLOB))+length(CAST(result_digest AS BLOB))+length(CAST(author AS BLOB))+length(CAST(created_at AS BLOB)) FROM standards_proposal_versions WHERE proposal_id=? AND version=?`, id, version).Scan(&bytes)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if bytes > int64(maxBytes) {
			return ErrStandardsReadTooLarge
		}
		out, err = readStandardsProposal(ctx, tx, id, version)
		return err
	})
	return out, err
}
