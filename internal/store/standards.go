package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Standards transition errors distinguish retryable conflicts, damaged history and absent authority.
var (
	ErrStandardsConflict         = errors.New("standards: stale version or base; inspect and retry or rebase")
	ErrStandardsCorrupt          = errors.New("standards: corrupt retained record")
	ErrStandardsApprovalRequired = errors.New("standards: exact owner approval required")
	ErrStandardsUnpublished      = errors.New("standards: no published book")
)

// StandardsCategory is category metadata retained in a revision.
type StandardsCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// StandardsExceptionPolicy is the reviewed revision-specific limit on exceptions.
// A missing policy denies exceptions.
type StandardsExceptionPolicy struct {
	Version                   int      `json:"version"`
	Mode                      string   `json:"mode"`
	AllRegisteredRepositories bool     `json:"all_registered_repositories,omitempty"`
	RepositoryIDs             []int64  `json:"repository_ids,omitempty"`
	TaskRefs                  []string `json:"task_refs,omitempty"`
	MaxLifetimeSeconds        int64    `json:"max_lifetime_seconds,omitempty"`
}

// Standard check kinds name how compliance is shown.
const (
	StandardsCheckAutomated     = "automated"
	StandardsCheckSemiAutomated = "semi_automated"
	StandardsCheckManual        = "manual"
)

// StandardsStandard is one structured MUST rule in exactly one category,
// applied by the agent roles it names.
type StandardsStandard struct {
	ID              string                    `json:"id"`
	CategoryID      string                    `json:"category_id"`
	Rule            string                    `json:"rule"`
	Description     string                    `json:"description"`
	Evidence        string                    `json:"evidence"`
	Scope           string                    `json:"scope"`
	Check           string                    `json:"check"`
	ExceptionPolicy *StandardsExceptionPolicy `json:"exception_policy,omitempty"`
	AppliesTo       []string                  `json:"applies_to"`
}

// StandardsSnapshot is a complete book, independent of later edits.
type StandardsSnapshot struct {
	Categories []StandardsCategory `json:"categories"`
	Standards  []StandardsStandard `json:"standards"`
}

// StandardsOperation is a typed proposed category or standard mutation.
type StandardsOperation struct {
	Kind     string             `json:"kind"`
	ID       string             `json:"id,omitempty"`
	Category *StandardsCategory `json:"category,omitempty"`
	Standard *StandardsStandard `json:"standard,omitempty"`
}

// StandardsProposalVersion binds immutable operations to their base and canonical digests.
type StandardsProposalVersion struct {
	ProposalID    string               `json:"proposal_id"`
	Version       int64                `json:"version"`
	BaseRevision  int64                `json:"base_revision"`
	SchemaVersion int                  `json:"schema_version"`
	Operations    []StandardsOperation `json:"operations"`
	ChangeDigest  string               `json:"change_digest"`
	ResultDigest  string               `json:"result_digest"`
	Author        string               `json:"author"`
	CreatedAt     time.Time            `json:"created_at"`
}

// StandardsApproval records the local owner approval of one exact proposal version.
type StandardsApproval struct {
	ProposalID   string    `json:"proposal_id"`
	Version      int64     `json:"version"`
	BaseRevision int64     `json:"base_revision"`
	ChangeDigest string    `json:"change_digest"`
	ResultDigest string    `json:"result_digest"`
	Owner        string    `json:"owner"`
	Provenance   string    `json:"provenance"`
	CreatedAt    time.Time `json:"created_at"`
}

// StandardsPublication retains the approved change that produced a revision.
type StandardsPublication struct {
	RevisionID   int64             `json:"revision_id"`
	ProposalID   string            `json:"proposal_id"`
	Version      int64             `json:"version"`
	BaseRevision int64             `json:"base_revision"`
	ChangeDigest string            `json:"change_digest"`
	ResultDigest string            `json:"result_digest"`
	Owner        string            `json:"owner"`
	Provenance   string            `json:"provenance"`
	CreatedAt    time.Time         `json:"created_at"`
	Approval     StandardsApproval `json:"approval"`
}

// StandardsRevision includes complete contents and immutable publication provenance.
type StandardsRevision struct {
	ID             int64                 `json:"id"`
	ParentRevision int64                 `json:"parent_revision"`
	SchemaVersion  int                   `json:"schema_version"`
	Digest         string                `json:"digest"`
	Snapshot       StandardsSnapshot     `json:"snapshot"`
	CreatedAt      time.Time             `json:"created_at"`
	Publication    *StandardsPublication `json:"publication"`
}

// standardsWrite reports contention after rollback; callers decide when to retry.
func (db *DB) standardsWrite(ctx context.Context, fn func(*sql.Tx) error) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error { return standardsConversationWrite(ctx, tx, fn) })
	if err != nil && (strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked")) {
		return fmt.Errorf("%w: %w", ErrStandardsConflict, err)
	}
	return err
}
func standardsHead(ctx context.Context, tx *sql.Tx) (int64, error) {
	var n sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT current_revision FROM standards_book WHERE id=1`).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrStandardsCorrupt
	}
	return n.Int64, err
}

// StandardsHead returns zero when no book has been published.
func (db *DB) StandardsHead(ctx context.Context) (int64, error) {
	var id int64
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; id, err = standardsHead(ctx, tx); return err })
	return id, err
}
func readStandardsProposal(ctx context.Context, tx *sql.Tx, id string, version int64) (StandardsProposalVersion, error) {
	var p StandardsProposalVersion
	var base sql.NullInt64
	var ops, created string
	if version == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT current_version FROM standards_proposals WHERE id=?`, id).Scan(&version); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return p, ErrNotFound
			}
			return p, err
		}
	}
	err := tx.QueryRowContext(ctx, `SELECT proposal_id,version,base_revision,schema_version,operations_json,change_digest,result_digest,author,created_at FROM standards_proposal_versions WHERE proposal_id=? AND version=?`, id, version).Scan(&p.ProposalID, &p.Version, &base, &p.SchemaVersion, &ops, &p.ChangeDigest, &p.ResultDigest, &p.Author, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.BaseRevision = base.Int64
	if err = json.Unmarshal([]byte(ops), &p.Operations); err != nil {
		return p, fmt.Errorf("%w: proposal operations: %w", ErrStandardsCorrupt, err)
	}
	p.CreatedAt, err = ParseTime(created)
	if err != nil {
		return p, fmt.Errorf("%w: proposal time: %w", ErrStandardsCorrupt, err)
	}
	return p, nil
}

// GetStandardsProposal reads an immutable version; version zero selects the current version.
func (db *DB) GetStandardsProposal(ctx context.Context, id string, version int64) (StandardsProposalVersion, error) {
	var p StandardsProposalVersion
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = readStandardsProposal(ctx, tx, id, version)
		return err
	})
	return p, err
}

// ListStandardsProposals reads all current proposal versions from one stable snapshot.
func (db *DB) ListStandardsProposals(ctx context.Context) ([]StandardsProposalVersion, error) {
	out := []StandardsProposalVersion{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM standards_proposals ORDER BY id`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			p, err := readStandardsProposal(ctx, tx, id, 0)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// SaveStandardsProposal appends a version only when expected still identifies the unpublished current version.
func (db *DB) SaveStandardsProposal(ctx context.Context, p StandardsProposalVersion, expected int64) (StandardsProposalVersion, error) {
	p.CreatedAt = nowUTC().Truncate(time.Millisecond)
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "proposal", ID: p.ProposalID, Version: p.Version}
	}), func(tx *sql.Tx) error {
		var err error
		p, err = saveStandardsProposalTx(ctx, tx, p, expected)
		return err
	})
	return p, err
}

// saveStandardsProposalTx is shared by standalone edits and atomic analysis batches.
func saveStandardsProposalTx(ctx context.Context, tx *sql.Tx, p StandardsProposalVersion, expected int64) (StandardsProposalVersion, error) {
	if p.ProposalID == "" || expected < 0 || p.SchemaVersion != 1 || p.ChangeDigest == "" || p.ResultDigest == "" {
		return p, errors.New("standards: invalid proposal metadata")
	}
	ops, err := json.Marshal(p.Operations)
	if err != nil {
		return p, err
	}
	p.Version = expected + 1
	if p.CreatedAt.IsZero() {
		p.CreatedAt = nowUTC().Truncate(time.Millisecond)
	}
	if expected == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO standards_proposals(id,current_version) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, p.ProposalID, p.Version); err != nil {
			return p, err
		}
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM standards_proposal_versions WHERE proposal_id=?`, p.ProposalID).Scan(&n); err != nil {
			return p, err
		}
		if n != 0 {
			return p, ErrStandardsConflict
		}
	} else {
		res, err := tx.ExecContext(ctx, `UPDATE standards_proposals SET current_version=? WHERE id=? AND current_version=? AND published_version IS NULL`, p.Version, p.ProposalID, expected)
		if err != nil {
			return p, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return p, err
		}
		if n != 1 {
			return p, ErrStandardsConflict
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO standards_proposal_versions(proposal_id,version,base_revision,schema_version,operations_json,change_digest,result_digest,author,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, p.ProposalID, p.Version, nullID(p.BaseRevision), p.SchemaVersion, string(ops), p.ChangeDigest, p.ResultDigest, p.Author, FormatTime(p.CreatedAt))
	return p, err
}

func standardsBinding(p StandardsProposalVersion, a StandardsApproval) bool {
	return p.ProposalID == a.ProposalID && p.Version == a.Version && p.BaseRevision == a.BaseRevision && p.ChangeDigest == a.ChangeDigest && p.ResultDigest == a.ResultDigest
}
func readStandardsApproval(ctx context.Context, tx *sql.Tx, id string, version int64) (StandardsApproval, error) {
	var a StandardsApproval
	var base sql.NullInt64
	var created string
	err := tx.QueryRowContext(ctx, `SELECT proposal_id,version,base_revision,change_digest,result_digest,owner,provenance,created_at FROM standards_approvals WHERE proposal_id=? AND version=?`, id, version).Scan(&a.ProposalID, &a.Version, &base, &a.ChangeDigest, &a.ResultDigest, &a.Owner, &a.Provenance, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrStandardsApprovalRequired
	}
	if err != nil {
		return a, err
	}
	a.BaseRevision = base.Int64
	a.CreatedAt, err = ParseTime(created)
	return a, err
}

// GetStandardsApproval reads the exact retained approval or reports that approval is required.
func (db *DB) GetStandardsApproval(ctx context.Context, id string, version int64) (StandardsApproval, error) {
	var a StandardsApproval
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		a, err = readStandardsApproval(ctx, tx, id, version)
		return err
	})
	return a, err
}

// ApproveStandardsProposal records exact local approval after checking the current version and book head.
func (db *DB) ApproveStandardsProposal(ctx context.Context, a StandardsApproval) (StandardsApproval, error) {
	if a.Owner == "" || !StandardsProvenanceValid(a.Provenance) {
		return a, ErrStandardsApprovalRequired
	}
	a.CreatedAt = nowUTC().Truncate(time.Millisecond)
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "approval", ID: a.ProposalID, Version: a.Version}
	}), func(tx *sql.Tx) error {
		p, err := readStandardsProposal(ctx, tx, a.ProposalID, 0)
		if err != nil {
			return err
		}
		if !standardsBinding(p, a) {
			return ErrStandardsConflict
		}

		existing, err := readStandardsApproval(ctx, tx, a.ProposalID, a.Version)
		if err == nil {
			if !standardsBinding(p, existing) || existing.Owner != a.Owner {
				return ErrStandardsConflict
			}
			a = existing
			return nil
		}
		if !errors.Is(err, ErrStandardsApprovalRequired) {
			return err
		}
		head, err := standardsHead(ctx, tx)
		if err != nil {
			return err
		}
		if head != a.BaseRevision {
			return ErrStandardsConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO standards_approvals(proposal_id,version,base_revision,change_digest,result_digest,owner,provenance,created_at) VALUES(?,?,?,?,?,?,?,?)`, a.ProposalID, a.Version, nullID(a.BaseRevision), a.ChangeDigest, a.ResultDigest, a.Owner, a.Provenance, FormatTime(a.CreatedAt))
		return err
	})
	return a, err
}
func readStandardsRevision(ctx context.Context, tx *sql.Tx, id int64) (StandardsRevision, error) {
	var r StandardsRevision
	var parent sql.NullInt64
	var created string
	err := tx.QueryRowContext(ctx, `SELECT id,parent_revision,schema_version,digest,created_at FROM standards_revisions WHERE id=?`, id).Scan(&r.ID, &parent, &r.SchemaVersion, &r.Digest, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.ParentRevision = parent.Int64
	r.CreatedAt, err = ParseTime(created)
	if err != nil {
		return r, fmt.Errorf("%w: revision time", ErrStandardsCorrupt)
	}
	r.Snapshot.Categories = []StandardsCategory{}
	r.Snapshot.Standards = []StandardsStandard{}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,description FROM standards_categories WHERE revision_id=? ORDER BY id`, id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var c StandardsCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			_ = rows.Close()
			return r, err
		}
		r.Snapshot.Categories = append(r.Snapshot.Categories, c)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return r, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,category_id,rule,description,evidence,scope,check_kind FROM standards_standards WHERE revision_id=? ORDER BY id`, id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var s StandardsStandard
		if err := rows.Scan(&s.ID, &s.CategoryID, &s.Rule, &s.Description, &s.Evidence, &s.Scope, &s.Check); err != nil {
			_ = rows.Close()
			return r, err
		}
		s.AppliesTo = []string{}
		r.Snapshot.Standards = append(r.Snapshot.Standards, s)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return r, err
	}
	indices := map[string]int{}
	for i, s := range r.Snapshot.Standards {
		indices[s.ID] = i
	}
	rows, err = tx.QueryContext(ctx, `SELECT standard_id,policy_json FROM standards_exception_policies WHERE revision_id=? ORDER BY standard_id`, id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var sid, payload string
		if err := rows.Scan(&sid, &payload); err != nil {
			_ = rows.Close()
			return r, err
		}
		i, ok := indices[sid]
		if !ok {
			_ = rows.Close()
			return r, ErrStandardsCorrupt
		}
		var policy StandardsExceptionPolicy
		if err := json.Unmarshal([]byte(payload), &policy); err != nil {
			_ = rows.Close()
			return r, fmt.Errorf("%w: policy: %w", ErrStandardsCorrupt, err)
		}
		r.Snapshot.Standards[i].ExceptionPolicy = &policy
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return r, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT standard_id,role FROM standards_applies_to WHERE revision_id=? ORDER BY standard_id,role`, id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var sid, role string
		if err := rows.Scan(&sid, &role); err != nil {
			_ = rows.Close()
			return r, err
		}
		i, ok := indices[sid]
		if !ok {
			_ = rows.Close()
			return r, ErrStandardsCorrupt
		}
		r.Snapshot.Standards[i].AppliesTo = append(r.Snapshot.Standards[i].AppliesTo, role)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return r, err
	}
	var pub StandardsPublication
	var base sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT revision_id,proposal_id,version,base_revision,change_digest,result_digest,owner,provenance,created_at FROM standards_publications WHERE revision_id=?`, id).Scan(&pub.RevisionID, &pub.ProposalID, &pub.Version, &base, &pub.ChangeDigest, &pub.ResultDigest, &pub.Owner, &pub.Provenance, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrStandardsCorrupt
	}
	if err != nil {
		return r, err
	}
	pub.BaseRevision = base.Int64
	pub.CreatedAt, err = ParseTime(created)
	if err != nil {
		return r, ErrStandardsCorrupt
	}
	pub.Approval, err = readStandardsApproval(ctx, tx, pub.ProposalID, pub.Version)
	if err != nil {
		return r, fmt.Errorf("%w: publication approval: %w", ErrStandardsCorrupt, err)
	}
	p, err := readStandardsProposal(ctx, tx, pub.ProposalID, pub.Version)
	if err != nil {
		return r, fmt.Errorf("%w: publication proposal: %w", ErrStandardsCorrupt, err)
	}
	if !standardsBinding(p, pub.Approval) || pub.Owner != pub.Approval.Owner || pub.BaseRevision != r.ParentRevision || pub.ResultDigest != r.Digest || pub.ChangeDigest != p.ChangeDigest || pub.ResultDigest != p.ResultDigest || pub.BaseRevision != p.BaseRevision {
		return r, ErrStandardsCorrupt
	}
	r.Publication = &pub
	return r, nil
}

// GetStandardsRevision reads complete immutable contents; zero selects the current book.
func (db *DB) GetStandardsRevision(ctx context.Context, id int64) (StandardsRevision, error) {
	var r StandardsRevision
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
		var err error
		r, err = readStandardsRevision(ctx, tx, id)
		return err
	})
	return r, err
}

// ListStandardsRevisions reads complete revision history from one stable snapshot.
func (db *DB) ListStandardsRevisions(ctx context.Context) ([]StandardsRevision, error) {
	out := []StandardsRevision{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM standards_revisions ORDER BY id`)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, err := readStandardsRevision(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

// PublishStandardsProposal makes the complete snapshot, provenance and head visible
// together. A committed identity is recovered before checking a now-stale head.
func (db *DB) PublishStandardsProposal(ctx context.Context, p StandardsProposalVersion, snapshot StandardsSnapshot, owner string) (StandardsRevision, error) {
	return db.PublishStandardsProposalWithProvenance(ctx, p, snapshot, owner, "local-cli")
}

// PublishStandardsProposalWithProvenance records authenticated adapter provenance.
func (db *DB) PublishStandardsProposalWithProvenance(ctx context.Context, p StandardsProposalVersion, snapshot StandardsSnapshot, owner, provenance string) (StandardsRevision, error) {
	var out StandardsRevision
	if owner == "" || !StandardsProvenanceValid(provenance) {
		return out, ErrStandardsApprovalRequired
	}
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "book", ID: p.ProposalID, Version: p.Version, Revision: out.ID}
	}), func(tx *sql.Tx) error {
		stored, err := readStandardsProposal(ctx, tx, p.ProposalID, p.Version)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, p) {
			return ErrStandardsConflict
		}
		var published int64
		err = tx.QueryRowContext(ctx, `SELECT revision_id FROM standards_publications WHERE proposal_id=? AND version=?`, p.ProposalID, p.Version).Scan(&published)
		if err == nil {
			out, err = readStandardsRevision(ctx, tx, published)
			if err != nil {
				return err
			}
			if out.Publication.Owner != owner || !reflect.DeepEqual(out.Snapshot, snapshot) {
				return ErrStandardsConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		current, err := readStandardsProposal(ctx, tx, p.ProposalID, 0)
		if err != nil {
			return err
		}
		if current.Version != p.Version {
			return ErrStandardsConflict
		}
		head, err := standardsHead(ctx, tx)
		if err != nil {
			return err
		}
		if head != p.BaseRevision {
			return ErrStandardsConflict
		}
		a, err := readStandardsApproval(ctx, tx, p.ProposalID, p.Version)
		if err != nil {
			return err
		}
		if !standardsBinding(p, a) || a.Owner != owner || !StandardsProvenanceValid(a.Provenance) {
			return ErrStandardsApprovalRequired
		}
		now := FormatTime(nowUTC())
		res, err := tx.ExecContext(ctx, `INSERT INTO standards_revisions(parent_revision,schema_version,digest,created_at) VALUES(?,?,?,?)`, nullID(p.BaseRevision), p.SchemaVersion, p.ResultDigest, now)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for _, c := range snapshot.Categories {
			if _, err := tx.ExecContext(ctx, `INSERT INTO standards_categories(revision_id,id,name,description) VALUES(?,?,?,?)`, id, c.ID, c.Name, c.Description); err != nil {
				return err
			}
		}
		for _, s := range snapshot.Standards {
			if _, err := tx.ExecContext(ctx, `INSERT INTO standards_standards(revision_id,id,category_id,rule,description,evidence,scope,check_kind) VALUES(?,?,?,?,?,?,?,?)`, id, s.ID, s.CategoryID, s.Rule, s.Description, s.Evidence, s.Scope, s.Check); err != nil {
				return err
			}
			if s.ExceptionPolicy != nil {
				payload, err := json.Marshal(s.ExceptionPolicy)
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO standards_exception_policies(revision_id,standard_id,policy_json) VALUES(?,?,?)`, id, s.ID, string(payload)); err != nil {
					return err
				}
			}
			for _, role := range s.AppliesTo {
				if _, err := tx.ExecContext(ctx, `INSERT INTO standards_applies_to(revision_id,standard_id,role) VALUES(?,?,?)`, id, s.ID, role); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO standards_publications(revision_id,proposal_id,version,base_revision,change_digest,result_digest,owner,provenance,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, p.ProposalID, p.Version, nullID(p.BaseRevision), p.ChangeDigest, p.ResultDigest, owner, provenance, now); err != nil {
			return err
		}
		res, err = tx.ExecContext(ctx, `UPDATE standards_proposals SET published_version=? WHERE id=? AND current_version=? AND published_version IS NULL`, p.Version, p.ProposalID, p.Version)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrStandardsConflict
		}
		res, err = tx.ExecContext(ctx, `UPDATE standards_book SET current_revision=? WHERE id=1 AND current_revision IS ?`, id, nullID(p.BaseRevision))
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrStandardsConflict
		}
		out, err = readStandardsRevision(ctx, tx, id)
		return err
	})
	if err != nil {
		return StandardsRevision{}, err
	}
	return out, nil
}
