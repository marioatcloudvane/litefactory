package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// JudgeActivity is a thin workflow caller of the shared advisory service.
// Context supplies factory-owned evidence and captured role resources.
type JudgeActivity struct {
	Profile *graphexec.Profile
	Service *JudgeService
	Context func(context.Context, ActivityRequest) (JudgeRequest, error)
}

type workflowJudgeOutput struct {
	RequestID    int64  `json:"request_id,omitempty"`
	ReportDigest string `json:"report_digest,omitempty"`
	Execution    string `json:"execution"`
	Failure      string `json:"failure,omitempty"`
}

func judgeDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Execute pins the explicit source and returns only a private advisory reference.
func (h JudgeActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Service == nil || h.Service.DB != opts.DB || h.Service.Redactor == nil || h.Service.Config == nil || (req.Mode != agent.ModeResearch && req.Mode != agent.ModePlan) {
		return ActivityResult{}, errors.New("workflow judge requires shared service and Research or Plan context")
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != graphexec.JudgeAction || node.Limit <= 0 {
		return ActivityResult{}, errors.New("workflow judge profile binding mismatch")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	current, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if pin.Mode != string(req.Mode) || pin.Team != h.Profile.Team || (req.Mode != agent.ModeResearch && pin.Digest != h.Profile.Digest) || current.InstanceID != instance.ID || current.Node != node.Name || current.Status != store.GraphRunning || instance.CurrentNode != node.Name {
		return ActivityResult{}, errors.New("workflow judge attempt binding mismatch")
	}
	fail := func(cause error) (ActivityResult, error) {
		body, err := h.Service.Redactor.Apply(context.WithoutCancel(ctx), "Unable to assess saved work: "+cause.Error(), 8192)
		if err != nil {
			return ActivityResult{}, err
		}
		raw, err := json.Marshal(workflowJudgeOutput{Execution: "failed", Failure: body.String()})
		return ActivityResult{Outcome: "failed", Kind: store.GraphOutputJSON, Output: raw}, err
	}
	// Recovery reads the durable binding first, before looking at newer source work.
	saved, err := opts.DB.FindWorkflowJudgeRequest(ctx, instance.ID, current.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	if req.Recovery && errors.Is(err, store.ErrNotFound) {
		return fail(errors.New("reserved judge attempt was abandoned before its request was retained; no assessment was rerun"))
	}
	var input JudgeRequest
	if h.Context != nil {
		input, err = h.Context(ctx, req)
	} else {
		input, err = workflowJudgeContext(ctx, opts, req)
	}
	if err != nil {
		return ActivityResult{}, err
	}
	input.GraphInstanceID, input.JudgeNodeAttemptID = instance.ID, current.ID
	input.SourceNode = node.Attrs["lf_source"]
	input.RemoteCommentID = fmt.Sprintf("workflow:%d:%d", instance.ID, current.ID)
	input.Options = JudgeCommandOptions{Focus: node.Attrs["lf_focus"], JudgeOptions: config.JudgeOptions{Agent: node.Attrs["lf_agent"], Model: node.Attrs["lf_model"], Thinking: config.Thinking(node.Attrs["lf_thinking"])}}
	minutes, present, err := graphexec.AgentBudget(node.Attrs)
	if err != nil {
		return fail(err)
	}
	if present {
		input.Budget = time.Duration(minutes) * time.Minute
	}
	if saved.ID != 0 {
		input.PinnedSource, input.SourceNodeAttemptID = &saved.Source, saved.SourceNodeAttemptID
		input.TicketKey, input.RemoteCommentID = saved.TicketKey, saved.CommentID
	} else {
		sourceNode, ok := h.Profile.Node(input.SourceNode)
		if !ok || sourceNode.Action != ResearchAgentAction {
			return fail(errors.New("explicit judge source must name an agent node"))
		}
		source, attempt, err := opts.DB.ResolveWorkflowJudgeSource(ctx, instance.ID, input.SourceNode)
		if err != nil {
			return fail(err)
		}
		input.PinnedSource, input.SourceNodeAttemptID = &source, attempt.ID
	}
	if input.TaskProvider == "" {
		input.TaskProvider = config.Kind(input.PinnedSource.Provider)
	}
	_, err = h.Service.Execute(ctx, input)
	if err != nil {
		if errors.Is(err, ErrJudgeSettings) || errors.Is(err, store.ErrJudgeSourceUnavailable) {
			return fail(err)
		}
		return ActivityResult{}, err
	}
	retained, err := opts.DB.FindWorkflowJudgeRequest(context.WithoutCancel(ctx), instance.ID, current.ID)
	if err != nil {
		return ActivityResult{}, err
	}
	var report judgeSavedReport
	if json.Unmarshal(retained.Report, &report) != nil || report.Execution == "" {
		return ActivityResult{}, errors.New("workflow judge lacks a durable report")
	}
	output := workflowJudgeOutput{RequestID: retained.ID, ReportDigest: judgeDigest(retained.Report), Execution: report.Execution}
	outcome := "failed"
	if report.Execution == "completed" {
		outcome = "success"
	}
	raw, err := json.Marshal(output)
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}

func workflowJudgeContext(ctx context.Context, opts Options, req ActivityRequest) (JudgeRequest, error) {
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return JudgeRequest{}, err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return JudgeRequest{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return JudgeRequest{}, err
	}
	input := JudgeRequest{Ref: ref, SessionID: session.ID, TicketKey: session.TaskRef, Root: session.WorktreePath}
	if req.RunID != 0 {
		run, err := opts.DB.GetRun(ctx, req.RunID)
		if err != nil {
			return JudgeRequest{}, err
		}
		input.CommentID, input.TaskProvider = run.TriggerCommentID, config.Kind(run.Agent)
	}
	segments, err := opts.DB.ListSegments(ctx, session.ID)
	if err != nil {
		return JudgeRequest{}, err
	}
	if len(segments) > 0 {
		input.TaskProvider = config.Kind(segments[len(segments)-1].Agent)
	}
	comments, err := opts.DB.ListComments(ctx, session.ID)
	if err != nil {
		return JudgeRequest{}, err
	}
	if opts.MCP != nil && opts.MCP.Now != nil {
		input.Cutoff = opts.MCP.Now()
	}
	for _, c := range comments {
		if !input.Cutoff.IsZero() && c.CreatedAt.After(input.Cutoff) {
			continue
		}
		input.Comments = append(input.Comments, JudgeComment{ID: strconv.FormatInt(c.ForgeCommentID, 10), Body: c.Body})
	}
	return input, nil
}
