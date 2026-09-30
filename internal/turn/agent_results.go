package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/resultservice"
	"github.com/cloudvane/litefactory/internal/store"
)

// AgentResultServiceOptions supplies only factory-owned binding, grants and a
// fresh evidence resolver. This direct service is deliberately not runner wiring.
type AgentResultServiceOptions struct {
	DB                       *store.DB
	Attempt                  store.ResultAttempt
	Resolve                  func(context.Context) (agentresult.Context, error)
	ReadTargets, EditTargets []agentresult.Target
	PrepareAggregate         func(context.Context, store.ResultBinding, agent.EngineeringContribution, agent.AggregateCapture) (store.ResultAggregate, error)
	ValidateCompletion       func(context.Context, store.ResultBinding, agentresult.Result, agentresult.Context) agentresult.Diagnostics
}

// AgentResultService composes pure result validation with atomic storage. No
// method dispatches runners, executes checks, changes readiness or publishes.
// Aggregate capture retains only explicitly reported immutable evidence.
type AgentResultService struct {
	db                       *store.DB
	binding                  store.ResultBinding
	attempt                  store.ResultAttempt
	resolve                  func(context.Context) (agentresult.Context, error)
	readTargets, editTargets []agentresult.Target
	readOnly                 bool
	prepareAggregate         func(context.Context, store.ResultBinding, agent.EngineeringContribution, agent.AggregateCapture) (store.ResultAggregate, error)
	validateCompletion       func(context.Context, store.ResultBinding, agentresult.Result, agentresult.Context) agentresult.Diagnostics
}

// AgentResultSubmission separates validation/input acceptance from engineering,
// acceptance and publication observations. A receipt is not a workflow decision.
type AgentResultSubmission struct {
	Validation agentresult.UpdateDiagnostics
	Receipt    *store.ResultReceipt
	Snapshot   *agentresult.Snapshot
	Baseline   *agentresult.Projection
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
	return &AgentResultService{db: o.DB, binding: binding, attempt: a, resolve: o.Resolve, readTargets: slices.Clone(o.ReadTargets), editTargets: slices.Clone(o.EditTargets), validateCompletion: o.ValidateCompletion, prepareAggregate: o.PrepareAggregate}, nil
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
	if c.Workflow != "" {
		if _, err := agent.ResolveWorkflow(c.Workflow, agent.Mode(c.Mode)); err != nil {
			return a, fmt.Errorf("application workflow binding: %w", err)
		}
		if c.Workflow == agent.WorkflowBug && (c.Bug == nil || (c.Bug.Role != "bug-analyst" && c.Bug.Role != "bug-fixer") || c.Bug.Invocation == "" || a.Workflow != "bug") {
			return a, fmt.Errorf("bug shared-service result requires exact role workflow and invocation binding")
		}
	}
	if c.Workflow == agent.WorkflowBug && c.Bug != nil && c.Bug.Role == "bug-fixer" {
		b := c.Bug
		if b.Reservation == "" || b.Invocation != b.Reservation || b.ApprovedRevision <= 0 || b.ApprovedRevision != b.BaseRevision || b.ApprovedDigest == "" || b.ApprovalReview == "" || b.SourceCommit == "" || b.SourceTree == "" || b.CriteriaJSON == "" {
			return a, fmt.Errorf("bug fixer shared-service context lacks exact approved repair binding")
		}
	}
	if a.Workflow != "" && a.Workflow != string(c.Workflow) {
		return a, fmt.Errorf("application workflow differs from trusted run")
	}
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
	p, d := agentresult.Project(snapshot, selections, s.readTargets, agentresult.Context{Workflow: agent.Workflow(s.binding.Workflow), Mode: s.binding.Mode, SchemaVersion: s.binding.SchemaVersion})
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
	c = cloneResultContext(c)
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
func (s *AgentResultService) prepare(ctx context.Context, data []byte) (candidate agentresult.PreparedUpdate, diagnostics agentresult.UpdateDiagnostics, resultErr error) {
	defer func() {
		if resultErr == nil && !diagnostics.Valid {
			baseline, err := s.Baseline(ctx)
			if err != nil {
				resultErr = err
				return
			}
			diagnostics.CurrentRevision = baseline.Projection.Revision
			diagnostics.Baseline = &baseline.Projection
		}
	}()
	defer func() {
		if resultErr == nil && diagnostics.Valid && s.validateCompletion != nil {
			c, err := s.trustedContext(ctx)
			if err != nil {
				resultErr = err
				return
			}
			d := s.validateCompletion(ctx, s.binding, candidate.Snapshot.Result, c)
			diagnostics.Valid = diagnostics.Valid && d.Valid
			diagnostics.Complete = diagnostics.Complete && d.Complete
			diagnostics.Diagnostics.Diagnostics = append(diagnostics.Diagnostics.Diagnostics, d.Diagnostics...)
			diagnostics.UncheckedPaths = append(diagnostics.UncheckedPaths, d.UncheckedPaths...)
		}
	}()

	// MCP cannot initiate deferred issue creation. Refuse the fully assembled
	// candidate, including updates to retained proposals, before durable admission.
	// Ordinary helper/terminal callers retain the existing publication policy.
	defer func() {
		restricted, _ := ctx.Value(mcpCommunicationKey{}).(bool)
		plan := candidate.Snapshot.Result.Payload.Plan
		if restricted && diagnostics.Valid && creationCapablePlan(plan, candidate.Snapshot.Result.Outcome) {
			diagnostics.Valid = false
			diagnostics.Diagnostics.Diagnostics = append(diagnostics.Diagnostics.Diagnostics, agentresult.Diagnostic{Path: "$.result.payload.plan", Code: "mcp_creation_refused", Expected: "discussion without publishable stories", Actual: "creation-capable Plan", Message: "MCP cannot submit a publishable Plan; use the owner-controlled Plan workflow. No proposal was accepted."})
			candidate = agentresult.PreparedUpdate{}
		}
	}()
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
	candidate, v = agentresult.ApplyUpdate(snapshot, retired, input, c, s.editTargets)
	if v.Valid {
		candidate.Snapshot.Meta = s.metadata(candidate.Snapshot.Meta.ResultRevision)
	}
	return candidate, v, nil
}

// creationCapablePlan reports whether a Plan payload could publish stories on its
// own. A graph Plan's story request or story set is not one: it is a proposal the
// factory-bound owner path alone promotes, and only a graph-bound run can carry it,
// because validation refuses those branches under the legacy Plan contract.
func creationCapablePlan(plan *agentresult.Plan, outcome string) bool {
	if plan == nil || plan.StoryRequest != nil || plan.StorySet != nil {
		return false
	}
	return outcome == "completed" || len(plan.Stories) > 0
}

func (s *AgentResultService) metadata(revision int64) agentresult.Metadata {
	return agentresult.Metadata{Workflow: agent.Workflow(s.binding.Workflow), SchemaVersion: s.binding.SchemaVersion, Task: s.binding.Task, RunID: s.binding.RunID, AttemptID: s.binding.Attempt, Mode: s.binding.Mode, ResultID: fmt.Sprintf("result-%d", s.binding.ResultID), ResultRevision: revision, ScopeRef: s.binding.ContextHash, WorkflowVersion: s.binding.WorkflowVersion, SubmissionContractVersion: s.binding.SubmissionVersion}
}

// Preflight performs the same fresh scope/evidence reads as Submit without
// allocating a request, proof, revision or durable observation.
func (s *AgentResultService) Preflight(ctx context.Context, data []byte) (agentresult.UpdateDiagnostics, error) {
	if aggregateInput(data) {
		_, d, err := s.prepareAggregateInput(ctx, data)
		return d, err
	}
	_, d, err := s.prepare(ctx, data)
	return d, err
}

// Submit revalidates and records one request. Exact correlation retry recovers
// its original application before current-revision validation or writer checks.
func (s *AgentResultService) Submit(ctx context.Context, requestID string, data []byte) (AgentResultSubmission, error) {
	if len(data) > agentresult.MaxBytes {
		return AgentResultSubmission{}, fmt.Errorf("application result input exceeds contribution ceiling")
	}

	// Aggregate capture retains its separate protocol; normal application results
	// share replay-before-validation mechanics with the doctor service.
	if aggregateInput(data) {
		if old, e := s.db.ReplayResultRequest(ctx, s.binding, requestID, data); e == nil {
			return s.response(ctx, old)
		} else if !errors.Is(e, store.ErrNotFound) {
			return AgentResultSubmission{}, e
		}
		return s.submitAggregateInput(ctx, requestID, data)
	}
	var d agentresult.UpdateDiagnostics
	receipt, replayed, err := resultservice.Submit(ctx, s.db, s.binding, requestID, data, func(ctx context.Context) (*store.ResultRecord, error) {
		candidate, diagnostics, e := s.prepare(ctx, data)
		d = diagnostics
		if e != nil {
			return nil, e
		}
		for _, fault := range d.Diagnostics.Diagnostics {
			if fault.Code == "stale_revision" {
				return nil, nil
			}
		}
		encoded, e := json.Marshal(d)
		if e != nil {
			return nil, fmt.Errorf("encode result diagnostics: %w", e)
		}
		q := store.ResultRecord{Diagnostics: encoded, ExpectedRevision: d.CurrentRevision, Accepted: d.Valid}
		if d.Valid {
			candidate.Snapshot.Meta.RequestID = requestID
			q.Snapshot, e = json.Marshal(candidate.Snapshot)
			if e != nil {
				return nil, fmt.Errorf("encode result snapshot: %w", e)
			}
			q.Retired, e = json.Marshal(candidate.Retired)
			if e != nil {
				return nil, fmt.Errorf("encode result retirement: %w", e)
			}
		}
		return &q, nil
	})
	if replayed {
		response, responseErr := s.response(ctx, *receipt)
		if responseErr != nil {
			return response, responseErr
		}
		restricted, _ := ctx.Value(mcpCommunicationKey{}).(bool)
		if restricted && response.Snapshot != nil && creationCapablePlan(response.Snapshot.Result.Payload.Plan, response.Snapshot.Result.Outcome) {
			return AgentResultSubmission{}, fmt.Errorf("MCP cannot replay a publishable Plan; use receipt lookup")
		}
		return response, nil
	}

	var conflict *store.ResultRevisionConflict
	if errors.As(err, &conflict) {
		baseline, readErr := s.Baseline(ctx)
		if readErr != nil {
			return AgentResultSubmission{}, readErr
		}
		d.Valid = false
		d.CurrentRevision = baseline.Projection.Revision
		d.Baseline = &baseline.Projection
		d.Diagnostics.Diagnostics = append(d.Diagnostics.Diagnostics, agentresult.Diagnostic{Path: "$.expected_revision", Code: "stale_revision", Message: "Use the current authorized baseline and a new request identity."})
		return AgentResultSubmission{Validation: d}, nil
	}
	if err != nil {
		return AgentResultSubmission{}, err
	}
	if receipt == nil {
		return AgentResultSubmission{Validation: d}, nil
	}
	return s.response(ctx, *receipt)
}
func (s *AgentResultService) response(ctx context.Context, r store.ResultReceipt) (AgentResultSubmission, error) {
	response := AgentResultSubmission{Receipt: &r}
	if !s.completeReadGrant() {
		public := r
		public.Payload, public.Diagnostics = nil, nil
		response.Receipt = &public
	}
	if err := json.Unmarshal(r.Diagnostics, &response.Validation); err != nil {
		return response, fmt.Errorf("decode result receipt diagnostics: %w", err)
	}
	if !s.completeReadGrant() {
		response.Validation.Baseline = nil
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
		if s.completeReadGrant() {
			response.Snapshot = &snapshot
		} else {
			projection, d := agentresult.Project(snapshot, resultBaselineTargets(s.readTargets), s.readTargets, agentresult.Context{Workflow: agent.Workflow(s.binding.Workflow), Mode: s.binding.Mode, SchemaVersion: s.binding.SchemaVersion})
			if !d.Valid || !d.Complete {
				return response, resultDiagnosticError(d)
			}
			response.Baseline = &projection
		}
	}
	return response, nil
}

// errNoRetainedResult means this attempt has no accepted shared result. A terminal
// envelope, however well formed, is not one.
var errNoRetainedResult = errors.New("no accepted shared result was retained for this attempt")

// retainedCurrent reads the latest accepted revision and requires it to be this
// attempt's own. It is the only source of application data for a graph node: an
// earlier attempt's revision is a baseline to update, never this attempt's input,
// and nothing the runner printed can stand in for it.
func (s *AgentResultService) retainedCurrent(ctx context.Context) (agentresult.Snapshot, int64, error) {
	retained, err := s.db.GetResultRevision(ctx, s.binding, 0)
	if errors.Is(err, store.ErrNotFound) {
		return agentresult.Snapshot{}, 0, errNoRetainedResult
	}
	if err != nil {
		return agentresult.Snapshot{}, 0, fmt.Errorf("read retained result: %w", err)
	}
	snapshot, _, err := decodeRetainedResult(retained)
	if err != nil {
		return snapshot, 0, err
	}
	if snapshot.Meta.AttemptID != s.binding.Attempt || snapshot.Meta.RunID != s.binding.RunID {
		return agentresult.Snapshot{}, 0, errNoRetainedResult
	}
	return snapshot, retained.Revision, nil
}

// Receipt observes authorized historical correlation without granting writing.
func (s *AgentResultService) Receipt(ctx context.Context, sourceRunID int64, requestID string) (AgentResultSubmission, error) {
	r, err := s.db.GetResultReceipt(ctx, s.binding, sourceRunID, requestID)
	if errors.Is(err, store.ErrNotFound) && sourceRunID == s.binding.RunID {
		r, err = s.db.FindResultReceipt(ctx, s.binding, requestID)
	}
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
