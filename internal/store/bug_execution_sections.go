package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// BugRepairReferences is the repair-owned projection. References are not
// reservations or source-readiness proof; their owning activity must resolve
// them against its authoritative ledger before any substantive work.
type BugRepairReferences struct {
	Reservations   []string `json:"reservations,omitempty"`
	SourceCommit   string   `json:"source_commit,omitempty"`
	SourceTree     string   `json:"source_tree,omitempty"`
	SaveOperations []string `json:"save_operations,omitempty"`
}

// BugCheckReferences keeps the exact input identities and immutable evidence
// references used by the check and child owners. Missing fields mean pending.
type BugCheckReferences struct {
	TargetRepository string   `json:"target_repository,omitempty"`
	TargetBranch     string   `json:"target_branch,omitempty"`
	TargetCommit     string   `json:"target_commit,omitempty"`
	RecipeDigest     string   `json:"recipe_digest,omitempty"`
	ChildCalls       []string `json:"child_calls,omitempty"`
	Evidence         []string `json:"evidence,omitempty"`
}

// BugDeliveryReferences retains remote identities and owed operations; it has
// no mutable approved/merged/closed flags that could authorize effects.
type BugDeliveryReferences struct {
	PRID           int64    `json:"pr_id,omitempty"`
	PRNumber       int64    `json:"pr_number,omitempty"`
	Reviews        []string `json:"reviews,omitempty"`
	Intents        []string `json:"intents,omitempty"`
	Observations   []string `json:"observations,omitempty"`
	PendingEffects []string `json:"pending_effects,omitempty"`
}

// BugFailureReferences retains causes, supporting evidence and owed reports.
type BugFailureReferences struct {
	Reasons             []string `json:"reasons,omitempty"`
	Evidence            []string `json:"evidence,omitempty"`
	PendingPublications []string `json:"pending_publications,omitempty"`
}

// BugExecutionSections is a copied read projection. Only typed owning-domain
// updates below can append a section; none can replace analysis or approvals.
type BugExecutionSections struct {
	Repair   *BugRepairReferences
	Checks   *BugCheckReferences
	Delivery *BugDeliveryReferences
	Failures *BugFailureReferences
}

func validateBugReferences(groups ...[]string) error {
	for _, group := range groups {
		seen := make(map[string]bool)
		for _, ref := range group {
			if strings.TrimSpace(ref) == "" || len(ref) > 8192 || seen[ref] {
				return errors.New("store: invalid or duplicate Bug execution reference")
			}
			seen[ref] = true
		}
	}
	return nil
}

// UpdateBugRepairReferences appends the repair-owned section under a revision guard.
func (d *DB) UpdateBugRepairReferences(ctx context.Context, session, expected int64, operation string, in BugRepairReferences) (int64, error) {
	if err := validateBugReferences(in.Reservations, in.SaveOperations); err != nil {
		return 0, err
	}
	return appendBugSection(ctx, d, session, expected, operation, "repair", in)
}

// UpdateBugCheckReferences appends the checks-owned section under a revision guard.
func (d *DB) UpdateBugCheckReferences(ctx context.Context, session, expected int64, operation string, in BugCheckReferences) (int64, error) {
	if err := validateBugReferences(in.ChildCalls, in.Evidence); err != nil {
		return 0, err
	}
	if in.RecipeDigest != "" && !validSubmissionDigest(in.RecipeDigest) {
		return 0, errors.New("store: invalid Bug recipe digest")
	}
	return appendBugSection(ctx, d, session, expected, operation, "checks", in)
}

// UpdateBugDeliveryReferences appends delivery references without granting authority.
func (d *DB) UpdateBugDeliveryReferences(ctx context.Context, session, expected int64, operation string, in BugDeliveryReferences) (int64, error) {
	if err := validateBugReferences(in.Reviews, in.Intents, in.Observations, in.PendingEffects); err != nil {
		return 0, err
	}
	if in.PRID < 0 || in.PRNumber < 0 || (in.PRID == 0) != (in.PRNumber == 0) {
		return 0, errors.New("store: incomplete Bug PR identity")
	}
	return appendBugSection(ctx, d, session, expected, operation, "delivery", in)
}

// UpdateBugFailureReferences appends failure evidence without replacing other sections.
func (d *DB) UpdateBugFailureReferences(ctx context.Context, session, expected int64, operation string, in BugFailureReferences) (int64, error) {
	if err := validateBugReferences(in.Reasons, in.Evidence, in.PendingPublications); err != nil {
		return 0, err
	}
	return appendBugSection(ctx, d, session, expected, operation, "failures", in)
}

// bugSectionContent is a closed union: no public arbitrary JSON patch surface.
type bugSectionContent interface {
	BugRepairReferences | BugCheckReferences | BugDeliveryReferences | BugFailureReferences
}

// appendBugSection receives one of the four typed values through its owning
// public accessor. The private serialization boundary is never agent input.
func appendBugSection[T bugSectionContent](ctx context.Context, d *DB, session, expected int64, operation, section string, in T) (int64, error) {
	if session <= 0 || expected <= 0 || operation == "" || len(operation) > 256 {
		return 0, errors.New("store: invalid Bug section operation")
	}
	content, err := json.Marshal(in)
	if err != nil {
		return 0, err
	}
	if len(content) > 1<<20 {
		return 0, errors.New("store: Bug execution section exceeds 1 MiB")
	}
	digest := submissionDigest(append([]byte(section+"\x00"), content...))
	var revision int64
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		var oldSection, oldDigest string
		err := tx.QueryRowContext(ctx, `SELECT section,input_digest,revision FROM bug_execution_sections WHERE session_id=? AND operation=?`, session, operation).Scan(&oldSection, &oldDigest, &revision)
		if err == nil {
			if oldSection != section || oldDigest != digest {
				return errors.New("store: Bug section replay conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state, err := bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		revision = state.Revision + 1
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_execution_sections(session_id,section,revision,operation,input_digest,content) VALUES(?,?,?,?,?,?)`, session, section, revision, operation, digest, content); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=? WHERE session_id=?`, revision, session); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "section_"+section, operation, "")
	})
	return revision, err
}

// ReadBugExecutionSections returns the latest independently owned section projections.
func (d *DB) ReadBugExecutionSections(ctx context.Context, session int64) (BugExecutionSections, error) {
	var out BugExecutionSections
	rows, err := d.sql.QueryContext(ctx, `SELECT s.section,s.content FROM bug_execution_sections s WHERE s.session_id=? AND s.revision=(SELECT MAX(p.revision) FROM bug_execution_sections p WHERE p.session_id=s.session_id AND p.section=s.section) ORDER BY s.section`, session)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var section string
		var content []byte
		if err = rows.Scan(&section, &content); err != nil {
			return out, err
		}
		switch section {
		case "repair":
			out.Repair = &BugRepairReferences{}
			err = json.Unmarshal(content, out.Repair)
		case "checks":
			out.Checks = &BugCheckReferences{}
			err = json.Unmarshal(content, out.Checks)
		case "delivery":
			out.Delivery = &BugDeliveryReferences{}
			err = json.Unmarshal(content, out.Delivery)
		case "failures":
			out.Failures = &BugFailureReferences{}
			err = json.Unmarshal(content, out.Failures)
		default:
			return out, errors.New("store: unknown Bug section")
		}
		if err != nil {
			return out, err
		}
	}
	return out, rows.Err()
}
