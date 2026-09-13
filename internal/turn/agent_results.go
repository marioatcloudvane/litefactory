package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// AgentResultServiceOptions supplies only factory-owned binding, grants and a
// fresh evidence resolver. This direct service is deliberately not runner wiring.
type AgentResultServiceOptions struct {
	DB                       *store.DB
	Attempt                  store.ResultAttempt
	Resolve                  func(context.Context) (agentresult.Context, error)
	ReadTargets, EditTargets []agentresult.Target
}

// AgentResultService composes pure result validation with atomic storage. No
// method dispatches runners, captures proof, changes readiness or publishes.
type AgentResultService struct {
	db                       *store.DB
	binding                  store.ResultBinding
	attempt                  store.ResultAttempt
	resolve                  func(context.Context) (agentresult.Context, error)
	readTargets, editTargets []agentresult.Target
	readOnly                 bool
}

// AgentResultSubmission separates validation/input acceptance from engineering,
// acceptance and publication observations. A receipt is not a workflow decision.
type AgentResultSubmission struct {
	Validation agentresult.UpdateDiagnostics
	Receipt    *store.ResultReceipt
	Snapshot   *agentresult.Snapshot
}

// BindAgentResultService starts fresh authority for one real factory run. Exact
// frozen revision identity is derived from trusted context, never authored data.
func BindAgentResultService(ctx context.Context, o AgentResultServiceOptions) (*AgentResultService, error) {
	if o.DB == nil || o.Resolve == nil {
		return nil, fmt.Errorf("agent result service requires store and trusted resolver")
	}
	contextValue, err := o.Resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve application result context: %w", err)
	}
	a, err := resultAttemptContext(o.Attempt, contextValue)
	if err != nil {
		return nil, err
	}
	binding, err := o.DB.BindResultAttempt(ctx, a)
	if err != nil {
		return nil, err
	}
	return &AgentResultService{db: o.DB, binding: binding, attempt: a, resolve: o.Resolve, readTargets: slices.Clone(o.ReadTargets), editTargets: slices.Clone(o.EditTargets)}, nil
}

// OpenAgentResultReader reconnects an explicitly read-only service after restart.
// Historical attribution grants receipt/projection access, never new submissions.
func OpenAgentResultReader(ctx context.Context, db *store.DB, b store.ResultBinding, targets []agentresult.Target) (*AgentResultService, error) {
	if db == nil {
		return nil, fmt.Errorf("agent result reader requires store")
	}
	if err := db.CheckResultReader(ctx, b); err != nil {
		return nil, err
	}
	return &AgentResultService{db: db, binding: b, readTargets: slices.Clone(targets), readOnly: true}, nil
}

func resultAttemptContext(a store.ResultAttempt, c agentresult.Context) (store.ResultAttempt, error) {
	if a.Mode != c.Mode || a.SchemaVersion != c.SchemaVersion || a.WorkflowVersion != c.WorkflowVersion {
		return a, fmt.Errorf("application context conflicts with trusted run binding")
	}
	a.FrozenStories = []store.ResultStoryRevision{}
	if a.Mode == "implement" || a.Mode == "validate" {
		for key, story := range c.Stories {
			a.FrozenStories = append(a.FrozenStories, store.ResultStoryRevision{Story: key, Revision: story.Revision})
		}
	}
	return a, nil
}

// Binding returns copied factory attribution, not a capability or workflow state.
func (s *AgentResultService) Binding() store.ResultBinding { return s.binding }

// Read returns an explicitly scoped historical or latest (revision zero) baseline.
func (s *AgentResultService) Read(ctx context.Context, revision int64, selections []agentresult.Target) (agentresult.Projection, agentresult.Diagnostics, error) {
	retained, err := s.db.GetResultRevision(ctx, s.binding, revision)
	if err != nil {
		return agentresult.Projection{}, agentresult.Diagnostics{}, err
	}
	snapshot, _, err := decodeRetainedResult(retained)
	if err != nil {
		return agentresult.Projection{}, agentresult.Diagnostics{}, err
	}
	p, d := agentresult.Project(snapshot, selections, s.readTargets, agentresult.Context{Mode: s.binding.Mode, SchemaVersion: s.binding.SchemaVersion})
	return p, d, nil
}
func decodeRetainedResult(r store.ResultRevision) (agentresult.Snapshot, agentresult.RetiredIDs, error) {
	var snapshot agentresult.Snapshot
	var retired agentresult.RetiredIDs
	if err := json.Unmarshal(r.Snapshot, &snapshot); err != nil {
		return snapshot, nil, fmt.Errorf("decode retained result: %w", err)
	}
	if err := json.Unmarshal(r.Retired, &retired); err != nil {
		return snapshot, nil, fmt.Errorf("decode retained retirement history: %w", err)
	}
	return snapshot, retired, nil
}
func (s *AgentResultService) trustedContext(ctx context.Context) (agentresult.Context, error) {
	if s.readOnly {
		return agentresult.Context{}, store.ErrResultAuthority
	}
	if err := s.db.CheckResultWriter(ctx, s.binding); err != nil {
		return agentresult.Context{}, err
	}
	c, err := s.resolve(ctx)
	if err != nil {
		return c, fmt.Errorf("resolve current result evidence: %w", err)
	}
	a, err := resultAttemptContext(s.attempt, c)
	if err != nil {
		return c, err
	}
	hash, err := store.ResultContextHash(a)
	if err != nil {
		return c, err
	}
	if hash != s.binding.ContextHash {
		return c, store.ErrResultAuthority
	}
	return c, nil
}
func (s *AgentResultService) prepare(ctx context.Context, data []byte) (agentresult.PreparedUpdate, agentresult.UpdateDiagnostics, error) {
	c, err := s.trustedContext(ctx)
	if err != nil {
		return agentresult.PreparedUpdate{}, agentresult.UpdateDiagnostics{}, err
	}
	retained, err := s.db.GetResultRevision(ctx, s.binding, 0)
	if errors.Is(err, store.ErrNotFound) {
		r, d := agentresult.Create(data, c)
		v := agentresult.UpdateDiagnostics{Diagnostics: d}
		if !d.Valid {
			return agentresult.PreparedUpdate{}, v, nil
		}
		snapshot, d := agentresult.Assemble(r, s.metadata(1), c)
		v.Diagnostics = d
		return agentresult.PreparedUpdate{Snapshot: snapshot, Retired: agentresult.RetiredIDs{}}, v, nil
	}
	if err != nil {
		return agentresult.PreparedUpdate{}, agentresult.UpdateDiagnostics{}, err
	}
	snapshot, retired, err := decodeRetainedResult(retained)
	if err != nil {
		return agentresult.PreparedUpdate{}, agentresult.UpdateDiagnostics{}, err
	}
	input, d := agentresult.DecodeUpdate(data)
	v := agentresult.UpdateDiagnostics{Diagnostics: d, ExpectedRevision: input.ExpectedRevision, CurrentRevision: retained.Revision}
	if !d.Valid {
		return agentresult.PreparedUpdate{}, v, nil
	}
	candidate, v := agentresult.ApplyUpdate(snapshot, retired, input, c, s.editTargets)
	if v.Valid {
		candidate.Snapshot.Meta = s.metadata(candidate.Snapshot.Meta.ResultRevision)
	}
	return candidate, v, nil
}
func (s *AgentResultService) metadata(revision int64) agentresult.Metadata {
	return agentresult.Metadata{SchemaVersion: s.binding.SchemaVersion, Task: s.binding.Task, RunID: s.binding.RunID, AttemptID: s.binding.Attempt, Mode: s.binding.Mode, ResultID: fmt.Sprintf("result-%d", s.binding.ResultID), ResultRevision: revision, ScopeRef: s.binding.ContextHash, WorkflowVersion: s.binding.WorkflowVersion, SubmissionContractVersion: s.binding.SubmissionVersion}
}

// Preflight performs the same fresh scope/evidence reads as Submit without
// allocating a request, proof, revision or durable observation.
func (s *AgentResultService) Preflight(ctx context.Context, data []byte) (agentresult.UpdateDiagnostics, error) {
	_, d, err := s.prepare(ctx, data)
	return d, err
}

// Submit revalidates and records one request. Exact correlation retry recovers
// its original application before current-revision validation or writer checks.
func (s *AgentResultService) Submit(ctx context.Context, requestID string, data []byte) (AgentResultSubmission, error) {
	if len(data) > agentresult.MaxBytes {
		return AgentResultSubmission{}, fmt.Errorf("application result input exceeds contribution ceiling")
	}
	old, err := s.db.ReplayResultRequest(ctx, s.binding, requestID, data)
	if err == nil {
		return s.response(ctx, old)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return AgentResultSubmission{}, err
	}
	candidate, d, err := s.prepare(ctx, data)
	if err != nil {
		return AgentResultSubmission{}, err
	}
	// A stale update gets actionable concurrency diagnostics and no durable write.
	for _, fault := range d.Diagnostics.Diagnostics {
		if fault.Code == "stale_revision" {
			return AgentResultSubmission{Validation: d}, nil
		}
	}
	diagnostics, err := json.Marshal(d)
	if err != nil {
		return AgentResultSubmission{}, fmt.Errorf("encode result diagnostics: %w", err)
	}
	q := store.ResultRecord{Binding: s.binding, RequestID: requestID, Payload: slices.Clone(data), Diagnostics: diagnostics, ExpectedRevision: d.CurrentRevision, Accepted: d.Valid}
	if d.Valid {
		candidate.Snapshot.Meta.RequestID = requestID
		q.Snapshot, err = json.Marshal(candidate.Snapshot)
		if err != nil {
			return AgentResultSubmission{}, fmt.Errorf("encode result snapshot: %w", err)
		}
		q.Retired, err = json.Marshal(candidate.Retired)
		if err != nil {
			return AgentResultSubmission{}, fmt.Errorf("encode result retirement: %w", err)
		}
	}
	receipt, err := s.db.RecordResult(ctx, q)
	if err != nil {
		return AgentResultSubmission{}, err
	}
	return s.response(ctx, receipt)
}
func (s *AgentResultService) response(ctx context.Context, r store.ResultReceipt) (AgentResultSubmission, error) {
	response := AgentResultSubmission{Receipt: &r}
	if err := json.Unmarshal(r.Diagnostics, &response.Validation); err != nil {
		return response, fmt.Errorf("decode result receipt diagnostics: %w", err)
	}
	if r.AppliedRevision != 0 {
		retained, err := s.db.GetResultRevision(ctx, s.binding, r.AppliedRevision)
		if err != nil {
			return response, err
		}
		snapshot, _, err := decodeRetainedResult(retained)
		if err != nil {
			return response, err
		}
		response.Snapshot = &snapshot
	}
	return response, nil
}

// Receipt observes authorized historical correlation without granting writing.
func (s *AgentResultService) Receipt(ctx context.Context, sourceRunID int64, requestID string) (AgentResultSubmission, error) {
	r, err := s.db.GetResultReceipt(ctx, s.binding, sourceRunID, requestID)
	if err != nil {
		return AgentResultSubmission{}, err
	}
	return s.response(ctx, r)
}

// Observe is factory-only bookkeeping of separately established facts. It has
// no authored serialization surface and cannot mutate workflow or remote state.
func (s *AgentResultService) Observe(ctx context.Context, sourceRunID int64, requestID, dimension, state string) (store.ResultReceipt, error) {
	if s.readOnly {
		return store.ResultReceipt{}, store.ErrResultAuthority
	}
	return s.db.ObserveResult(ctx, s.binding, sourceRunID, requestID, dimension, state)
}
