package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// ErrJudgeSettings identifies a visible admission refusal, not an infrastructure failure.
var ErrJudgeSettings = errors.New("judge settings cannot be used")

var errJudgePersistence = errors.New("judge durable bookkeeping failed")

// JudgeService runs advisory challenges independently of ordinary workflow execution.
// Each call uses its caller's existing worker capacity, cancellation and task lock.
type JudgeService struct {
	Logger     *slog.Logger
	DB         *store.DB
	Config     *config.Config
	Providers  map[agent.Kind]agent.JudgeRunner
	Paths      config.Paths
	Executable string
	Git        *git.Binary
	Redactor   *redact.Redactor
}

// JudgeRequest is factory-authenticated command context. It grants no workflow writes.
type JudgeRequest struct {
	Ref                        naming.Ref
	SessionID, CommentID       int64
	TicketKey, RemoteCommentID string
	Root                       string
	TaskProvider               config.Kind
	Options                    JudgeCommandOptions
	Comments                   []JudgeComment
	Cutoff                     time.Time
	// Workflow fields are factory-pinned and never accepted from command text.
	GraphInstanceID, JudgeNodeAttemptID, SourceNodeAttemptID int64
	SourceNode                                               string
	PinnedSource                                             *store.JudgeSource
	Instructions                                             *instructionpackage.RoleCapture
	Budget                                                   time.Duration
	RecoverOnly                                              bool
}

type judgeConfiguration struct {
	ObservedModel     string                 `json:"observed_model,omitempty"`
	ProviderVersion   string                 `json:"provider_version,omitempty"`
	Requested         JudgeCommandOptions    `json:"requested"`
	Effective         config.JudgeInvocation `json:"effective"`
	Role              string                 `json:"role"`
	RoleDigest        string                 `json:"role_digest"`
	Instructions      json.RawMessage        `json:"instructions,omitempty"`
	InstructionDigest string                 `json:"instruction_digest,omitempty"`
}

type judgeSavedReport struct {
	Presentation JudgePresentation `json:"presentation"`
	Execution    string            `json:"execution"`
}

// Execute pins source, settings, discussion and commit before any provider launch.
// Saved reports and interrupted launches are recovered without rerunning an assessment.
func (s *JudgeService) Execute(ctx context.Context, in JudgeRequest) (text string, returnedErr error) {
	if s.DB == nil || s.Config == nil || s.Redactor == nil || in.SessionID < 1 || (in.CommentID < 1 && in.GraphInstanceID == 0) || in.TicketKey == "" || in.RemoteCommentID == "" {
		return "", errors.New("judge requires authenticated ticket context and configured dependencies")
	}
	if err := in.Ref.Validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Paths.StoryProgressLocksDir(), 0700); err != nil {
		return "", err
	}
	release, err := s.DB.LockJudgeRequest(ctx, in.TicketKey, in.RemoteCommentID, s.Paths.StoryProgressLocksDir())
	if err != nil {
		return "", err
	}
	defer func() { returnedErr = errors.Join(returnedErr, release()) }()
	q, err := s.DB.FindJudgeRequest(ctx, in.TicketKey, in.RemoteCommentID)
	if errors.Is(err, store.ErrNotFound) {
		var source store.JudgeSource
		var e error
		if in.PinnedSource != nil && in.GraphInstanceID > 0 {
			source = *in.PinnedSource
		} else {
			source, e = s.DB.ResolveJudgeSource(ctx, in.Ref.String(), in.Options.Target)
		}
		if e != nil {
			return "", e
		}
		invocation, e := s.Config.ResolveJudge(in.Options.JudgeOptions, in.TaskProvider, config.Kind(source.Provider))
		if e != nil {
			return "", fmt.Errorf("%w: %w", ErrJudgeSettings, e)
		}
		cfg, e := s.captureJudgeConfiguration(ctx, in, invocation)
		if e != nil {
			return "", e
		}

		pinned, e := s.pinJudgeContext(ctx, in, source)
		if e != nil {
			return "", e
		}
		configuration, e := json.Marshal(cfg)
		if e != nil {
			return "", e
		}
		if len(configuration) > store.MaxResultInputBytes {
			return "", errors.New("judge instruction configuration exceeds retained record bound")
		}
		captured, e := json.Marshal(pinned)
		if e != nil {
			return "", e
		}
		if len(captured) > 128<<20 {
			return "", errors.New("judge context exceeds retained record bound")
		}
		caller := "manual"
		if in.GraphInstanceID > 0 {
			caller = "workflow"
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		q, err = s.DB.AdmitJudgeRequest(ctx, store.JudgeRequest{CallerKind: caller, GraphInstanceID: in.GraphInstanceID, JudgeNodeAttemptID: in.JudgeNodeAttemptID, SourceNode: in.SourceNode, SourceNodeAttemptID: in.SourceNodeAttemptID, Task: in.Ref.String(), TicketKey: in.TicketKey, CommentID: in.RemoteCommentID, SessionID: in.SessionID, TriggerCommentID: in.CommentID, Source: source, Configuration: configuration, Evidence: captured})
	}
	if err != nil {
		return "", err
	}
	if q.Task != in.Ref.String() || q.SessionID != in.SessionID || q.GraphInstanceID != in.GraphInstanceID || q.JudgeNodeAttemptID != in.JudgeNodeAttemptID || q.SourceNode != in.SourceNode {
		return "", store.ErrResultAuthority
	}
	if len(q.Report) > 0 {
		return savedJudgeText(q.Report)
	}
	var cfg judgeConfiguration
	var pinned judgeContext
	if len(q.Configuration) > store.MaxResultInputBytes {
		return "", errors.New("judge retained configuration exceeds record bound")
	}
	if err = json.Unmarshal(q.Configuration, &cfg); err != nil {
		return "", fmt.Errorf("decode judge retained configuration: %w", err)
	}
	if err = json.Unmarshal(q.Evidence, &pinned); err != nil {
		return "", err
	}
	attempts, err := s.DB.ListJudgeAttempts(ctx, q.ID)
	if err != nil {
		return "", err
	}
	if len(attempts) > 0 {
		// A process may have submitted before a crash. Its exact immutable result is
		// adopted; an allocated launch without one is interrupted, never relaunched.
		a := attempts[len(attempts)-1]
		revision, e := s.DB.JudgeAcceptedResult(ctx, q.ID)
		if e != nil {
			return "", e
		}
		cfg.ObservedModel = a.ObservedModel
		cfg.ProviderVersion = a.ProviderVersion
		cfg.Effective.Provider = config.Kind(a.Provider)
		cfg.Effective.Model = a.Model
		cfg.Effective.Thinking = config.Thinking(a.Thinking)
		if a.Ordinal > 1 {
			cfg.Effective.FallbackReason = "Selected provider was unavailable."
		}
		if a.State == "completed" {
			var report agentresult.JudgeReport
			if json.Unmarshal(revision, &report) == nil && agentresult.ValidateJudge(report).Valid {
				return s.save(ctx, q, cfg, pinned, report, "completed")
			}
		}
		if err := s.DB.FinishJudgeAttempt(context.WithoutCancel(ctx), a.ID, "interrupted", "Provider launch interrupted before a usable result was retained."); err != nil {
			return "", err
		}
		return s.save(ctx, q, cfg, pinned, unableJudge("Provider execution was interrupted; no assessment was rerun."), "interrupted")
	}
	if in.RecoverOnly {
		return s.save(ctx, q, cfg, pinned, unableJudge("Provider execution was abandoned before launch; no assessment was rerun."), "interrupted")
	}
	decoder := json.NewDecoder(bytes.NewReader(q.Configuration))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return "", fmt.Errorf("decode judge launch configuration: %w", err)
	}
	if err := validateJudgeInstructions(cfg); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	invocation := cfg.Effective
	// One wall-clock budget covers primary and the optional availability fallback.
	budget := 30 * time.Minute
	if in.Budget > 0 && in.Budget < budget {
		budget = in.Budget
	}
	budgetCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	workspace, err := s.openJudgeWorkspace(budgetCtx, q, pinned)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := workspace.close(context.WithoutCancel(ctx)); closeErr != nil {
			s.logJudge(ctx, "judge workspace cleanup failed", closeErr)
		}
	}()
	for launch := 0; launch < 2; launch++ {
		report, runErr := s.launch(budgetCtx, q, &cfg, workspace, invocation)
		if errors.Is(runErr, errJudgePersistence) {
			return "", runErr
		}
		if runErr == nil {
			cfg.Effective = invocation
			return s.save(ctx, q, cfg, pinned, report, "completed")
		}
		retained, acceptedErr := s.DB.JudgeAcceptedResult(ctx, q.ID)
		if acceptedErr != nil {
			return "", acceptedErr
		}
		if launch == 0 && len(retained) == 0 && errors.Is(runErr, agent.ErrJudgeUnavailable) && budgetCtx.Err() == nil {
			next, fallbackErr := s.Config.JudgeFallback(cfg.Requested.JudgeOptions, config.Kind(q.Source.Provider), invocation, "Selected provider was unavailable.")
			if fallbackErr == nil {
				invocation = next
				continue
			}
			runErr = errors.Join(runErr, fallbackErr)
		}
		message := "The provider could not complete the assessment: " + runErr.Error()
		body, e := s.Redactor.Apply(context.WithoutCancel(ctx), message, 8192)
		if e != nil {
			return "", e
		}
		cfg.Effective = invocation
		return s.save(context.WithoutCancel(ctx), q, cfg, pinned, unableJudge(body.String()), "failed")
	}
	return "", errors.New("judge launch bound exceeded")
}

func unableJudge(reason string) agentresult.JudgeReport {
	return agentresult.JudgeReport{Version: 1, Verdict: "unable_to_assess", Summary: reason, Uncertainty: []string{reason}, Findings: []agentresult.JudgeFinding{}}
}

func (s *JudgeService) save(ctx context.Context, q store.JudgeRequest, cfg judgeConfiguration, pinned judgeContext, r agentresult.JudgeReport, status string) (string, error) {
	p := JudgePresentation{SourceName: judgeSourceName(q.Source), SourceReference: q.Source.Reference, Provider: string(cfg.Effective.Provider), Model: cfg.Effective.Model, Thinking: string(cfg.Effective.Thinking), FallbackReason: cfg.Effective.FallbackReason, Commit: pinned.Commit, RepositoryGap: pinned.RepositoryGap, Report: r, ObservedModel: cfg.ObservedModel, ProviderVersion: cfg.ProviderVersion, Cutoff: pinned.Cutoff.Format(time.RFC3339)}
	if q.Source.NewestLegacy {
		p.SourceReference += "; chosen as the newest legacy save (older saves have no verified commit order; use --target to pick another)"
	}
	if q.CallerKind == "workflow" {
		p.SourceReference = fmt.Sprintf("graph instance %d, node %s, attempt %d; %s", q.GraphInstanceID, q.SourceNode, q.SourceNodeAttemptID, q.Source.Reference)
	}
	if p.Thinking == "" {
		p.Thinking = "native default (unset)"
	}
	raw, err := json.Marshal(judgeSavedReport{Presentation: p, Execution: status})
	if err != nil {
		return "", err
	}
	if err = s.DB.SaveJudgeReport(ctx, q.ID, raw); err != nil {
		return "", err
	}
	return savedJudgeText(raw)
}
func savedJudgeText(raw []byte) (string, error) {
	return savedJudgeTextAs(raw, "")
}

// savedJudgeTextAs renders a saved report in the format an advisory node
// declares: "brief" for the short form, otherwise the full report.
func savedJudgeTextAs(raw []byte, format string) (string, error) {
	var saved judgeSavedReport
	if err := json.Unmarshal(raw, &saved); err != nil {
		return "", err
	}
	if format == JudgeBriefFormat {
		return JudgeBriefText(saved.Presentation), nil
	}
	return JudgeText(saved.Presentation), nil
}

func (s *JudgeService) launch(ctx context.Context, q store.JudgeRequest, cfg *judgeConfiguration, workspace *judgeWorkspace, invocation config.JudgeInvocation) (report agentresult.JudgeReport, returnedErr error) {
	if err := validateJudgeInstructions(*cfg); err != nil {
		return report, err
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	a, err := s.DB.StartJudgeAttempt(ctx, q.ID, string(invocation.Provider), invocation.Model, string(invocation.Thinking))
	if err != nil {
		return report, fmt.Errorf("%w: %w", errJudgePersistence, err)
	}
	defer func() {
		state := "completed"
		failure := ""
		if returnedErr != nil {
			state = "failed"
			failure = "Provider did not return an accepted advisory report."
		}
		if finishErr := s.DB.FinishJudgeAttempt(context.WithoutCancel(ctx), a.ID, state, failure); finishErr != nil {
			returnedErr = fmt.Errorf("%w: %w", errJudgePersistence, finishErr)
		}
	}()
	service, err := bindJudgeResults(ctx, s.DB, a.ID)
	if err != nil {
		return report, fmt.Errorf("%w: %w", errJudgePersistence, err)
	}
	if err = os.MkdirAll(s.Paths.SubmissionDir(), 0700); err != nil {
		return report, err
	}
	broker, err := submission.Start(ctx, submission.Options{Directory: s.Paths.SubmissionDir(), AllowResult: true, AllowTools: true, AttemptReference: a.ID, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: service.handle})
	if err != nil {
		return report, err
	}
	defer func() { returnedErr = errors.Join(returnedErr, broker.Close()) }()
	spec, err := agent.NewMCPLaunchSpec(s.Executable, broker.DescriptorPath(), a.ID, agent.JudgeToolNames()...)
	if err != nil {
		return report, err
	}
	prompt := judgePrompt(cfg.Role, q, cfg.Requested.Focus, workspace)
	meterAttempt, err := s.DB.StartJudgeMeterAttempt(ctx, a.ID, string(invocation.Provider))
	if err != nil {
		return report, fmt.Errorf("%w: %w", errJudgePersistence, err)
	}
	req := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: a.ID, Model: invocation.Model, Prompt: prompt, WorkingDirectory: workspace.repo, ReadableDirectories: []string{workspace.context}, MCPLaunch: spec, UsageAttemptID: meterAttempt.ID, Usage: &agent.UsageCapture{AttemptID: meterAttempt.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		return s.DB.RecordMeterEvent(c, meterAttempt.ID, string(invocation.Provider), event)
	}}}, Thinking: invocation.Thinking}
	runner := s.Providers[agent.Kind(invocation.Provider)]
	var result agent.JudgeRunResult
	if runner == nil {
		err = agent.ErrJudgeUnavailable
	} else {
		result, err = runner.RunJudge(ctx, req)
	}
	cfg.ObservedModel = result.ObservedModel
	cfg.ProviderVersion = result.ProviderVersion
	provenanceErr := s.DB.RecordJudgeInvocation(context.WithoutCancel(ctx), a.ID, result.ObservedModel, result.ProviderVersion)

	outcome, status, version, format := "failure", "unknown", "", ""
	codes := []string{"missing_evidence"}
	if err == nil {
		outcome = "success"
	}
	if ctx.Err() != nil {
		outcome = "cancelled"
		err = errors.Join(err, ctx.Err())
	}
	if result.Usage != nil {
		status = string(result.Usage.Status)
		version = result.Usage.Version
		format = result.Usage.SourceFormat
		codes = result.Usage.Diagnostics
	}
	finishErr := s.DB.FinishMeterAttempt(context.WithoutCancel(ctx), meterAttempt.ID, outcome, status, version, format, codes)
	if provenanceErr != nil || finishErr != nil {
		return report, fmt.Errorf("%w: %w", errJudgePersistence, errors.Join(provenanceErr, finishErr))
	}
	if err != nil {
		return report, err
	}
	report, err = service.accepted(ctx)
	if err != nil {
		return report, fmt.Errorf("provider ended without a valid shared advisory result: %w", err)
	}
	return report, nil
}

// ReportJudge uses the existing durable delivery journal and the requesting ticket.
func (r *Reporter) ReportJudge(ctx context.Context, ref naming.Ref, sessionID, commentID int64, text string) (Posted, error) {
	intent := r.deliveryIntent(ref, ref, sessionID, commentID, 0, "judge:"+strconv.FormatInt(commentID, 10))
	posted, err := r.deliver(ctx, intent, func() (redact.Body, error) { return r.renderer.RenderReportDelivery(ctx, text, "") })
	if err != nil {
		return posted, err
	}
	_, err = r.Consume(ctx, sessionID, commentID)
	return posted, err
}

func judgeSourceName(source store.JudgeSource) string {
	if source.Kind == "plan" {
		return "saved Plan"
	}
	if source.Label != "" {
		return "saved " + source.Label
	}
	var snapshot struct {
		Result struct {
			Payload struct {
				Research *json.RawMessage `json:"research"`
				Plan     *json.RawMessage `json:"plan"`
				Bug      *json.RawMessage `json:"bug"`
			} `json:"payload"`
		} `json:"result"`
	}
	if json.Unmarshal(source.Snapshot, &snapshot) == nil {
		if snapshot.Result.Payload.Research != nil {
			return "saved research result"
		}
		if snapshot.Result.Payload.Plan != nil {
			return "saved Plan"
		}
		if snapshot.Result.Payload.Bug != nil {
			return "saved Bug analysis"
		}
	}
	return "saved " + source.Kind
}
