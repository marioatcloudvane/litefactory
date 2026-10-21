package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrPreventionEvidenceLimited means the retained local source prefix reached its byte or record ceiling.
var ErrPreventionEvidenceLimited = errors.New("prevention analysis: local evidence limit reached")

// ErrPreventionEvidenceUnavailable means selected local records lack a diagnosis or repair observation.
var ErrPreventionEvidenceUnavailable = errors.New("prevention analysis: local diagnosis or repair observation unavailable")

// PreventionLocalObservation is a bounded recorded judgment, not proof of causation.
type PreventionLocalObservation struct {
	InstanceID   int64           `json:"instance_id"`
	Kind         string          `json:"kind"`
	Revision     string          `json:"revision"`
	ReviewStatus string          `json:"review_status"`
	Body         json.RawMessage `json:"body"`
}

// boundedPreventionRecord checks the byte ceiling in SQL before payload transfer or decoding.
func boundedPreventionRecord(ctx context.Context, tx *sql.Tx, instance int64, record string, remaining int) (json.RawMessage, error) {
	var raw []byte
	var size int64
	err := tx.QueryRowContext(ctx, `SELECT length(o.payload), CASE WHEN length(o.payload)<=? THEN o.payload ELSE NULL END FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id WHERE a.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')=? ORDER BY o.id DESC LIMIT 1`, remaining, instance, record).Scan(&size, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if size > int64(remaining) {
		return nil, ErrPreventionEvidenceLimited
	}
	return raw, nil
}

func preventionRecordSize(ctx context.Context, tx *sql.Tx, instance int64, record string) (int, error) {
	var size int
	err := tx.QueryRowContext(ctx, `SELECT length(o.payload) FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id WHERE a.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')=? ORDER BY o.id DESC LIMIT 1`, instance, record).Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return size, err
}

// ReadPreventionBugEvidence returns only the selected issue's bounded local Bug
// diagnosis and repair observations. A partial prefix is valid even with an error;
// callers must retain it and report the missing or limited coverage separately.
func (db *DB) ReadPreventionBugEvidence(ctx context.Context, repository string, issue, limit, byteLimit int) ([]PreventionLocalObservation, error) {
	return db.readPreventionBugEvidence(ctx, 0, repository, issue, limit, byteLimit)
}

func (db *DB) readPreventionBugEvidence(ctx context.Context, repoID int64, repository string, issue, limit, byteLimit int) ([]PreventionLocalObservation, error) {
	if repository == "" || issue < 1 || limit < 1 || limit > 20 || byteLimit < 1 || byteLimit > 65536 {
		return nil, errors.New("prevention analysis: invalid bounded local scope")
	}
	out := []PreventionLocalObservation{}
	used := 0
	var gaps error
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT g.id FROM graph_instances g JOIN sessions s ON s.id=g.session_id JOIN graph_definitions d ON d.id=g.definition_id WHERE (?=0 OR s.repo_id=?) AND s.task_ref=? COLLATE NOCASE AND d.workflow='bug' ORDER BY g.id DESC LIMIT ?`, repoID, repoID, fmt.Sprintf("github:%s#%d", repository, issue), limit+1)
		if e != nil {
			return e
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		if len(ids) == 0 {
			return ErrPreventionEvidenceUnavailable
		}
		if len(ids) > limit {
			ids = ids[:limit]
			gaps = errors.Join(gaps, ErrPreventionEvidenceLimited)
		}
		for _, id := range ids {
			for _, kind := range []string{"diagnosis", "repair_eligibility"} {
				remaining := byteLimit - used
				// Reserve wrapper bytes before reading either recorded payload.
				if remaining <= 32 {
					return errors.Join(gaps, ErrPreventionEvidenceLimited)
				}
				if kind == "diagnosis" {
					diagnosisSize, e := preventionRecordSize(ctx, tx, id, kind)
					if e != nil && !errors.Is(e, ErrNotFound) {
						return errors.Join(gaps, e)
					}
					if e == nil {
						approvalSize, e := preventionRecordSize(ctx, tx, id, "diagnosis_approval")
						if errors.Is(e, ErrNotFound) {
							approvalSize = 4
						} else if e != nil {
							return errors.Join(gaps, e)
						}
						if diagnosisSize+approvalSize+32 > remaining {
							return errors.Join(gaps, ErrPreventionEvidenceLimited)
						}
					}
				}
				raw, e := boundedPreventionRecord(ctx, tx, id, kind, remaining-32)
				if errors.Is(e, ErrNotFound) {
					gaps = errors.Join(gaps, ErrPreventionEvidenceUnavailable)
					continue
				}
				if e != nil {
					return errors.Join(gaps, e)
				}
				observation := PreventionLocalObservation{InstanceID: id, Kind: kind, ReviewStatus: "recorded", Body: raw}
				if kind == "diagnosis" {
					approval, e := boundedPreventionRecord(ctx, tx, id, "diagnosis_approval", remaining-len(raw)-32)
					if errors.Is(e, ErrNotFound) {
						approval = json.RawMessage(`null`)
					} else if e != nil {
						return errors.Join(gaps, e)
					}
					var diagnosis struct {
						Revision    int64  `json:"revision"`
						Digest      string `json:"digest"`
						ReviewReady bool   `json:"review_ready"`
					}
					if e = json.Unmarshal(raw, &diagnosis); e != nil {
						return e
					}
					observation.Revision = fmt.Sprintf("%d:%s", diagnosis.Revision, diagnosis.Digest)
					observation.ReviewStatus = "proposed"
					if string(approval) != "null" {
						var accepted GraphDiagnosisApproval
						if e = json.Unmarshal(approval, &accepted); e != nil {
							return e
						}
						var reshaped int
						if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_outputs o JOIN graph_node_attempts n ON n.id=o.node_attempt_id WHERE n.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')='diagnosis_reshape' AND json_extract(CAST(o.payload AS TEXT),'$.revision')=? AND o.id>(SELECT MAX(p.id) FROM graph_outputs p JOIN graph_node_attempts x ON x.id=p.node_attempt_id WHERE x.instance_id=? AND p.kind='json' AND json_valid(CAST(p.payload AS TEXT)) AND json_extract(CAST(p.payload AS TEXT),'$.record')='diagnosis_approval')`, id, diagnosis.Revision, id).Scan(&reshaped); e != nil {
							return e
						}
						if diagnosis.ReviewReady && accepted.Revision == diagnosis.Revision && accepted.Digest == diagnosis.Digest && reshaped == 0 {
							observation.ReviewStatus = "reviewed"
						}
					}
					// Concatenation retains the exact raw source bytes, including whitespace.
					observation.Body = append(append(append(append(json.RawMessage(`{"diagnosis":`), raw...), []byte(`,"approval":`)...), approval...), '}')
				} else {
					var repair struct {
						AttemptID      int64 `json:"attempt_id"`
						ResultID       int64 `json:"result_id"`
						ResultRevision int64 `json:"result_revision"`
					}
					if e = json.Unmarshal(raw, &repair); e != nil {
						return e
					}
					observation.Revision = fmt.Sprintf("attempt:%d/result:%d:%d", repair.AttemptID, repair.ResultID, repair.ResultRevision)
				}
				if len(observation.Body) > remaining {
					return errors.Join(gaps, ErrPreventionEvidenceLimited)
				}
				used += len(observation.Body)
				out = append(out, observation)
			}
		}
		return gaps
	})
	return out, err
}
