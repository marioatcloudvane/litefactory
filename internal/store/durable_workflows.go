package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

// Workflow errors distinguish request conflicts, consumed revisions and stale workers.
var (
	ErrWorkflowConflict = errors.New("workflow request key has different inputs")
	ErrWorkflowRevision = errors.New("workflow revision is stale or already reserved")
	ErrWorkflowFenced   = errors.New("workflow execution fence is stale")
)

// WorkflowRequest freezes an owner-scoped operation independently of its client.
type WorkflowRequest struct {
	OwnerID                                                string
	OwnerGeneration                                        int64
	Key, Digest, ProposalID, JobID, Snapshot, Inputs, Kind string
	ExpectedRevision                                       int64
	CreatedAt                                              time.Time
}

// WorkflowJob is a retained projection; domain records remain authoritative.
type WorkflowJob struct {
	ID, OwnerID                           string
	OwnerGeneration                       int64
	Snapshot, State                       string
	Revision                              int64
	Result, Questions, DomainRef, Summary string
	CreatedAt, UpdatedAt                  time.Time
}

// WorkflowAttempt links one confirmed action to its fenced background execution.
type WorkflowAttempt struct {
	ID                                        int64
	JobID, ProposalID, ExecutionID, State     string
	AdmissionKind, AdvisoryID                 string
	Fence                                     int64
	QueuedAt, StartedAt, Deadline, FinishedAt time.Time
}

const workflowRequestColumns = `owner_id,owner_generation,request_key,digest,proposal_id,job_id,snapshot,inputs,kind,expected_revision,created_at`
const workflowJobColumns = `id,owner_id,owner_generation,snapshot,state,revision,result,questions,domain_ref,summary,created_at,updated_at`
const workflowAttemptColumns = `id,job_id,COALESCE(proposal_id,''),execution_id,state,fence,queued_at,started_at,deadline,finished_at,admission_kind,COALESCE(advisory_id,'')`

func scanWorkflowRequest(row scanner) (WorkflowRequest, error) {
	var r WorkflowRequest
	var at string
	err := row.Scan(&r.OwnerID, &r.OwnerGeneration, &r.Key, &r.Digest, &r.ProposalID, &r.JobID, &r.Snapshot, &r.Inputs, &r.Kind, &r.ExpectedRevision, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
	return r, err
}
func scanWorkflowJob(row scanner) (WorkflowJob, error) {
	var j WorkflowJob
	var created, updated string
	err := row.Scan(&j.ID, &j.OwnerID, &j.OwnerGeneration, &j.Snapshot, &j.State, &j.Revision, &j.Result, &j.Questions, &j.DomainRef, &j.Summary, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	j.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		j.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	return j, err
}
func scanWorkflowAttempt(row scanner) (WorkflowAttempt, error) {
	var a WorkflowAttempt
	var queued, started, deadline, finished string
	err := row.Scan(&a.ID, &a.JobID, &a.ProposalID, &a.ExecutionID, &a.State, &a.Fence, &queued, &started, &deadline, &finished, &a.AdmissionKind, &a.AdvisoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	for _, v := range []struct {
		raw string
		out *time.Time
	}{{queued, &a.QueuedAt}, {started, &a.StartedAt}, {deadline, &a.Deadline}, {finished, &a.FinishedAt}} {
		if v.raw != "" {
			*v.out, err = time.Parse(time.RFC3339Nano, v.raw)
			if err != nil {
				return a, err
			}
		}
	}
	return a, nil
}

// WorkflowRequestByKey returns the original operation for the owner generation and request key.
func (db *DB) WorkflowRequestByKey(ctx context.Context, owner string, generation int64, key string) (WorkflowRequest, error) {
	return scanWorkflowRequest(db.sql.QueryRowContext(ctx, `SELECT `+workflowRequestColumns+` FROM durable_workflow_requests WHERE owner_id=? AND owner_generation=? AND request_key=?`, owner, generation, key))
}

// ReserveWorkflowRequest does not admit work. Only confirmation creates an attempt.
func (db *DB) ReserveWorkflowRequest(ctx context.Context, r WorkflowRequest) (WorkflowRequest, error) {
	if r.OwnerID == "" || r.Key == "" || r.Digest == "" || r.JobID == "" || !json.Valid([]byte(r.Snapshot)) || !json.Valid([]byte(r.Inputs)) || r.CreatedAt.IsZero() || (r.Kind != "start" && r.Kind != "continue") {
		return WorkflowRequest{}, errors.New("invalid workflow request")
	}
	out := r
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, err := scanWorkflowRequest(tx.QueryRowContext(ctx, `SELECT `+workflowRequestColumns+` FROM durable_workflow_requests WHERE owner_id=? AND owner_generation=? AND request_key=?`, r.OwnerID, r.OwnerGeneration, r.Key))
		if err == nil {
			if old.Digest != r.Digest {
				return ErrWorkflowConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, r.ProposalID))
		if err != nil {
			return err
		}
		if p.Principal.OwnerID != r.OwnerID || p.Principal.OwnerGeneration != r.OwnerGeneration || !ownerActionPending(p.State) {
			return ErrConnectorInvalid
		}
		if r.Kind == "continue" {
			j, e := scanWorkflowJob(tx.QueryRowContext(ctx, `SELECT `+workflowJobColumns+` FROM durable_workflow_jobs WHERE id=?`, r.JobID))
			if e != nil {
				return e
			}
			if j.OwnerID != r.OwnerID || j.OwnerGeneration != r.OwnerGeneration || !workflowSameScope(j.Snapshot, r.Snapshot) || j.State != "waiting_for_input" || j.Revision != r.ExpectedRevision {
				return ErrWorkflowRevision
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_requests(`+workflowRequestColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, r.OwnerID, r.OwnerGeneration, r.Key, r.Digest, r.ProposalID, r.JobID, r.Snapshot, r.Inputs, r.Kind, r.ExpectedRevision, ownerActionTime(r.CreatedAt))
		return err
	})
	return out, err
}

// admitDurableWorkflowTx is part of the existing confirmation transaction.
func admitDurableWorkflowTx(ctx context.Context, tx *sql.Tx, p OwnerAction, at time.Time) error {
	r, err := scanWorkflowRequest(tx.QueryRowContext(ctx, `SELECT `+workflowRequestColumns+` FROM durable_workflow_requests WHERE proposal_id=?`, p.ID))
	if errors.Is(err, ErrNotFound) {
		var facts struct {
			Invocation struct {
				Command string `json:"command"`
			} `json:"invocation"`
		}
		if e := json.Unmarshal([]byte(p.Facts), &facts); e != nil {
			return e
		}
		if facts.Invocation.Command == "workflow-analysis" || facts.Invocation.Command == "workflow-author" {
			return ErrWorkflowRevision
		}
		return nil
	}
	if err != nil {
		return err
	}
	if r.Kind == "start" {
		_, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_jobs(`+workflowJobColumns+`) VALUES(?,?,?,?,'queued',0,'{}','[]','','',?,?)`, r.JobID, r.OwnerID, r.OwnerGeneration, r.Snapshot, ownerActionTime(at), ownerActionTime(at))
	} else {
		current, e := scanWorkflowJob(tx.QueryRowContext(ctx, `SELECT `+workflowJobColumns+` FROM durable_workflow_jobs WHERE id=?`, r.JobID))
		if e != nil {
			return e
		}
		if !workflowSameScope(current.Snapshot, r.Snapshot) {
			return ErrWorkflowRevision
		}
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state='queued',updated_at=?,snapshot=? WHERE id=? AND owner_id=? AND owner_generation=? AND revision=? AND state='waiting_for_input'`, ownerActionTime(at), r.Snapshot, r.JobID, r.OwnerID, r.OwnerGeneration, r.ExpectedRevision)
		if err == nil {
			var n int64
			n, err = result.RowsAffected()
			if err == nil && n != 1 {
				return ErrWorkflowRevision
			}
		}
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_attempts(job_id,proposal_id,execution_id,state,queued_at) VALUES(?,?,?,'queued',?)`, r.JobID, p.ID, p.ExecutionID, ownerActionTime(at))
	return err
}

// ReadWorkflowJob returns retained job facts without applying transport authorization.
func (db *DB) ReadWorkflowJob(ctx context.Context, id string) (WorkflowJob, error) {
	return scanWorkflowJob(db.sql.QueryRowContext(ctx, `SELECT `+workflowJobColumns+` FROM durable_workflow_jobs WHERE id=?`, id))
}

// WorkflowAttempts returns a job's attempts in admission order.
func (db *DB) WorkflowAttempts(ctx context.Context, id string) ([]WorkflowAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE job_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkflowAttempt{}
	for rows.Next() {
		a, e := scanWorkflowAttempt(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// WorkflowAttemptByExecution finds the attempt linked to an existing execution identity.
func (db *DB) WorkflowAttemptByExecution(ctx context.Context, id string) (WorkflowAttempt, error) {
	return scanWorkflowAttempt(db.sql.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE execution_id=?`, id))
}

// ClaimWorkflowAttempt grants one durable global slot to an admitted action.
// The worker rechecks current authority before adapter execution; claiming itself has no external effect.
// Expired slots require reconciliation.
func (db *DB) ClaimWorkflowAttempt(ctx context.Context, at time.Time, limit time.Duration) (WorkflowAttempt, WorkflowJob, error) {
	var a WorkflowAttempt
	var j WorkflowJob
	if limit <= 0 {
		return a, j, errors.New("invalid workflow deadline")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var active, generation int64
		if err := tx.QueryRowContext(ctx, `SELECT attempt_id,generation FROM durable_workflow_worker WHERE singleton=1`).Scan(&active, &generation); err != nil {
			return err
		}
		if active != 0 {
			return ErrNotFound
		}
		var err error
		a, err = scanWorkflowAttempt(tx.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE id=(SELECT MIN(id) FROM durable_workflow_attempts WHERE state='queued') AND (admission_kind='advisory' OR EXISTS(SELECT 1 FROM owner_action_proposals p JOIN chat_natural_executions e ON e.identity=p.execution_id WHERE p.id=durable_workflow_attempts.proposal_id AND p.state='executing' AND e.state='executing')) ORDER BY id LIMIT 1`))
		if err != nil {
			return err
		}
		a.Fence = generation + 1
		a.State = "running"
		a.StartedAt = at
		a.Deadline = at.Add(limit)
		if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_worker SET attempt_id=?,generation=?,expires_at=?,heartbeat_at=? WHERE singleton=1`, a.ID, a.Fence, ownerActionTime(a.Deadline), ownerActionTime(at)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state='running',fence=?,started_at=?,deadline=? WHERE id=?`, a.Fence, ownerActionTime(at), ownerActionTime(a.Deadline), a.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state='running',updated_at=? WHERE id=?`, ownerActionTime(at), a.JobID); err != nil {
			return err
		}
		j, err = scanWorkflowJob(tx.QueryRowContext(ctx, `SELECT `+workflowJobColumns+` FROM durable_workflow_jobs WHERE id=?`, a.JobID))
		return err
	})
	return a, j, err
}

// WorkflowFinish contains only the adapter's redacted, retained projection.
type WorkflowFinish struct {
	State, Result, Questions, DomainRef, Summary string
	Revision                                     int64
	Notice                                       *WorkflowNotice
}

// FinishWorkflowAttempt fences output and settles the existing action in one transaction.
func (db *DB) FinishWorkflowAttempt(ctx context.Context, a WorkflowAttempt, f WorkflowFinish, at time.Time) error {
	if (f.State != "succeeded" && f.State != "failed" && f.State != "uncertain" && f.State != "waiting_for_input" && (a.AdmissionKind != "advisory" || f.State != "cancelled")) || !json.Valid([]byte(f.Result)) || !json.Valid([]byte(f.Questions)) {
		return errors.New("invalid workflow result")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error { return finishWorkflowTx(ctx, tx, a, f, at, false) })
}
func finishWorkflowTx(ctx context.Context, tx *sql.Tx, a WorkflowAttempt, f WorkflowFinish, at time.Time, recovery bool) error {
	var active, generation int64
	if err := tx.QueryRowContext(ctx, `SELECT attempt_id,generation FROM durable_workflow_worker WHERE singleton=1`).Scan(&active, &generation); err != nil {
		return err
	}
	if active != a.ID || generation != a.Fence {
		return ErrWorkflowFenced
	}

	current, err := scanWorkflowAttempt(tx.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE id=?`, a.ID))
	if err != nil {
		return err
	}
	if (current.State != "running" && (current.AdmissionKind != "advisory" || current.State != "stop_requested")) || current.Fence != a.Fence || (!recovery && !at.Before(current.Deadline)) {
		return ErrWorkflowFenced
	}
	if current.AdmissionKind == "advisory" {
		return finishAdvisoryTx(ctx, tx, current, f, at)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state=?,finished_at=? WHERE id=?`, f.State, ownerActionTime(at), a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state=?,revision=?,result=?,questions=?,domain_ref=?,summary=?,updated_at=? WHERE id=?`, f.State, f.Revision, f.Result, f.Questions, f.DomainRef, f.Summary, ownerActionTime(at), current.JobID); err != nil {
		return err
	}
	actionState, journalState := "succeeded", "finished"
	if f.State == "failed" {
		actionState = "failed"
	}
	if f.State == "uncertain" {
		actionState = "uncertain"
		journalState = "uncertain"
	}
	p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, current.ProposalID))
	if err != nil {
		return err
	}
	receipt, err := json.Marshal(struct {
		State     string `json:"state"`
		Summary   string `json:"summary"`
		Reference string `json:"reference,omitempty"`
	}{f.State, f.Summary, current.JobID})
	if err != nil {
		return err
	}
	payload := "{}"
	if actionState == "uncertain" {
		payload = p.Payload
	}
	if _, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET state=?,result=?,reason=?,completed_at=?,payload=? WHERE id=?`, actionState, string(receipt), f.State, ownerActionTime(at), payload, p.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state=?,outcome=?,finished_at=?,payload=? WHERE identity=?`, journalState, string(receipt), ownerActionTime(at), payload, p.ExecutionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_worker SET attempt_id=0,expires_at='' WHERE singleton=1`); err != nil {
		return err
	}
	if f.Notice != nil {
		n := *f.Notice
		n.JobID = current.JobID
		n.Revision = f.Revision
		n.CreatedAt = at
		return insertWorkflowNoticeTx(ctx, tx, n)
	}
	return nil
}

// RunningWorkflowAttempt reads reconciliation evidence without launching work.
func (db *DB) RunningWorkflowAttempt(ctx context.Context) (WorkflowAttempt, error) {
	return scanWorkflowAttempt(db.sql.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE id=(SELECT attempt_id FROM durable_workflow_worker WHERE singleton=1)`))
}

// ReconcileWorkflowAttempt invalidates the old fence and preserves recovered evidence.
func (db *DB) ReconcileWorkflowAttempt(ctx context.Context, a WorkflowAttempt, f WorkflowFinish, at time.Time) error {
	if (f.State != "succeeded" && f.State != "failed" && f.State != "uncertain" && f.State != "waiting_for_input" && (a.AdmissionKind != "advisory" || f.State != "cancelled")) || !json.Valid([]byte(f.Result)) || !json.Valid([]byte(f.Questions)) {
		return errors.New("invalid workflow recovery")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error { return finishWorkflowTx(ctx, tx, a, f, at, true) })
}

// WorkflowRequestByProposal returns the frozen request linked to a confirmation proposal.
func (db *DB) WorkflowRequestByProposal(ctx context.Context, proposal string) (WorkflowRequest, error) {
	return scanWorkflowRequest(db.sql.QueryRowContext(ctx, `SELECT `+workflowRequestColumns+` FROM durable_workflow_requests WHERE proposal_id=?`, proposal))
}

// workflowSameScope allows new domain revision facts while freezing the selected contract and target.
func workflowSameScope(a, b string) bool {
	var left, right map[string]any
	if json.Unmarshal([]byte(a), &left) != nil || json.Unmarshal([]byte(b), &right) != nil {
		return false
	}
	if left["definition"] == nil || right["definition"] == nil {
		return a == b
	}
	return reflect.DeepEqual(left["definition"], right["definition"]) && reflect.DeepEqual(left["resolved"], right["resolved"])
}

// SetWorkflowAttemptDeadline tightens the claim before effects to its advertised domain limit.
func (db *DB) SetWorkflowAttemptDeadline(ctx context.Context, a WorkflowAttempt, deadline time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := scanWorkflowAttempt(tx.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE id=?`, a.ID))
		if err != nil {
			return err
		}
		if current.State != "running" || current.Fence != a.Fence || !deadline.After(current.StartedAt) || deadline.After(current.Deadline) {
			return ErrWorkflowFenced
		}
		var active, fence int64
		if err = tx.QueryRowContext(ctx, `SELECT attempt_id,generation FROM durable_workflow_worker WHERE singleton=1`).Scan(&active, &fence); err != nil {
			return err
		}
		if active != a.ID || fence != a.Fence {
			return ErrWorkflowFenced
		}
		if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET deadline=? WHERE id=?`, ownerActionTime(deadline), a.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE durable_workflow_worker SET expires_at=? WHERE singleton=1`, ownerActionTime(deadline))
		return err
	})
}

// workflowEffectAuthority applies the complete frozen binding and target at each domain effect.
func workflowEffectAuthority(ctx context.Context, tx *sql.Tx, p OwnerAction, at time.Time) error {
	r, err := scanWorkflowRequest(tx.QueryRowContext(ctx, `SELECT `+workflowRequestColumns+` FROM durable_workflow_requests WHERE proposal_id=?`, p.ID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	a, err := scanWorkflowAttempt(tx.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE proposal_id=? AND execution_id=?`, p.ID, p.ExecutionID))
	if err != nil {
		return err
	}
	var active, fence int64
	if err = tx.QueryRowContext(ctx, `SELECT attempt_id,generation FROM durable_workflow_worker WHERE singleton=1`).Scan(&active, &fence); err != nil {
		return err
	}
	if a.State != "running" || active != a.ID || fence != a.Fence || !at.Before(a.Deadline) {
		return ErrWorkflowFenced
	}
	var frozen struct {
		Definition struct {
			BindingProject string `json:"binding_project"`
		} `json:"definition"`
		Resolved struct {
			Project string `json:"project"`
		} `json:"resolved"`
	}
	if err = json.Unmarshal([]byte(r.Snapshot), &frozen); err != nil {
		return err
	}
	for _, ref := range []string{frozen.Definition.BindingProject, frozen.Resolved.Project} {
		record := p
		record.RepositoryID = 0
		record.RepositoryRef = ""
		if ref != "" {
			forge, slug, ok := strings.Cut(ref, ":")
			if !ok {
				forge = "github"
				slug = ref
			}
			owner, name, ok := strings.Cut(slug, "/")
			if !ok {
				return ErrConnectorScope
			}
			if err = tx.QueryRowContext(ctx, `SELECT id,forge||':'||owner||'/'||name FROM repos WHERE forge=? AND owner=? AND name=?`, forge, owner, name).Scan(&record.RepositoryID, &record.RepositoryRef); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrConnectorScope
				}
				return err
			}
		}
		if err = ownerActionAuthority(ctx, tx, p.Principal, record, at); err != nil {
			return err
		}
	}
	return nil
}

// settleQueuedWorkflowActionTx projects a refused dispatch without orphaning its admitted job.
func settleQueuedWorkflowActionTx(ctx context.Context, tx *sql.Tx, p OwnerAction, state, result string, at time.Time) error {
	if state != "failed" && state != "uncertain" {
		return nil
	}
	a, err := scanWorkflowAttempt(tx.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE proposal_id=? AND state='queued'`, p.ID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	job, err := scanWorkflowJob(tx.QueryRowContext(ctx, `SELECT `+workflowJobColumns+` FROM durable_workflow_jobs WHERE id=?`, a.JobID))
	if err != nil {
		return err
	}
	var receipt struct {
		Summary string `json:"summary"`
	}
	if err = json.Unmarshal([]byte(result), &receipt); err != nil {
		return err
	}
	var projection map[string]any
	if err = json.Unmarshal([]byte(job.Result), &projection); err != nil {
		return err
	}
	if projection == nil {
		projection = map[string]any{}
	}
	projection["state"] = state
	projection["summary"] = receipt.Summary
	raw, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state=?,finished_at=? WHERE id=? AND state='queued'`, state, ownerActionTime(at), a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state=?,revision=revision+1,result=?,summary=?,updated_at=? WHERE id=?`, state, string(raw), receipt.Summary, ownerActionTime(at), job.ID); err != nil {
		return err
	}
	return insertWorkflowNoticeTx(ctx, tx, WorkflowNotice{JobID: job.ID, Revision: job.Revision + 1, Event: "terminal", State: "unavailable", Body: receipt.Summary, CreatedAt: at})
}

// HeartbeatWorkflowAttempt records liveness without extending execution authority.
func (db *DB) HeartbeatWorkflowAttempt(ctx context.Context, a WorkflowAttempt, at time.Time) error {
	r, err := db.sql.ExecContext(ctx, `UPDATE durable_workflow_worker SET heartbeat_at=? WHERE singleton=1 AND attempt_id=? AND generation=? AND EXISTS(SELECT 1 FROM durable_workflow_attempts a WHERE a.id=attempt_id AND a.fence=generation AND a.state IN ('running','stop_requested') AND a.started_at<=? AND a.deadline>?)`, ownerActionTime(at), a.ID, a.Fence, ownerActionTime(at), ownerActionTime(at))
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrWorkflowFenced
	}
	return err
}

// WorkflowRecentAttempts reads at most limit recent attempts, in chronological order.
func (db *DB) WorkflowRecentAttempts(ctx context.Context, job string, limit int) ([]WorkflowAttempt, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid workflow history limit")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+workflowAttemptColumns+` FROM (SELECT * FROM durable_workflow_attempts WHERE job_id=? ORDER BY id DESC LIMIT ?) ORDER BY id`, job, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkflowAttempt{}
	for rows.Next() {
		a, e := scanWorkflowAttempt(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// WorkflowAttemptAtRevision finds the attempt that produced a terminal job revision.
func (db *DB) WorkflowAttemptAtRevision(ctx context.Context, job string, revision int64) (WorkflowAttempt, error) {
	if revision < 1 {
		return WorkflowAttempt{}, ErrNotFound
	}
	return scanWorkflowAttempt(db.sql.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE job_id=? ORDER BY id LIMIT 1 OFFSET ?`, job, revision-1))
}
