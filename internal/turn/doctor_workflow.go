package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// The Doctor is one conversational agent on a stopped task. Everything it does
// lives in its role's Markdown and its flow in the graph file. The factory only
// guards it: the owner starts and answers it, it starts only on a stopped task,
// the factory hands it read-only evidence the sandbox hides, and the factory
// commits and pushes whatever it changed through the shared delivery path.

// doctorBusyReason is the refusal an owner reads when the task is not stopped.
const doctorBusyReason = "The Doctor did not start: this task still has work in flight (a running run or a pending step). Nothing was changed. Wait until it stops, or stop it on the factory host with `litefactory doctor stop <task>`, then comment `/doctor` again."

// doctorEvidenceLimit bounds the tail of one run log copied into the bundle.
const doctorEvidenceLimit = 256 << 10

// doctorEvidenceRuns bounds how many of the task's latest run logs are copied.
const doctorEvidenceRuns = 5

// DoctorWorkflowPolicy is Spike's shared conversational policy under the Doctor
// mode, plus the factory-prepared evidence bundle.
func DoctorWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Mode:                      agent.ModeDoctor,
		Control:                   graphexec.ControlPolicy{Mode: agent.ModeDoctor, Responses: []string{"ask", "done"}, Defaults: []string{"ask", "done"}, Ask: "ask"},
		Decode:                    agentresult.DecodeDoctorNode,
		Guidance:                  doctorNodeInstructions,
		Contract:                  validateResearchComment,
		Render:                    renderSpikeComment,
		NextStep:                  doctorNextStep,
		Failure:                   doctorFailureCandidate,
		Evidence:                  doctorEvidence,
		Questions:                 true,
		AcceptAfterProcessFailure: true,
	}
}

// DoctorSpecs is the closed registry for prelaunch built-in asset validation.
func DoctorSpecs() (graphexec.Registry, error) {
	return WorkflowSpecs(DoctorWorkflowPolicy())
}

const doctorNodeInstructions = "\nFactory invocation: Doctor graph node. Deliver the result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request_id. Terminal text is never read as a result. Read the doctor-result skill before you submit.\n"

// admitDoctorExecution starts, continues or resumes the Doctor on this task.
// The owner's authority was checked before the turn; this adds the one Doctor
// rule: a fresh Doctor starts only on a stopped task.
func admitDoctorExecution(ctx context.Context, opts Options, req Request, result Result) (Result, bool, error) {
	if req.mode() != string(agent.ModeDoctor) {
		return result, false, nil
	}
	policy := DoctorWorkflowPolicy()
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		out, err := resumeWorkflowDispatch(ctx, opts, req, result, agent.WorkflowDoctor, policy, ExecuteDoctorWorkflow)
		return out, true, err
	}
	if req.Continuation != nil {
		return result, true, errors.New("doctor graphs require explicit owner continuation")
	}
	instance, err := opts.DB.LatestWorkflowInstance(ctx, req.SessionID, string(agent.ModeDoctor))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return result, true, err
	}
	if err != nil || instance.Status != store.GraphRunning {
		if stopErr := opts.DB.CheckTaskStopped(ctx, req.SessionID); stopErr != nil {
			if errors.Is(stopErr, store.ErrTaskNotStopped) {
				out, refuseErr := prelaunchRefused(ctx, opts, result, doctorBusyReason)
				return out, true, refuseErr
			}
			return result, true, stopErr
		}
	}
	specs, err := DoctorSpecs()
	if err != nil {
		return result, true, err
	}
	definition, err := capturedWorkflow(ctx, opts, req, agent.WorkflowDoctor, specs)
	if err != nil {
		return result, true, err
	}
	out, err := launchWorkflowGraph(ctx, opts, req, result, definition, string(agent.ModeDoctor), ExecuteDoctorWorkflow)
	return out, true, err
}

// ExecuteDoctorWorkflow advances the captured Doctor graph under Execute's task
// lock through the shared driver, with the Doctor's own factory policy.
func ExecuteDoctorWorkflow(ctx context.Context, opts Options, req Request, result Result, definition *teamsource.WorkflowDefinition, admission store.WorkflowAdmission) (Result, error) {
	return ExecuteWorkflow(ctx, opts, req, result, definition, admission, DoctorWorkflowPolicy())
}

// doctorEvidence writes the task's recorded rows, its latest run log tails and
// its git state into the worktree's git directory, which the agent may read and
// the factory never commits, and tells the agent where they are. Every file
// passes the factory's redactor first; without one nothing is written.
func doctorEvidence(ctx context.Context, w *researchWorkflow, a ActivityRequest) (string, error) {
	prep := w.prep
	if prep == nil || prep.worktree == nil {
		return "", errors.New("doctor evidence needs the task's worktree")
	}
	if w.opts.MCP == nil || w.opts.MCP.Redactor == nil {
		return "", errors.New("doctor evidence needs the factory redactor")
	}
	redactor := w.opts.MCP.Redactor
	gitDir, err := worktreeGitDir(prep.worktree.Path())
	if err != nil {
		return "", err
	}
	dir := filepath.Join(gitDir, "litefactory-doctor", strconv.FormatInt(a.NodeAttemptID, 10))
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		return "", err
	}
	files := map[string]string{}
	rows, err := w.opts.DB.TaskRows(ctx, prep.session.ID)
	if err != nil {
		return "", err
	}
	if files["task.json"], err = indentedJSON(rows); err != nil {
		return "", err
	}
	runs, err := w.opts.DB.ListRuns(ctx, prep.session.ID)
	if err != nil {
		return "", err
	}
	copied := 0
	for i := len(runs) - 1; i >= 0 && copied < doctorEvidenceRuns; i-- {
		run := runs[i]
		if run.ID == prep.run.ID || run.LogPath == "" {
			continue
		}
		tail, err := readTail(run.LogPath, doctorEvidenceLimit)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		files[filepath.Join("logs", fmt.Sprintf("run-%d.jsonl", run.ID))] = tail
		copied++
	}
	if w.opts.Git != nil {
		observed, err := w.opts.Git.Observe(ctx, prep.worktree.Path(), prep.worktree.Branch())
		state := map[string]any{"branch": prep.worktree.Branch(), "head": observed.Head, "branch_head": observed.BranchHead, "status": observed.Status, "unpushed": observed.Unpushed}
		if err != nil {
			state["error"] = err.Error()
		}
		if files["git.json"], err = indentedJSON(state); err != nil {
			return "", err
		}
	}
	for name, text := range files {
		safe, err := redactor.Apply(ctx, text, len(text)+redact.Margin+1)
		if err != nil {
			return "", fmt.Errorf("redact doctor evidence %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(safe.String()), 0o600); err != nil {
			return "", err
		}
	}
	return "Factory evidence for this task (read-only, redacted; the factory's database and logs are otherwise unreadable): " + dir + "\n", nil
}

// worktreeGitDir reads the git directory a linked worktree's .git file names.
func worktreeGitDir(worktree string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(worktree, ".git")) // #nosec G304 -- the factory's own worktree.
	if err != nil {
		return "", fmt.Errorf("read the worktree's git link: %w", err)
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	if !ok || !filepath.IsAbs(dir) {
		return "", errors.New("the worktree's git link names no absolute git directory")
	}
	return dir, nil
}

func indentedJSON(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	return string(raw) + "\n", err
}

// readTail reads at most limit trailing bytes of a log, starting at a line
// boundary and keeping only valid UTF-8.
func readTail(path string, limit int64) (string, error) {
	in, err := os.Open(path) // #nosec G304 -- a run log path the factory recorded.
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	cut := info.Size() > limit
	if cut {
		if _, err := in.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return "", err
		}
	}
	raw, err := io.ReadAll(io.LimitReader(in, limit))
	if err != nil {
		return "", err
	}
	if cut {
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		}
	}
	return strings.ToValidUTF8(string(raw), ""), nil
}

// doctorFailureCandidate authors the owner-facing account of a failed Doctor
// step. It claims no diagnosis: the outcome is blocked and the reason carries
// the cause.
func doctorFailureCandidate(req Request, cause error) agentresult.Result {
	reason := "The Doctor failed before it produced a reportable result."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = boundedFailureReason(strings.TrimSpace(cause.Error()))
	}
	request := strings.TrimSpace(req.Trigger.Body)
	if request == "" || store.IsGraphTrigger(req.Trigger.CommentID) {
		request = "The owner's Doctor request is unavailable on this attempt."
	}
	return agentresult.Result{
		Outcome: "blocked",
		Comment: "The Doctor did not finish. No diagnosis is claimed and nothing it changed was saved. The reason is recorded below; comment `/doctor` to start again.",
		Reason:  reason,
		Payload: agentresult.Payload{Doctor: &agentresult.Spike{Request: request}},
	}
}

func doctorNextStep(profile *graphexec.Profile, node, kind string) string {
	switch kind {
	case "question":
		return "reply on this issue to answer the Doctor; it picks up where it left off. Nothing runs until you reply."
	case "final":
		return "the Doctor is finished. Continue the task with its usual command, or comment `/doctor` again."
	}
	return researchNextStep(profile, node, kind)
}
