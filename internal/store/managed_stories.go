package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// WorkflowLegacy preserves immediate acceptance/closure. WorkflowSeparated
// records engineering independently and reserves acceptance for Validate.
const (
	WorkflowLegacy    = 1
	WorkflowSeparated = 2
)

// ManagedFeature exists only for plans created through the managed-story protocol.
type ManagedFeature struct {
	WorkflowVersion      int
	EngineeringSucceeded bool
	ParentTask           string
	Generation           int
	State                string
	Ready                bool
}

// ManagedStory holds identity and execution metadata, never an editable specification.
type ManagedStory struct {
	EngineeringComplete bool
	ParentTask          string
	Generation          int
	Key                 string
	IssueNumber         int
	IssueID             int64
	BodyHash            string
	Dependencies        []string
	State               string
}

// StoryPublication is recorded before any network mutation. A pending operation
// is reconciled before another create may be attempted.
type StoryPublication struct {
	ParentTask   string
	Generation   int
	Key          string
	OperationKey string
	DesiredHash  string
	IssueNumber  int
	IssueID      int64
	Delivered    bool
}

// GetManagedFeature loads the durable generation and lifecycle state for a parent.
func (db *DB) GetManagedFeature(ctx context.Context, parent string) (ManagedFeature, error) {
	var f ManagedFeature
	err := db.sql.QueryRowContext(ctx, `SELECT parent_task,generation,state,ready,workflow_version,engineering_succeeded FROM managed_features WHERE parent_task=?`, parent).Scan(&f.ParentTask, &f.Generation, &f.State, &f.Ready, &f.WorkflowVersion, &f.EngineeringSucceeded)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, fmt.Errorf("store: get managed feature: %w", err)
	}
	return f, nil
}

// PutManagedFeature initializes a feature; existing generations must be changed
// through CompareManagedFeature so a replay cannot reset implementation state.
func (db *DB) PutManagedFeature(ctx context.Context, f ManagedFeature) error {
	if f.ParentTask == "" || f.Generation < 1 || f.State != "planning" {
		return fmt.Errorf("store: invalid new managed feature")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO managed_features(parent_task,generation,state,workflow_version) VALUES(?,?,?,?) ON CONFLICT(parent_task) DO NOTHING`, f.ParentTask, f.Generation, f.State, f.WorkflowVersion)
	if err != nil {
		return fmt.Errorf("store: create managed feature: %w", err)
	}
	return nil
}

// CompareManagedFeature changes state only if the caller still holds the expected generation and state.
func (db *DB) CompareManagedFeature(ctx context.Context, f ManagedFeature, expectedState string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE managed_features SET state=? WHERE parent_task=? AND generation=? AND state=?`, f.State, f.ParentTask, f.Generation, expectedState)
	if err != nil {
		return fmt.Errorf("store: transition managed feature: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: managed feature changed")
	}
	return nil
}

// PutManagedStory inserts an identity or updates specification metadata only while that same identity is planning.
func (db *DB) PutManagedStory(ctx context.Context, s ManagedStory) error {
	deps, err := json.Marshal(s.Dependencies)
	if err != nil {
		return fmt.Errorf("store: encode story dependencies: %w", err)
	}
	res, err := db.sql.ExecContext(ctx, `INSERT INTO managed_stories(parent_task,generation,story_key,issue_number,issue_id,body_hash,dependencies,state) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(parent_task,generation,story_key) DO UPDATE SET body_hash=excluded.body_hash,dependencies=excluded.dependencies WHERE managed_stories.issue_id=excluded.issue_id AND managed_stories.issue_number=excluded.issue_number AND managed_stories.state='planning'`, s.ParentTask, s.Generation, s.Key, s.IssueNumber, s.IssueID, s.BodyHash, string(deps), s.State)
	if err != nil {
		return fmt.Errorf("store: save managed story: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: managed story identity or state changed")
	}
	return nil
}

// ListManagedStories returns a generation in stable story-key order for reproducible execution.
func (db *DB) ListManagedStories(ctx context.Context, parent string, generation int) ([]ManagedStory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT parent_task,generation,story_key,issue_number,issue_id,body_hash,dependencies,state,engineering_complete FROM managed_stories WHERE parent_task=? AND generation=? ORDER BY story_key`, parent, generation)
	if err != nil {
		return nil, fmt.Errorf("store: list managed stories: %w", err)
	}
	defer rows.Close()
	var out []ManagedStory
	for rows.Next() {
		var s ManagedStory
		var deps string
		if err := rows.Scan(&s.ParentTask, &s.Generation, &s.Key, &s.IssueNumber, &s.IssueID, &s.BodyHash, &deps, &s.State, &s.EngineeringComplete); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(deps), &s.Dependencies); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CompareManagedStory changes state only for the expected issue identity and prior state.
func (db *DB) CompareManagedStory(ctx context.Context, s ManagedStory, expectedState string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE managed_stories SET state=? WHERE parent_task=? AND generation=? AND story_key=? AND issue_id=? AND state=?`, s.State, s.ParentTask, s.Generation, s.Key, s.IssueID, expectedState)
	if err != nil {
		return fmt.Errorf("store: transition managed story: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: managed story changed")
	}
	return nil
}

// GetStoryPublication loads the durable publication record used to reconcile retries.
func (db *DB) GetStoryPublication(ctx context.Context, parent string, generation int, key string) (StoryPublication, error) {
	var p StoryPublication
	err := db.sql.QueryRowContext(ctx, `SELECT parent_task,generation,story_key,operation_key,desired_hash,issue_number,issue_id,delivered FROM story_publications WHERE parent_task=? AND generation=? AND story_key=?`, parent, generation, key).Scan(&p.ParentTask, &p.Generation, &p.Key, &p.OperationKey, &p.DesiredHash, &p.IssueNumber, &p.IssueID, &p.Delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("store: get story publication: %w", err)
	}
	return p, nil
}

// BeginStoryPublication records intent before the caller attempts a forge mutation.
func (db *DB) BeginStoryPublication(ctx context.Context, p StoryPublication) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO story_publications(parent_task,generation,story_key,operation_key,desired_hash) VALUES(?,?,?,?,?)`, p.ParentTask, p.Generation, p.Key, p.OperationKey, p.DesiredHash)
	if err != nil {
		return fmt.Errorf("store: begin story publication: %w", err)
	}
	return nil
}

// SaveStoryPublication records the discovered issue and delivery state only for the authorized content hash.
func (db *DB) SaveStoryPublication(ctx context.Context, p StoryPublication) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE story_publications SET issue_number=?,issue_id=?,delivered=? WHERE operation_key=? AND desired_hash=?`, p.IssueNumber, p.IssueID, p.Delivered, p.OperationKey, p.DesiredHash)
	if err != nil {
		return fmt.Errorf("store: save story publication: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// StoryRevisionOperation binds a current-base authorization to one complete edit.
type StoryRevisionOperation struct {
	OperationKey string
	ParentTask   string
	Generation   int
	Key          string
	ExpectedBase string
	DesiredHash  string
	Delivered    bool
}

// GetStoryRevisionOperation loads the recorded authorization for a replayable story edit.
func (db *DB) GetStoryRevisionOperation(ctx context.Context, key string) (StoryRevisionOperation, error) {
	var op StoryRevisionOperation
	err := db.sql.QueryRowContext(ctx, `SELECT operation_key,parent_task,generation,story_key,expected_base,desired_hash,delivered FROM story_revision_operations WHERE operation_key=?`, key).Scan(&op.OperationKey, &op.ParentTask, &op.Generation, &op.Key, &op.ExpectedBase, &op.DesiredHash, &op.Delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return op, ErrNotFound
	}
	if err != nil {
		return op, fmt.Errorf("store: get story revision operation: %w", err)
	}
	return op, nil
}

// BeginStoryRevisionOperation persists the expected base and desired hash before editing the forge.
func (db *DB) BeginStoryRevisionOperation(ctx context.Context, op StoryRevisionOperation) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO story_revision_operations(operation_key,parent_task,generation,story_key,expected_base,desired_hash) VALUES(?,?,?,?,?,?)`, op.OperationKey, op.ParentTask, op.Generation, op.Key, op.ExpectedBase, op.DesiredHash)
	if err != nil {
		return fmt.Errorf("store: begin story revision: %w", err)
	}
	return nil
}

// DeliverStoryRevisionOperation marks a reconciled story edit delivered without changing its authorization.
func (db *DB) DeliverStoryRevisionOperation(ctx context.Context, key string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE story_revision_operations SET delivered=1 WHERE operation_key=?`, key)
	if err != nil {
		return fmt.Errorf("store: deliver story revision: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// SetManagedFeatureReady is called false before publication and true only after
// all verified story links have been committed and pushed successfully.
func (db *DB) SetManagedFeatureReady(ctx context.Context, parent string, generation int, ready bool) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE managed_features SET ready=? WHERE parent_task=? AND generation=? AND state='planning'`, ready, parent, generation)
	if err != nil {
		return fmt.Errorf("store: set managed plan readiness: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: plan generation is no longer editable")
	}
	return nil
}
