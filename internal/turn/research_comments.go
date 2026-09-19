package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResearchCommentAction names the explicit durable publication activity.
const ResearchCommentAction = "lf_research_comment"

// ResearchCommentActivity publishes only explicit graph comments. Failure, when
// configured, supplies an already-authored application candidate for a failed
// attempt; it must not invoke an agent or change the original process outcome.
type ResearchCommentActivity struct {
	Profile  *graphexec.Profile
	Reporter *Reporter
	Context  func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Failure  func(context.Context, store.GraphNodeAttempt) (agentresult.Result, error)
}

// ResearchCommentSpec declares the closed publication kinds and confirmed outcome.
func ResearchCommentSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: ResearchCommentAction, Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question", "final", "failure"}}}}
}

// RegisterResearchComment installs the validated specification and its publication handler.
func RegisterResearchComment(specs *graphexec.Registry, activities *ActivityRegistry, h ResearchCommentActivity) error {
	if specs == nil || activities == nil || h.Profile == nil || h.Reporter == nil || h.Context == nil {
		return errors.New("research comment requires pinned profile, reporter and trusted context")
	}
	if _, ok := activities.Resolve(ResearchCommentAction); ok {
		return errors.New("research comment already registered")
	}
	if err := specs.Register(ResearchCommentSpec()); err != nil {
		return err
	}
	return activities.Register(ResearchCommentAction, h)
}

// Execute publishes the validated retained candidate bound to this reserved graph activity.
func (h ResearchCommentActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Reporter == nil || h.Reporter.db != opts.DB || h.Profile == nil || h.Context == nil || req.Mode != agent.ModeResearch {
		return ActivityResult{}, errors.New("research comment requires factory Research context")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != ResearchCommentAction || definition.Digest != h.Profile.Digest || definition.Mode != "research" {
		return ActivityResult{}, errors.New("research comment profile binding mismatch")
	}
	current, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if current.InstanceID != instance.ID || current.Node != req.Node || current.Status != store.GraphRunning || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("research comment attempt binding mismatch")
	}
	var source store.GraphNodeAttempt
	for _, n := range h.Profile.Nodes {
		if n.Action != ResearchAgentAction {
			continue
		}
		a, e := opts.DB.LatestGraphNodeAttempt(ctx, instance.ID, n.Name)
		if errors.Is(e, store.ErrNotFound) {
			continue
		}
		if e != nil {
			return ActivityResult{}, e
		}
		if a.ID > source.ID {
			source = a
		}
	}
	if source.ID == 0 {
		return ActivityResult{}, errors.New("research comment has no source attempt")
	}
	scope, err := h.Context(ctx, source)
	if err != nil {
		return ActivityResult{}, err
	}
	kind := node.Attrs["lf_kind"]
	var candidate agentresult.Result
	if kind == "failure" {
		if h.Failure == nil || source.Status != store.GraphFailed {
			return ActivityResult{}, errors.New("research failure comment requires retained failed-attempt candidate")
		}
		candidate, err = h.Failure(ctx, source)
	} else {
		var output agentresult.ResearchNode
		output, err = LoadResearchNodeOutput(ctx, opts.DB, source.ID, scope)
		candidate = output.Result
	}
	if err != nil {
		return ActivityResult{}, err
	}
	posted, key, err := h.Reporter.ReportResearchCandidate(ctx, source, req.Node, kind, candidate, scope)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	payload, err := json.Marshal(struct {
		DeliveryKey     string `json:"delivery_key"`
		CommentID       int64  `json:"comment_id"`
		SourceAttemptID int64  `json:"source_attempt_id"`
	}{key, posted.CommentID, source.ID})
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: payload}, nil
}

// ReportResearchCandidate retains exact authored bytes before rendering. The
// content digest is part of the operation identity: corrections cannot overwrite
// or replay an earlier uncertain effect. No run finalization or git save occurs.
func (r *Reporter) ReportResearchCandidate(ctx context.Context, source store.GraphNodeAttempt, node, kind string, candidate agentresult.Result, scope agentresult.Context) (Posted, string, error) {
	if scope.Mode != "research" {
		return Posted{}, "", errors.New("research comment context has different mode")
	}
	d := agentresult.ValidateResult(candidate, scope)
	if !d.Valid || !d.Complete {
		return Posted{}, "", resultDiagnosticError(d)
	}
	switch kind {
	case "question":
		if candidate.Outcome != "incomplete" || len(candidate.Questions) == 0 || source.Status != store.GraphSucceeded {
			return Posted{}, "", errors.New("question requires successful incomplete Research with questions")
		}
	case "final":
		if candidate.Outcome != "completed" || len(candidate.Questions) != 0 || source.Status != store.GraphSucceeded {
			return Posted{}, "", errors.New("final requires successful completed Research without questions")
		}
	case "failure":
		if candidate.Outcome == "completed" || source.Status != store.GraphFailed || candidate.Reason == "" {
			return Posted{}, "", errors.New("failure requires failed attempt and actionable incomplete candidate")
		}
	default:
		return Posted{}, "", errors.New("unknown Research comment kind")
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return Posted{}, "", err
	}
	if len(raw) > agentresult.MaxResearchNodeBytes {
		return Posted{}, "", errors.New("research comment candidate exceeds bound")
	}
	instance, err := r.db.GetGraphInstance(ctx, source.InstanceID)
	if err != nil {
		return Posted{}, "", err
	}
	run, err := r.db.GetRun(ctx, source.RunID)
	if err != nil {
		return Posted{}, "", err
	}
	session, err := r.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return Posted{}, "", err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return Posted{}, "", err
	}
	sum := sha256.Sum256(raw)
	purpose := fmt.Sprintf("research-comment:%d:%d:%s:%s:%s", source.InstanceID, source.ID, node, kind, hex.EncodeToString(sum[:]))
	intent := r.deliveryIntent(ref, ref, instance.SessionID, run.TriggerCommentID, run.ID, purpose)
	if err := r.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: intent.Key, InstanceID: source.InstanceID, SourceAttemptID: source.ID, Node: node, Kind: kind, Candidate: raw}); err != nil {
		return Posted{}, intent.Key, err
	}
	posted, err := r.deliver(ctx, intent, func() (redact.Body, error) {
		var body strings.Builder
		body.WriteString(candidate.Comment)
		if candidate.Reason != "" {
			body.WriteString("\n\n")
			body.WriteString(candidate.Reason)
		}
		for _, q := range candidate.Questions {
			body.WriteString("\n\n")
			body.WriteString(q.Text)
			for _, o := range q.Options {
				body.WriteString("\n- ")
				body.WriteString(o.Text)
			}
		}
		return r.renderer.RenderReportDelivery(ctx, body.String(), "")
	})
	return posted, intent.Key, err
}

// RecoverResearchComment retries only the original durable publication. It does
// not reserve a graph attempt, invoke a process, save artifacts or alter outcomes.
func (r *Reporter) RecoverResearchComment(ctx context.Context, key string, scope agentresult.Context) (Posted, error) {
	intent, err := r.db.GetGraphComment(ctx, key)
	if err != nil {
		return Posted{}, err
	}
	delivery, err := r.db.GetReportDelivery(ctx, key)
	if err == nil {
		return r.dispatchDelivery(ctx, delivery)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	// A render failure still leaves the candidate durable. Revalidate it under
	// factory context and reconstruct the exact operation, never an agent call.
	var candidate agentresult.Result
	diagnostics := agentresult.Decode(intent.Candidate, &candidate)
	if !diagnostics.Valid || !diagnostics.Complete {
		return Posted{}, resultDiagnosticError(diagnostics)
	}
	source, err := r.db.GetGraphNodeAttempt(ctx, intent.SourceAttemptID)
	if err != nil {
		return Posted{}, err
	}
	posted, recoveredKey, err := r.ReportResearchCandidate(ctx, source, intent.Node, intent.Kind, candidate, scope)
	if recoveredKey != key && err == nil {
		return Posted{}, errors.New("research recovery identity changed")
	}
	return posted, err
}
