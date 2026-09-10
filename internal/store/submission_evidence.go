package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// MaxSubmissionRetainedBytes is the shared 64 MiB per-run accounting limit.
const MaxSubmissionRetainedBytes = 64 << 20

var submissionCriterionID = regexp.MustCompile(`^AC-(?:00[1-9]|0[1-9][0-9]|[1-9][0-9]{2,5})$`)

func submissionDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func validSubmissionDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && value == strings.ToLower(value)
}

// SubmissionSnapshotFile retains exact bytes including deletion facts.
type SubmissionSnapshotFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	SHA256  string `json:"sha256"`
	Deleted bool   `json:"deleted"`
}

// SubmissionSnapshot is immutable scoped evidence, stored once per reference.
type SubmissionSnapshot struct {
	RunID                                               int64
	Attempt, Reference, Parent, Story, Revision, Digest string
	Generation                                          int
	Files                                               []SubmissionSnapshotFile
}

// RetainSubmissionSnapshot preserves complete provenance and file bytes in one
// bounded transaction; duplicate references must contain byte-identical facts.
func (db *DB) RetainSubmissionSnapshot(ctx context.Context, s SubmissionSnapshot) error {
	if s.Reference == "" || len(s.Reference) > 256 || len(s.Files) == 0 || len(s.Files) > 256 {
		return fmt.Errorf("store: invalid snapshot reference or file count")
	}
	var manifest strings.Builder
	previous := ""
	rawBytes := 0
	for _, f := range s.Files {
		if len(f.Content) > MaxSubmissionRetainedBytes-rawBytes {
			return fmt.Errorf("store: retained snapshot exceeds run budget")
		}
		rawBytes += len(f.Content)
		if !safeSubmissionPath(f.Path) || f.Path <= previous || submissionDigest(f.Content) != f.SHA256 || (f.Deleted && len(f.Content) != 0) {
			return fmt.Errorf("store: invalid retained file")
		}
		previous = f.Path
		fmt.Fprintf(&manifest, "%s\x00%s\x00%t\n", f.Path, f.SHA256, f.Deleted)
	}
	if submissionDigest([]byte(manifest.String())) != s.Digest {
		return fmt.Errorf("store: invalid snapshot digest")
	}
	payload, err := json.Marshal(s.Files)
	if err != nil {
		return err
	}
	if len(payload) > MaxSubmissionRetainedBytes {
		return fmt.Errorf("store: retained snapshot exceeds run budget")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := currentSubmissionBinding(ctx, tx, s.RunID, s.Attempt, s.Story, s.Revision); err != nil {
			return err
		}
		var parent string
		var generation int
		if err := tx.QueryRowContext(ctx, `SELECT parent_task,generation FROM submission_attempts WHERE run_id=? AND attempt=? AND stage IN ('engineering','legacy')`, s.RunID, s.Attempt).Scan(&parent, &generation); err != nil {
			return err
		}
		if parent != s.Parent || generation != s.Generation {
			return fmt.Errorf("store: snapshot provenance conflict")
		}
		var oldPayload []byte
		var oldParent, oldStory, oldRevision, oldDigest string
		var oldGeneration int
		err := tx.QueryRowContext(ctx, `SELECT parent_task,generation,story_key,revision,digest,payload FROM submission_snapshots WHERE run_id=? AND reference=?`, s.RunID, s.Reference).Scan(&oldParent, &oldGeneration, &oldStory, &oldRevision, &oldDigest, &oldPayload)
		if err == nil {
			if oldParent != s.Parent || oldGeneration != s.Generation || oldStory != s.Story || oldRevision != s.Revision || oldDigest != s.Digest || string(oldPayload) != string(payload) {
				return fmt.Errorf("store: immutable snapshot reference conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := reserveSubmissionBytes(ctx, tx, s.RunID, len(payload)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO submission_snapshots VALUES(?,?,?,?,?,?,?,?,?)`, s.RunID, s.Reference, s.Attempt, s.Parent, s.Generation, s.Story, s.Revision, s.Digest, payload)
		return err
	})
}

// GetSubmissionSnapshot returns complete retained bytes, never a mutable path.
func (db *DB) GetSubmissionSnapshot(ctx context.Context, runID int64, reference string) (SubmissionSnapshot, error) {
	s := SubmissionSnapshot{RunID: runID, Reference: reference}
	var payload []byte
	err := db.sql.QueryRowContext(ctx, `SELECT attempt,parent_task,generation,story_key,revision,digest,payload FROM submission_snapshots WHERE run_id=? AND reference=?`, runID, reference).Scan(&s.Attempt, &s.Parent, &s.Generation, &s.Story, &s.Revision, &s.Digest, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(payload, &s.Files)
	return s, err
}

// SubmissionCheck preserves factual failed checks too; retention alone grants
// neither engineering completion nor acceptance.
type SubmissionCheck struct {
	RunID                                 int64
	Attempt, Reference, SnapshotRef, Name string
	Executed, Passed                      bool
	Provenance, Result                    []byte
}

// RetainSubmissionCheck binds an immutable check to one retained snapshot.
func (db *DB) RetainSubmissionCheck(ctx context.Context, c SubmissionCheck) error {
	if c.Reference == "" || len(c.Reference) > 256 || c.Name == "" || len(c.Name) > 256 || len(c.Provenance) == 0 || len(c.Result) == 0 || len(c.Provenance)+len(c.Result) > 4<<20 {
		return fmt.Errorf("store: invalid retained check")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var story, revision string
		if err := tx.QueryRowContext(ctx, `SELECT story_key,revision FROM submission_snapshots WHERE run_id=? AND reference=?`, c.RunID, c.SnapshotRef).Scan(&story, &revision); err != nil {
			return err
		}
		if err := currentSubmissionBinding(ctx, tx, c.RunID, c.Attempt, story, revision); err != nil {
			return err
		}
		var stage string
		if err := tx.QueryRowContext(ctx, `SELECT stage FROM submission_attempts WHERE run_id=? AND attempt=?`, c.RunID, c.Attempt).Scan(&stage); err != nil {
			return err
		}
		if stage != "engineering" && stage != "legacy" {
			return fmt.Errorf("store: this stage cannot retain new execution")
		}

		old, err := scanSubmissionCheck(tx.QueryRowContext(ctx, `SELECT run_id,reference,attempt,snapshot_ref,name,executed,passed,provenance,result FROM submission_checks WHERE run_id=? AND reference=?`, c.RunID, c.Reference))
		if err == nil {
			if old.SnapshotRef != c.SnapshotRef || old.Name != c.Name || old.Executed != c.Executed || old.Passed != c.Passed || string(old.Provenance) != string(c.Provenance) || string(old.Result) != string(c.Result) {
				return fmt.Errorf("store: immutable check reference conflict")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := reserveSubmissionBytes(ctx, tx, c.RunID, len(c.Provenance)+len(c.Result)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO submission_checks VALUES(?,?,?,?,?,?,?,?,?)`, c.RunID, c.Reference, c.Attempt, c.SnapshotRef, c.Name, c.Executed, c.Passed, c.Provenance, c.Result)
		return err
	})
}
func scanSubmissionCheck(row interface{ Scan(...interface{}) error }) (SubmissionCheck, error) {
	var c SubmissionCheck
	err := row.Scan(&c.RunID, &c.Reference, &c.Attempt, &c.SnapshotRef, &c.Name, &c.Executed, &c.Passed, &c.Provenance, &c.Result)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// GetSubmissionCheck reads the original execution facts and source attribution.
func (db *DB) GetSubmissionCheck(ctx context.Context, runID int64, reference string) (SubmissionCheck, error) {
	return scanSubmissionCheck(db.sql.QueryRowContext(ctx, `SELECT run_id,reference,attempt,snapshot_ref,name,executed,passed,provenance,result FROM submission_checks WHERE run_id=? AND reference=?`, runID, reference))
}

// SubmissionRetainedBytes exposes exact accounted transport/evidence bytes.
func (db *DB) SubmissionRetainedBytes(ctx context.Context, runID int64) (int64, error) {
	var size int64
	err := db.sql.QueryRowContext(ctx, `SELECT retained_bytes FROM submission_run_counters WHERE run_id=?`, runID).Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return size, err
}
func reserveSubmissionBytes(ctx context.Context, tx *sql.Tx, runID int64, size int) error {
	if size < 0 || size > MaxSubmissionRetainedBytes {
		return fmt.Errorf("store: retained run budget exceeded")
	}
	res, err := tx.ExecContext(ctx, `UPDATE submission_run_counters SET retained_bytes=retained_bytes+? WHERE run_id=? AND retained_bytes<=?`, size, runID, MaxSubmissionRetainedBytes-size)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: retained run budget exceeded or unbound run")
	}
	return nil
}
func safeSubmissionPath(p string) bool {
	if p == "" || len(p) > 1024 || strings.ContainsAny(p, "\x00\r\n\\") || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		switch part {
		case ".git", ".litefactory-story-events", ".litefactory-plan-proposal.json", ".litefactory-plan-outcome.json":
			return false
		}
	}
	return true
}
