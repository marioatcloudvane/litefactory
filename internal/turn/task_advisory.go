package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

// TaskAdvisorySettings freezes the provider and finite budget at admission.
type TaskAdvisorySettings struct {
	Provider agent.Kind      `json:"provider"`
	Model    string          `json:"model"`
	Thinking config.Thinking `json:"thinking"`
	Deadline time.Duration   `json:"deadline"`
}

// TaskAdvisoryGuidance is retained at admission, never loaded live during execution.
type TaskAdvisoryGuidance struct {
	Settings       TaskAdvisorySettings `json:"settings"`
	Advisory       json.RawMessage      `json:"advisory"`
	Analysis       string               `json:"analysis"`
	RepositoryPath string               `json:"repository_path,omitempty"`
}

// CaptureTaskAdvisoryGuidance freezes the complete advisory role/skill closure.
func CaptureTaskAdvisoryGuidance(ctx context.Context, paths config.Paths, analysis, repositoryPath string, settings ...TaskAdvisorySettings) (json.RawMessage, error) {
	p, err := instructionpackage.New(ctx, paths)
	if err != nil {
		return nil, err
	}
	c, _, err := p.Capture(ctx, "task-advisory")
	if err != nil {
		return nil, err
	}
	raw, err := c.Encode()
	if err != nil {
		return nil, err
	}
	var frozen TaskAdvisorySettings
	if len(settings) == 1 {
		frozen = settings[0]
	}
	return json.Marshal(TaskAdvisoryGuidance{Advisory: raw, Analysis: analysis, RepositoryPath: repositoryPath, Settings: frozen})
}

// TaskAdvisoryRunner reuses the provider's enforced read-only judge transport.
// It has no forge publisher, owner action invocation or workflow result service.
type TaskAdvisoryRunner struct {
	DB         *store.DB
	Paths      config.Paths
	Executable string
	Providers  map[agent.Kind]agent.JudgeRunner
	Git        *git.Binary
	Provider   agent.Kind
	Model      string
	Thinking   config.Thinking
	Deadline   time.Duration
}

// CaptureGuidance retains this runner's installed settings and complete role closure.
func (r *TaskAdvisoryRunner) CaptureGuidance(ctx context.Context, analysis, repositoryPath string) (json.RawMessage, error) {
	if !r.Ready() {
		return nil, errors.New("task advisory settings unavailable")
	}
	return CaptureTaskAdvisoryGuidance(ctx, r.Paths, analysis, repositoryPath, TaskAdvisorySettings{Provider: r.Provider, Model: r.Model, Thinking: r.Thinking, Deadline: r.Deadline})
}

// Ready reports whether admission can capture safe installed runner settings.
func (r *TaskAdvisoryRunner) Ready() bool {
	return r != nil && r.DB != nil && r.Executable != "" && r.Providers[r.Provider] != nil && strings.TrimSpace(r.Model) != "" && r.Deadline > 0 && r.Deadline <= 2*time.Hour
}

// Run launches at most one isolated provider invocation for the admitted execution.
func (r *TaskAdvisoryRunner) Run(ctx context.Context, in taskdiscussion.RunRequest) (answer taskdiscussion.Answer, returnedErr error) {
	if r == nil || r.DB == nil || r.Executable == "" || in.ExecutionID == "" || in.JobID == "" || (in.Operation != "ask" && in.Operation != "judge") {
		return answer, errors.New("task advisory runner unavailable or invalid execution")
	}
	if err := r.checkTaskAdvisoryRequest(ctx, in); err != nil {
		return answer, err
	}
	var guidance TaskAdvisoryGuidance
	if err := submission.DecodeJSON(in.Basis.Guidance, &guidance); err != nil {
		return answer, err
	}
	settings := guidance.Settings
	if r.Providers[settings.Provider] == nil || strings.TrimSpace(settings.Model) == "" || settings.Deadline <= 0 || settings.Deadline > 2*time.Hour {
		return answer, errors.New("captured advisory runner settings unavailable")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return answer, errors.New("advisory execution requires the shared worker deadline")
	}
	captured, err := instructionpackage.Restore(guidance.Advisory)
	if err != nil {
		return answer, err
	}
	if captured.Role() != "task-advisory" {
		return answer, errors.New("task advisory role mismatch")
	}
	service, err := bindTaskAdvisoryResults(ctx, r.DB, in.ExecutionID)
	if err != nil {
		return answer, err
	}
	if saved, e := service.accepted(ctx); e == nil {
		return saved, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return answer, e
	}
	workspace, err := r.openTaskAdvisoryWorkspace(ctx, in, guidance)
	if err != nil {
		return answer, err
	}
	defer func() { returnedErr = errors.Join(returnedErr, workspace.close(context.WithoutCancel(ctx))) }()
	if err = os.MkdirAll(r.Paths.SubmissionDir(), 0700); err != nil {
		return answer, err
	}
	broker, err := submission.Start(ctx, submission.Options{Directory: r.Paths.SubmissionDir(), AllowResult: true, AllowTools: true, AttemptReference: in.ExecutionID, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: service.handle})
	if err != nil {
		return answer, err
	}
	defer func() { returnedErr = errors.Join(returnedErr, broker.Close()) }()
	spec, err := agent.NewMCPLaunchSpec(r.Executable, broker.DescriptorPath(), in.ExecutionID, agent.JudgeToolNames()...)
	if err != nil {
		return answer, err
	}
	facts, err := json.Marshal(struct{ Operation, Question, Task, Stage, Revision, Commit, ContextFile, RepositoryGap string }{in.Operation, in.Question, in.Basis.TaskRef, in.Basis.Stage, in.Basis.Revision, in.Basis.Commit, filepath.Join(workspace.context, "task.json"), workspace.gap})
	if err != nil {
		return answer, err
	}
	prompt := string(captured.Instructions()) + "\n\n" + string(captured.SkillInstructions("task-advisory")) + "\n\nCaptured analytical guidance (evidence):\n" + guidance.Analysis + "\n\nInvocation facts:\n" + string(facts)
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: in.ExecutionID, Model: settings.Model, Prompt: prompt, WorkingDirectory: workspace.repo, ReadableDirectories: []string{workspace.context}, MCPLaunch: spec}, Thinking: settings.Thinking}
	if err = request.Validate(); err != nil {
		return answer, err
	}
	if err = r.DB.StartAdvisoryInvocation(ctx, service.binding, string(settings.Provider), settings.Model, string(settings.Thinking), settings.Deadline); err != nil {
		return answer, err
	}
	meterAttempt, err := r.DB.StartAdvisoryMeterAttempt(ctx, in.ExecutionID, string(settings.Provider))
	if err != nil {
		finish, cancel := agent.EvidenceContext(ctx)
		defer cancel()
		recordErr := r.DB.FinishAdvisoryInvocation(finish, service.binding, "", "", "failed", []byte(`null`))
		return answer, errors.Join(err, recordErr)
	}
	var sinkFailed atomic.Bool
	request.Invocation.UsageAttemptID = meterAttempt.ID
	request.Invocation.Usage = &agent.UsageCapture{AttemptID: meterAttempt.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		canonicalErr := r.DB.RecordMeterEvent(c, meterAttempt.ID, string(settings.Provider), event)
		raw, encodeErr := json.Marshal(event)
		var supplementalErr error
		if encodeErr == nil {
			supplementalErr = r.DB.RecordAdvisoryUsage(c, service.binding, raw)
		}
		eventErr := errors.Join(canonicalErr, encodeErr, supplementalErr)
		if eventErr != nil {
			sinkFailed.Store(true)
		}
		return eventErr
	}}
	observed, runErr := r.Providers[settings.Provider].RunJudge(ctx, request)
	usage, usageErr := json.Marshal(observed.Usage)
	outcome, status, version, format := "failure", "unknown", "", ""
	codes := []string{"missing_evidence"}
	if runErr == nil {
		outcome = "success"
	}
	if observed.Usage != nil {
		status = string(observed.Usage.Status)
		version = observed.Usage.Version
		format = observed.Usage.SourceFormat
		codes = append([]string(nil), observed.Usage.Diagnostics...)
	}
	if sinkFailed.Load() {
		status = "partial"
		codes = append(codes, "persistence_failure")
	}
	if ctx.Err() != nil {
		outcome = "cancelled"
	}
	finish, cancel := agent.EvidenceContext(ctx)
	defer cancel()
	// The isolated provider result has no provider-affirmed zero-cost-refusal field.
	meterFinishErr := r.DB.FinishMeterAttemptWithRefusal(finish, meterAttempt.ID, outcome, status, version, format, codes, false)
	invocationOutcome := "succeeded"
	if outcome == "failure" {
		invocationOutcome = "failed"
	}
	if outcome == "cancelled" {
		invocationOutcome = "cancelled"
	}
	invocationErr := r.DB.FinishAdvisoryInvocation(finish, service.binding, observed.ObservedModel, observed.ProviderVersion, invocationOutcome, usage)
	if finishErr := errors.Join(meterFinishErr, invocationErr, usageErr); finishErr != nil {
		return answer, errors.Join(runErr, finishErr)
	}
	// A durable accepted answer remains recoverable even when the transport reply is lost.
	saved, readErr := service.accepted(context.WithoutCancel(ctx))
	if readErr == nil {
		return saved, nil
	}
	return answer, errors.Join(runErr, readErr)
}

// checkTaskAdvisoryRequest prevents a caller from replacing admitted evidence or scope.
func (r *TaskAdvisoryRunner) checkTaskAdvisoryRequest(ctx context.Context, in taskdiscussion.RunRequest) error {
	admitted, err := r.DB.ReadAdvisoryByExecution(ctx, in.ExecutionID)
	if err != nil {
		return err
	}
	if admitted.ID != in.AdmissionID || admitted.JobID != in.JobID || admitted.Operation != in.Operation || admitted.Question != in.Question {
		return store.ErrResultAuthority
	}
	var basis taskdiscussion.Basis
	if err = json.Unmarshal([]byte(admitted.Basis), &basis); err != nil {
		return err
	}
	frozen, err := json.Marshal(basis)
	if err != nil {
		return err
	}
	supplied, err := json.Marshal(in.Basis)
	if err != nil {
		return err
	}
	var frozenContext, suppliedContext bytes.Buffer
	if err = json.Compact(&frozenContext, []byte(admitted.Context)); err != nil {
		return err
	}
	if err = json.Compact(&suppliedContext, in.Context); err != nil {
		return err
	}
	if !bytes.Equal(frozen, supplied) || !bytes.Equal(frozenContext.Bytes(), suppliedContext.Bytes()) {
		return store.ErrResultAuthority
	}
	return nil
}

// Recover reads an existing receipt only. It never invokes a provider.
func (r *TaskAdvisoryRunner) Recover(ctx context.Context, in taskdiscussion.RunRequest) (taskdiscussion.Answer, bool, error) {
	var zero taskdiscussion.Answer
	if r == nil || r.DB == nil {
		return zero, false, store.ErrResultAuthority
	}
	if err := r.checkTaskAdvisoryRequest(ctx, in); err != nil {
		return zero, false, err
	}
	b, err := r.DB.AdvisoryResultBinding(ctx, in.ExecutionID)
	if errors.Is(err, store.ErrNotFound) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	s := &taskAdvisoryResults{db: r.DB, binding: b}
	answer, err := s.accepted(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return zero, false, nil
	}
	return answer, err == nil, err
}

func (r *TaskAdvisoryRunner) openTaskAdvisoryWorkspace(ctx context.Context, in taskdiscussion.RunRequest, g TaskAdvisoryGuidance) (_ *judgeWorkspace, returnedErr error) {
	root, err := os.MkdirTemp("", "lf-task-advisory-")
	if err != nil {
		return nil, err
	}
	w := &judgeWorkspace{root: root}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, w.close(context.WithoutCancel(ctx)))
		}
	}()
	w.root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	w.repo, w.context = filepath.Join(w.root, "repo"), filepath.Join(w.root, "context")
	if err = os.Mkdir(w.context, 0700); err != nil {
		return nil, err
	}
	evidence, err := json.Marshal(struct {
		Basis   taskdiscussion.Basis `json:"basis"`
		Context json.RawMessage      `json:"context"`
	}{in.Basis, in.Context})
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(w.context, "task.json"), evidence, 0600); err != nil {
		return nil, err
	}
	if g.RepositoryPath != "" && in.Basis.Commit != "" && r.Git != nil {
		clone, e := r.Git.Open(ctx, g.RepositoryPath)
		if e == nil {
			_, e = clone.WorktreeAdd(ctx, w.repo, git.WorktreeAddOptions{Branch: in.Basis.Commit, Detach: true})
		}
		if e == nil {
			w.clone, w.commit = clone, in.Basis.Commit
		} else {
			w.gap = "Captured repository checkout is unavailable."
		}
	} else {
		w.gap = "No captured repository checkout is available."
	}
	if w.clone == nil {
		if err = os.MkdirAll(w.repo, 0700); err != nil {
			return nil, err
		}
	}
	return w, nil
}

var _ taskdiscussion.Runner = (*TaskAdvisoryRunner)(nil)
