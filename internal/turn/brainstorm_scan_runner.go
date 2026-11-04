package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/skillclosure"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// BrainstormScanRunner runs one brainstorm scan with the captured Research role
// through the provider's enforced read-only judge transport. It has no forge
// publisher, owner action invocation or workflow result service.
type BrainstormScanRunner struct {
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

// Ready reports whether the runner has safe installed settings.
func (r *BrainstormScanRunner) Ready() bool {
	return r != nil && r.DB != nil && r.Executable != "" && r.Providers[r.Provider] != nil && strings.TrimSpace(r.Model) != "" && r.Deadline > 0 && r.Deadline <= 2*time.Hour
}

// Run launches at most one isolated provider invocation for the admitted scan.
func (r *BrainstormScanRunner) Run(ctx context.Context, in brainstorm.RunRequest) (result brainstorm.RunResult, returnedErr error) {
	if !r.Ready() || in.ExecutionID == "" || in.JobID == "" {
		return result, errors.New("brainstorm scan runner unavailable or invalid execution")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return result, errors.New("brainstorm scan requires the shared worker deadline")
	}
	scan, err := r.checkRequest(ctx, in)
	if err != nil {
		return result, err
	}
	b, err := r.DB.BindBrainstormScanResult(ctx, in.ExecutionID)
	if err != nil {
		return result, err
	}
	results := &brainstormScanResults{db: r.DB, binding: b, purpose: scan.Purpose}
	if saved, e := results.accepted(ctx, scan.BasisCommit); e == nil {
		return saved, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return result, e
	}
	prompt, err := brainstormScanInstructions(in.Team, scan.Purpose)
	if err != nil {
		return result, err
	}
	repo, err := brainstormScanRepository(ctx, r.DB, in.RepositoryRef)
	if err != nil {
		return result, err
	}
	workspace, err := r.openWorkspace(ctx, repo, in)
	if err != nil {
		return result, err
	}
	defer func() { returnedErr = errors.Join(returnedErr, workspace.close(context.WithoutCancel(ctx))) }()
	if err = r.DB.SetBrainstormScanCommit(ctx, b, workspace.commit); err != nil {
		return result, err
	}
	if err = os.MkdirAll(r.Paths.SubmissionDir(), 0o700); err != nil {
		return result, err
	}
	broker, err := submission.Start(ctx, submission.Options{Directory: r.Paths.SubmissionDir(), AllowResult: true, AllowTools: true, AttemptReference: in.ExecutionID, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: results.handle})
	if err != nil {
		return result, err
	}
	defer func() { returnedErr = errors.Join(returnedErr, broker.Close()) }()
	spec, err := agent.NewMCPLaunchSpec(r.Executable, broker.DescriptorPath(), in.ExecutionID, agent.JudgeToolNames()...)
	if err != nil {
		return result, err
	}
	facts, err := json.Marshal(struct {
		Question      string          `json:"question"`
		Repository    string          `json:"repository"`
		Commit        string          `json:"commit"`
		RepositoryGap string          `json:"repository_gap,omitempty"`
		ContextDir    string          `json:"context_directory"`
		Brainstorm    string          `json:"brainstorm"`
		Digest        json.RawMessage `json:"digest"`
	}{in.Question, in.RepositoryRef, workspace.commit, workspace.gap, filepath.Join(workspace.context, "brainstorm"), in.BrainstormID, in.Digest})
	if err != nil {
		return result, err
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: in.ExecutionID, Model: r.Model, Prompt: prompt + "\n\nInvocation facts:\n" + string(facts), WorkingDirectory: workspace.repo, ReadableDirectories: []string{workspace.context}, MCPLaunch: spec}, Thinking: r.Thinking}
	if err = request.Validate(); err != nil {
		return result, err
	}
	meter, err := r.DB.StartBrainstormScanMeterAttempt(ctx, b, string(r.Provider))
	if err != nil {
		return result, err
	}
	var sinkFailed atomic.Bool
	request.Invocation.UsageAttemptID = meter.ID
	request.Invocation.Usage = &agent.UsageCapture{AttemptID: meter.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		e := r.DB.RecordMeterEvent(c, meter.ID, string(r.Provider), event)
		if e != nil {
			sinkFailed.Store(true)
		}
		return e
	}}
	observed, runErr := r.Providers[r.Provider].RunJudge(ctx, request)
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
	if e := r.DB.FinishMeterAttemptWithRefusal(finish, meter.ID, outcome, status, version, format, codes, false); e != nil {
		return result, errors.Join(runErr, e)
	}
	// A durable accepted result remains recoverable even when the transport reply is lost.
	saved, readErr := results.accepted(context.WithoutCancel(ctx), workspace.commit)
	if readErr != nil {
		return result, errors.Join(runErr, readErr)
	}
	if observed.Usage != nil {
		if raw, e := json.Marshal(observed.Usage); e == nil {
			saved.Usage = raw
		}
	}
	return saved, nil
}

// Recover reads an existing receipt only. It never invokes a provider.
func (r *BrainstormScanRunner) Recover(ctx context.Context, in brainstorm.RunRequest) (brainstorm.RunResult, bool, error) {
	var zero brainstorm.RunResult
	if r == nil || r.DB == nil {
		return zero, false, store.ErrResultAuthority
	}
	scan, err := r.checkRequest(ctx, in)
	if err != nil {
		return zero, false, err
	}
	b, err := r.DB.BrainstormScanResultBinding(ctx, in.ExecutionID)
	if errors.Is(err, store.ErrNotFound) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	saved, err := (&brainstormScanResults{db: r.DB, binding: b, purpose: scan.Purpose}).accepted(ctx, scan.BasisCommit)
	if errors.Is(err, store.ErrNotFound) {
		return zero, false, nil
	}
	return saved, err == nil, err
}

// checkRequest prevents a caller from replacing the admitted scan identity.
func (r *BrainstormScanRunner) checkRequest(ctx context.Context, in brainstorm.RunRequest) (store.BrainstormScanRun, error) {
	scan, err := r.DB.ReadBrainstormScanByExecution(ctx, in.ExecutionID)
	if err != nil {
		return scan, err
	}
	if scan.ID != in.ScanID || scan.JobID != in.JobID || scan.BrainstormID != in.BrainstormID || scan.RepositoryRef != in.RepositoryRef || scan.Question != in.Question || scan.Purpose != in.Purpose {
		return scan, store.ErrResultAuthority
	}
	var team brainstorm.TeamCapture
	if err = json.Unmarshal([]byte(scan.TeamFiles), &team); err != nil {
		return scan, err
	}
	if team.Version != in.Team.Version || team.Role != in.Team.Role || len(team.Files) != len(in.Team.Files) {
		return scan, store.ErrResultAuthority
	}
	for name, content := range team.Files {
		if in.Team.Files[name] != content {
			return scan, store.ErrResultAuthority
		}
	}
	return scan, nil
}

// brainstormScanInstructions is the captured research role body followed by
// the two captured skills and their resources, all from the admission capture.
func brainstormScanInstructions(team brainstorm.TeamCapture, purpose string) (string, error) {
	role := team.Role
	if strings.HasPrefix(role, "---\n") {
		if end := strings.Index(role[4:], "\n---\n"); end >= 0 {
			role = role[4+end+5:]
		}
	}
	if strings.TrimSpace(role) == "" {
		return "", errors.New("captured research role is empty")
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(role, "\n"))
	skills := []string{"research-evidence", "brainstorm"}
	if purpose == brainstorm.PurposeDrafts {
		skills = append(skills, "brainstorm-drafts")
	}
	for _, skill := range skills {
		root := "skills/" + skill + "/SKILL.md"
		_, body, err := skillclosure.Parse([]byte(team.Files[root]), skill)
		if err != nil {
			return "", err
		}
		b.WriteString("\n\n")
		b.Write(bytes.TrimRight(body, "\n"))
		prefix := "skills/" + skill + "/"
		var names []string
		for name := range team.Files {
			if purpose != brainstorm.PurposeDrafts && strings.HasPrefix(name, "skills/brainstorm-drafts/") {
				continue
			}
			if strings.HasPrefix(name, prefix) && name != root {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			content := team.Files[name]
			if strings.HasSuffix(name, "/SKILL.md") {
				id := strings.TrimSuffix(strings.TrimPrefix(name, "skills/"), "/SKILL.md")
				_, parsed, err := skillclosure.Parse([]byte(content), id)
				if err != nil {
					return "", err
				}
				content = string(parsed)
			}
			b.WriteString("\n\nCaptured skill resource " + name + ":\n")
			b.WriteString(content)
		}
	}
	return b.String(), nil
}

func brainstormScanRepository(ctx context.Context, db *store.DB, ref string) (store.Repo, error) {
	forge, rest, ok := strings.Cut(ref, ":")
	owner, name, ok2 := strings.Cut(rest, "/")
	if !ok || !ok2 {
		return store.Repo{}, brainstorm.ErrInvalid
	}
	return db.GetRepoBySlug(ctx, forge, owner, name)
}

// openWorkspace makes a temporary detached read-only checkout of the default
// branch at the commit the registered clone last fetched, and writes the scan
// context files read-only. It never fetches.
func (r *BrainstormScanRunner) openWorkspace(ctx context.Context, repo store.Repo, in brainstorm.RunRequest) (_ *brainstormScanWorkspace, returnedErr error) {
	root, err := os.MkdirTemp("", "lf-brainstorm-scan-")
	if err != nil {
		return nil, err
	}
	w := &brainstormScanWorkspace{judgeWorkspace: &judgeWorkspace{root: root}}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, w.close(context.WithoutCancel(ctx)))
		}
	}()
	if w.root, err = filepath.EvalSymlinks(root); err != nil {
		w.root = root
		return nil, err
	}
	w.repo, w.context = filepath.Join(w.root, "repo"), filepath.Join(w.root, "context")
	dir := filepath.Join(w.context, "brainstorm")
	w.files = dir
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for name, raw := range in.ContextFiles {
		if name != path.Base(name) || !strings.HasSuffix(name, ".json") || len(raw) > brainstorm.MaxContextFileBytes {
			return nil, fmt.Errorf("invalid brainstorm context file %q", name)
		}
		if err = os.WriteFile(filepath.Join(dir, name), raw, 0o400); err != nil {
			return nil, err
		}
	}
	//nolint:gosec // Read-only private context directory; owner traversal only.
	if err = os.Chmod(dir, 0o500); err != nil {
		return nil, err
	}
	if r.Git == nil {
		return nil, errors.New("brainstorm scan requires git")
	}
	clone, err := r.Git.Open(ctx, repo.Path)
	if err != nil {
		return nil, err
	}
	var commit string
	for _, ref := range []string{"refs/remotes/origin/" + repo.BaseBranch, "refs/heads/" + repo.BaseBranch} {
		commit, err = clone.RevParse(ctx, ref+"^{commit}", git.RevParseOptions{})
		if err == nil {
			break
		}
		if !errors.Is(err, git.ErrRevisionNotFound) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err = clone.WorktreeAdd(ctx, w.repo, git.WorktreeAddOptions{Branch: commit, Detach: true}); err != nil {
		return nil, err
	}
	w.clone, w.commit = clone, commit
	return w, nil
}

// brainstormScanWorkspace makes the read-only context files removable again on close.
type brainstormScanWorkspace struct {
	*judgeWorkspace
	files string
}

func (w *brainstormScanWorkspace) close(ctx context.Context) error {
	if w.files != "" {
		//nolint:gosec // Restore owner write on the private directory so it can be removed.
		if err := os.Chmod(w.files, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(err, w.judgeWorkspace.close(ctx))
		}
	}
	return w.judgeWorkspace.close(ctx)
}

// brainstormScanResults is the private, fenced result writer of one scan execution.
type brainstormScanResults struct {
	db      *store.DB
	binding store.BrainstormScanResultBinding
	purpose string
}

// validate checks a result with the validator of the scan's purpose: a
// drafts scan accepts only payload.brainstorm_drafts, a question scan only research.
func (s *brainstormScanResults) validate(data []byte) (brainstorm.RunResult, agentresult.Diagnostics) {
	if s.purpose == brainstorm.PurposeDrafts {
		return brainstorm.ValidateDraftsResult(data)
	}
	return brainstorm.ValidateScanResult(data)
}

func (s *brainstormScanResults) accepted(ctx context.Context, commit string) (brainstorm.RunResult, error) {
	_, raw, err := s.db.ReadBrainstormScanResult(ctx, s.binding, "")
	if err != nil {
		return brainstorm.RunResult{}, err
	}
	result, d := s.validate(raw)
	if !d.Valid {
		return brainstorm.RunResult{}, errors.New("retained brainstorm scan result is invalid")
	}
	result.Commit = commit
	return result, nil
}

func (s *brainstormScanResults) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	method, data := q.Method, q.Payload
	if method == "tools/list" {
		schema, err := agentresult.JSONSchema()
		if err != nil {
			return nil, err
		}
		receipt := json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)
		return json.Marshal(map[string]any{"tools": []submission.ToolSpec{{Name: "communication_preflight", Description: "Validate this brainstorm scan result", InputSchema: schema}, {Name: "communication_submit", Description: "Retain this brainstorm scan result", InputSchema: schema}, {Name: "communication_receipt", Description: "Recover this brainstorm scan receipt", InputSchema: receipt}}})
	}
	if method == "tools/call" {
		call, err := submission.DecodeToolCall(data)
		if err != nil {
			return nil, err
		}
		method, data = call.Name, call.Arguments
	}
	switch method {
	case "communication_preflight", "preflight":
		if err := s.db.CheckBrainstormScanResultWriter(ctx, s.binding); err != nil {
			return nil, err
		}
		_, d := s.validate(data)
		return json.Marshal(d)
	case "communication_submit", "submit":
		if _, d := s.validate(data); !d.Valid {
			return nil, errors.New("brainstorm scan result is invalid; correct it with communication_preflight")
		}
		replay, err := s.db.SubmitBrainstormScanResult(ctx, s.binding, q.RequestID, data)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"request_id": q.RequestID, "accepted": true, "replay": replay})
	case "communication_receipt", "receipt":
		var input struct {
			RequestID string `json:"request_id"`
		}
		if err := submission.DecodeJSON(data, &input); err != nil || input.RequestID == "" {
			return nil, errors.New("invalid brainstorm scan receipt identity")
		}
		id, _, err := s.db.ReadBrainstormScanResult(ctx, s.binding, input.RequestID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"request_id": id, "accepted": true})
	default:
		return nil, fmt.Errorf("unsupported brainstorm scan method")
	}
}

var _ brainstorm.Runner = (*BrainstormScanRunner)(nil)
