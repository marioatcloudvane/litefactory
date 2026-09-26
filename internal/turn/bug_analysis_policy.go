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
	"github.com/cloudvane/litefactory/internal/store"
)

// BugAnalysisPolicy is the concrete analyst capability surface. The later
// parent composition adds fixer/testing/delivery capabilities; this registry
// cannot execute those roles or effects and does not install a parent graph.
type BugAnalysisPolicy struct {
	Profile  *graphexec.Profile
	Analyst  BugAnalystActivity
	Record   BugRecordActivity
	Reporter *Reporter
}

// Register installs only the complete factory-bound analysis capability surface.
func (p BugAnalysisPolicy) Register(specs *graphexec.Registry, activities *ActivityRegistry) error {
	if specs == nil || activities == nil || p.Profile == nil || p.Profile.Workflow != agent.WorkflowBug || p.Profile.Mode != "" || p.Analyst.Resolve == nil || p.Analyst.Prepare == nil || p.Record.Resolve == nil || p.Reporter == nil {
		return errors.New("bug analysis policy needs complete factory bindings")
	}
	for _, node := range p.Profile.Nodes {
		if node.Action == ResearchAgentAction && node.Attrs["lf_agent_markdown"] != "bug-analyst" {
			return errors.New("bug analysis policy refuses non-analyst role")
		}
	}
	p.Record.Profile = p.Profile
	if err := RegisterBugRecord(specs, activities, p.Record); err != nil {
		return err
	}
	if err := specs.Register(graphexec.ActionSpec{Name: ResearchAgentAction, Workflow: agent.WorkflowBug, Outcomes: []string{"success", "failed"}}); err != nil {
		return err
	}
	if err := activities.Register(ResearchAgentAction, p.Analyst); err != nil {
		return err
	}
	control := graphexec.ControlPolicy{Workflow: agent.WorkflowBug, Responses: []string{"ask", "done", "approved", "blocked", "exhausted"}, Defaults: []string{"ask"}, Ask: "ask", Approved: "approved"}
	for _, name := range []string{graphexec.ResearchIfAction, graphexec.ResearchLoopAction} {
		if err := specs.Register(graphexec.ControlSpec(name, control)); err != nil {
			return err
		}
		if err := activities.Register(name, bugAnalysisControl{p.Profile, control}); err != nil {
			return err
		}
	}
	for _, name := range []string{AskAction, ResearchCommentAction} {
		kinds := []string{"notice"}
		if name == AskAction {
			kinds = []string{"question", "review"}
		}
		spec := graphexec.ActionSpec{Name: name, Workflow: agent.WorkflowBug, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: kinds}}}
		if err := specs.Register(spec); err != nil {
			return err
		}
		if err := activities.Register(name, bugAnalysisComment{p.Profile, p.Reporter}); err != nil {
			return err
		}
	}
	return graphexec.Validate(p.Profile, *specs)
}

// bugAnalysisSource requires an explicit successful record node with references
// matching current canonical state. A stale source or arbitrary agent response
// cannot select a branch or approve a review.
func bugAnalysisSource(ctx context.Context, opts Options, req ActivityRequest, profile *graphexec.Profile) (store.GraphInstance, store.BugExecution, store.BugAnalysisRevision, *store.BugAnalysisApproval, error) {
	var instance store.GraphInstance
	var state store.BugExecution
	var revision store.BugAnalysisRevision
	fail := func(err error) (store.GraphInstance, store.BugExecution, store.BugAnalysisRevision, *store.BugAnalysisApproval, error) {
		return instance, state, revision, nil, err
	}
	if opts.DB == nil || profile == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return fail(errors.New("bug analysis activity missing workflow binding"))
	}
	var err error
	instance, err = opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return fail(err)
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return fail(err)
	}
	node, ok := profile.Node(req.Node)
	if !ok || definition.Workflow != "bug" || definition.Mode != "" || definition.Digest != profile.Digest || instance.CurrentNode != req.Node {
		return fail(errors.New("bug analysis profile mismatch"))
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return fail(err)
	}
	if attempt.InstanceID != instance.ID || attempt.Node != req.Node || attempt.Status != store.GraphRunning {
		return fail(errors.New("bug analysis attempt mismatch"))
	}
	sourceNode, ok := profile.Node(node.Attrs["lf_source"])
	if !ok || sourceNode.Action != BugRecordAction {
		return fail(errors.New("bug analysis requires explicit record source"))
	}
	source, err := opts.DB.LatestGraphNodeAttempt(ctx, instance.ID, sourceNode.Name)
	if err != nil {
		return fail(err)
	}
	if source.Status != store.GraphSucceeded {
		return fail(errors.New("bug record source did not succeed"))
	}
	outputs, err := opts.DB.ListGraphOutputsBounded(ctx, source.ID, 1<<20, 16)
	if err != nil {
		return fail(err)
	}
	var bound *BugRecordOutput
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		if bound != nil {
			return fail(errors.New("bug record source ambiguous"))
		}
		bound = &BugRecordOutput{}
		if err = json.Unmarshal(output.Payload, bound); err != nil {
			return fail(err)
		}
	}
	var approval *store.BugAnalysisApproval
	state, revision, approval, err = opts.DB.CurrentBugAnalysis(ctx, instance.SessionID)
	if err != nil {
		return fail(err)
	}
	// Approval changes execution revision mechanically but preserves exact
	// analysis. Record source identity pins that immutable scope, not a budget.
	if bound == nil || bound.SessionID != instance.SessionID || bound.AnalysisRevision != revision.Revision || bound.AnalysisDigest != revision.Digest {
		return fail(errors.New("bug record source is stale"))
	}
	return instance, state, revision, approval, nil
}

type bugAnalysisControl struct {
	profile *graphexec.Profile
	policy  graphexec.ControlPolicy
}

func (h bugAnalysisControl) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	_, state, revision, approval, err := bugAnalysisSource(ctx, opts, req, h.profile)
	if err != nil {
		return ActivityResult{}, err
	}
	node, _ := h.profile.Node(req.Node)
	condition, err := graphexec.DecodeCondition(node.Action, node.Attrs, h.policy)
	if err != nil {
		return ActivityResult{}, err
	}
	response := revision.Response
	if approval != nil {
		response = "approved"
	} else if !revision.ReviewReady && state.AnalysisUsed >= 8 {
		response = "exhausted"
	}
	outcome, err := condition.Evaluate(response, revision.Revision != 0)
	return ActivityResult{Outcome: outcome}, err
}

type bugAnalysisComment struct {
	profile  *graphexec.Profile
	reporter *Reporter
}

func (h bugAnalysisComment) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	instance, state, revision, _, err := bugAnalysisSource(ctx, opts, req, h.profile)
	if err != nil {
		return ActivityResult{}, err
	}
	if h.reporter == nil || h.reporter.db != opts.DB {
		return ActivityResult{}, errors.New("bug analysis reporter missing")
	}
	node, _ := h.profile.Node(req.Node)
	kind := node.Attrs["lf_kind"]
	if (kind == "review" && !revision.ReviewReady) || (kind == "question" && revision.Response != "ask") || (kind != "question" && kind != "review" && kind != "notice") {
		return ActivityResult{}, errors.New("bug analysis notice does not match recorded outcome")
	}
	if (kind == "notice") != (node.Action == ResearchCommentAction) {
		return ActivityResult{}, errors.New("bug analysis wait kind mismatch")
	}
	pass, err := opts.DB.GetBugAnalysisPass(ctx, instance.SessionID, revision.Invocation)
	if err != nil {
		return ActivityResult{}, err
	}
	source, err := opts.DB.GetGraphNodeAttempt(ctx, pass.GraphAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return ActivityResult{}, err
	}
	run, err := opts.DB.GetRun(ctx, source.RunID)
	if err != nil {
		return ActivityResult{}, err
	}
	raw, err := json.MarshalIndent(revision.Analysis, "", "  ")
	if err != nil {
		return ActivityResult{}, err
	}
	// JSON indentation keeps every line a literal owner-facing projection.
	body := fmt.Sprintf("Bug analysis revision %d (%s)\n\n", revision.Revision, revision.Digest)
	for _, line := range strings.Split(string(raw), "\n") {
		body += "    " + line + "\n"
	}
	body += "\n" + revision.Reason
	if kind == "review" {
		body += fmt.Sprintf("\n\nTo authorize this exact remedy, reply `/approve-bug analysis %d`.", revision.Revision)
	}
	purpose := fmt.Sprintf("bug-analysis:%d:%s:%s:%d", instance.ID, req.Node, kind, revision.Revision)
	intent := h.reporter.deliveryIntent(ref, ref, instance.SessionID, run.TriggerCommentID, run.ID, purpose)
	rendered, err := h.reporter.renderer.RenderReportDelivery(ctx, body, intent.Marker)
	if err != nil {
		return ActivityResult{}, err
	}
	if rendered.Truncated() {
		return ActivityResult{}, errors.New("bug analysis projection exceeds report bound")
	}
	intent.Body = rendered.String()
	candidate, err := json.Marshal(revision)
	if err != nil {
		return ActivityResult{}, err
	}
	storedKind := kind
	if kind == "notice" {
		storedKind = "final"
	}
	if err = opts.DB.RetainGraphComment(ctx, store.GraphCommentIntent{Key: intent.Key, InstanceID: instance.ID, SourceAttemptID: source.ID, Node: req.Node, Kind: storedKind, Candidate: candidate}); err != nil {
		return ActivityResult{}, err
	}
	reviewID := "analysis-" + intent.Key
	if kind == "review" {
		sum := sha256.Sum256([]byte(intent.Body))
		err = opts.DB.RetainBugAnalysisReview(ctx, store.BugAnalysisReview{SessionID: instance.SessionID, Review: reviewID, AnalysisRevision: revision.Revision, AnalysisDigest: revision.Digest, DeliveryKey: intent.Key, ContentDigest: hex.EncodeToString(sum[:])}, state.Revision)
		if err != nil {
			return ActivityResult{}, err
		}
	}
	saved, err := opts.DB.EnsureReportDelivery(ctx, intent)
	if err != nil {
		return ActivityResult{}, err
	}
	posted, err := h.reporter.dispatchDelivery(ctx, saved)
	if err != nil {
		return ActivityResult{}, err
	}
	if kind == "review" {
		if _, err = opts.DB.ConfirmBugAnalysisReview(ctx, instance.SessionID, reviewID, h.reporter.botUserID); err != nil {
			return ActivityResult{}, err
		}
	}
	payload, err := json.Marshal(struct {
		DeliveryKey string `json:"delivery_key"`
		CommentID   int64  `json:"comment_id"`
	}{intent.Key, posted.CommentID})
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: payload}, err
}

// BugWorkflowPolicy registers Bug's strict shared-result contract. The complete
// parent package supplies execution bindings separately through BugAnalysisPolicy;
// this descriptor does not admit an otherwise unavailable parent workflow.
func BugWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Workflow: agent.WorkflowBug,
		Control:  graphexec.ControlPolicy{Workflow: agent.WorkflowBug, Responses: []string{"ask", "done", "approved", "blocked"}, Defaults: []string{"ask"}, Ask: "ask", Approved: "approved"},
		Decode:   agentresult.DecodeBugNode,
		Retain:   RetainBugNode,
		Guidance: "Bug analyst results use the version-1 shared result helper and service: preflight, correct diagnostics, submit and read the matching receipt. Lost replies retain request identity and identical bytes; corrected content needs a new request. Terminal output only confirms the accepted result. Only factory-authenticated exact owner approval authorizes repair.",
	}
}
