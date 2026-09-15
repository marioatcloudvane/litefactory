package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// MCPRuntime is composed once by the runtime owner. A nil value disables automatic
// registration; it never creates authority while preparing an individual turn.
// Now belongs to the daemon and only bounds per-launch leases and activity.
type MCPRuntime struct {
	Authority  *AttemptAuthority
	Paths      config.Paths
	Executable string
	Now        func() time.Time
	Issues     IssueQueryForge
}

// Validate rejects incomplete startup-owned dependencies before allocating a launch.
func (r *MCPRuntime) Validate() error {
	if r == nil {
		return nil
	}
	if r.Authority == nil || r.Now == nil || r.Paths.Home() == "" || !filepath.IsAbs(r.Executable) {
		return errors.New("MCP runtime dependencies missing")
	}
	return nil
}

type preparedMCP struct {
	runtime *MCPRuntime
	service *AgentResultService
	guard   *AttemptGuard
	broker  *submission.Broker
}

func (p *preparedMCP) close(ctx context.Context) error {
	var revokeErr, brokerErr error
	if p.guard != nil {
		revokeErr = p.guard.Revoke(ctx, "closed")
	}
	if p.broker != nil {
		brokerErr = p.broker.Close()
	}
	return errors.Join(revokeErr, brokerErr, p.runtime.Authority.db.EndResultAttempt(ctx, p.service.Binding()))
}

// runMCPBound is called for every actual invocation. Deferred providers preserve
// their old path until they explicitly advertise native registration support.
func runMCPBound(ctx context.Context, prep *preparation, req agent.Request, session *SubmissionSession) (result *agent.RunResult, err error) {
	runtime := prep.mcpRuntime
	if runtime == nil {
		return prep.runner.Run(ctx, req)
	}
	supported, ok := prep.runner.(interface{ SupportsMCP() bool })
	if !ok || !supported.SupportsMCP() {
		return prep.runner.Run(ctx, req)
	}
	if err := runtime.Validate(); err != nil {
		return nil, err
	}
	if req.MCPLaunch != nil {
		return nil, errors.New("MCP registration already supplied")
	}
	// Legacy/unmanaged engineering has no checkpoint owner. It must retain the
	// legacy tool behavior rather than inventing managed story identities.
	if (req.Mode == "implement" || req.Mode == "validate") && session == nil {
		return prep.runner.Run(ctx, req)
	}
	p, spec, err := prepareMCP(ctx, prep, req, session)
	if err != nil {
		return nil, fmt.Errorf("prepare MCP launch: %w", err)
	}
	defer func() { err = errors.Join(err, p.close(context.WithoutCancel(ctx))) }()
	req.MCPLaunch = spec
	guidance, err := mcpGuidance(ctx, spec, p.service, session)
	if err != nil {
		return nil, fmt.Errorf("prepare MCP guidance: %w", err)
	}
	req.Prompt += guidance
	return prep.runner.Run(ctx, req)
}

func prepareMCP(ctx context.Context, prep *preparation, req agent.Request, session *SubmissionSession) (_ *preparedMCP, _ *agent.MCPLaunchSpec, err error) {
	runtime := prep.mcpRuntime
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, err
	}
	attempt := hex.EncodeToString(nonce[:])
	workflow := store.WorkflowSeparated
	if session != nil {
		attempt = session.binding.Attempt
		workflow = session.scope.WorkflowVersion
	}
	workContext := prep.session.FeatureSlug
	if workContext == "" {
		workContext = prep.ref.String()
	}
	contextValue := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: req.Mode, WorkflowVersion: workflow, QuestionsAllowed: prep.promptInput.MayAsk, RequireAggregate: req.Mode == "implement", Stories: map[string]agent.ContributionStory{}, EngineeringComplete: map[string]bool{}}
	if session != nil {
		for key, story := range session.scope.Stories {
			story.Criteria = slices.Clone(story.Criteria)
			story.ExecutionRequired = slices.Clone(story.ExecutionRequired)
			story.Checks = maps.Clone(story.Checks)
			contextValue.Stories[key] = story
		}
	}
	o := AgentResultServiceOptions{DB: runtime.Authority.db, Attempt: store.ResultAttempt{Task: prep.ref.String(), Mode: req.Mode, WorkContext: workContext, Attempt: attempt, RunID: prep.run.ID, SchemaVersion: agentresult.SchemaVersion, WorkflowVersion: workflow, SubmissionVersion: 2}, Resolve: func(check context.Context) (agentresult.Context, error) {
		if err := check.Err(); err != nil {
			return agentresult.Context{}, err
		}
		if prep.validateScope != nil && len(contextValue.Stories) > 0 {
			reason, err := prep.validateScope(check)
			if err != nil {
				return agentresult.Context{}, err
			}
			if reason != "" {
				return agentresult.Context{}, store.ErrResultAuthority
			}
		}
		return contextValue, nil
	}, ReadTargets: agentresult.EditTargets(req.Mode), EditTargets: agentresult.EditTargets(req.Mode)}
	service, err := BindAgentResultService(ctx, o)
	if err != nil {
		return nil, nil, err
	}
	p := &preparedMCP{runtime: runtime, service: service}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.close(context.WithoutCancel(ctx)))
		}
	}()
	projections := map[string]FamilyProjection{"result": ResultReceiptProjection(o.DB, service.Binding())}
	if session != nil && (req.Mode == "implement" || req.Mode == "validate") {
		projections["submission"] = SubmissionReceiptProjection(o.DB)
		projections["tool:evidence_submit"] = EvidenceReceiptProjection(o.DB)
	}
	receipts, err := NewReceiptService(o.DB, service.Binding(), nil, projections)
	if err != nil {
		return nil, nil, err
	}
	var evidence *EvidenceService
	if req.Mode == "implement" || req.Mode == "validate" {
		evidence, err = NewEvidenceService(session, receipts)
		if err != nil {
			return nil, nil, err
		}
	}
	feedback, err := NewFeedbackService(service, evidence)
	if err != nil {
		return nil, nil, err
	}
	families := FamilyDispatcherOptions{Service: service, Receipts: receipts, Feedback: feedback, Evidence: evidence}
	now := runtime.Now()
	activity, err := NewActivityService(ActivityOptions{Receipts: receipts, Now: now})
	if err != nil {
		return nil, nil, err
	}
	families.Registrations = append(families.Registrations, FamilyRegistration{Name: "activity_update", Owner: activity, Handle: activity.HandleUpdate})
	if runtime.Issues != nil {
		issues, err := NewIssueQueryService(receipts, runtime.Issues)
		if err != nil {
			return nil, nil, err
		}
		families.Registrations = append(families.Registrations, issues.Registrations()...)
	}
	digest, err := FamilyPermissionDigest(families)
	if err != nil {
		return nil, nil, err
	}
	expires := now.Add(24 * time.Hour)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expires) {
		expires = deadline
	}
	p.guard, err = runtime.Authority.Bind(ctx, service.Binding(), digest, expires)
	if err != nil {
		return nil, nil, err
	}
	dispatcher, err := NewFamilyToolDispatcher(p.guard, families)
	if err != nil {
		return nil, nil, err
	}
	dir := runtime.Paths.SubmissionDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	p.broker, err = submission.Start(ctx, submission.Options{Directory: dir, AllowTools: true, AttemptReference: attempt, MaxWorkers: 4, RequestTimeout: 30 * time.Second, Handler: dispatcher.Handler})
	if err != nil {
		return nil, nil, err
	}
	spec, err := BuildMCPLaunchSpec(ctx, true, runtime.Executable, p.broker, p.guard)
	if err != nil {
		return nil, nil, err
	}
	installed := dispatcher.profile.List()
	names := make([]string, len(installed))
	for i, tool := range installed {
		names[i] = tool.Name
	}
	if !slices.Equal(spec.PermittedTools(), names) {
		return nil, nil, errors.New("MCP installed catalog changed")
	}
	return p, spec, nil
}
