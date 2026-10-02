package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// BugParentPolicy composes the seven domain activities without acquiring their
// authority. All capability bindings are factory supplied, never package data.
type BugParentPolicy struct {
	Profile  *graphexec.Profile
	Analyst  BugAnalystActivity
	Fixer    BugFixerActivity
	Record   BugRecordActivity
	Attempt  BugAttemptActivity
	Tests    *BugTestGraphActivity
	PR       *BugPRActivity
	Merge    *BugMergeActivity
	Close    *BugCloseActivity
	Reporter *Reporter
	// RepairRecord reconciles the successful fixer and immutable factory save.
	// It returns done, reshape or blocked; receipts alone are not completion.
	RepairRecord func(context.Context, Options, ActivityRequest) (ActivityResult, error)
}

func bugParentControlPolicy() graphexec.ControlPolicy {
	return graphexec.ControlPolicy{Workflow: agent.WorkflowBug, Responses: []string{"ask", "done", "approved", "blocked", "exhausted", "reshape"}, Defaults: []string{"ask"}, Ask: "ask", Approved: "approved"}
}

// BugParentSpecs supplies fixed contracts for package capture before handlers exist.
func BugParentSpecs() (graphexec.Registry, error) {
	r := graphexec.Registry{Policy: func(p *graphexec.Profile) error {
		if p.Start == "regression" {
			return teamsource.ValidateBugTests(p)
		}
		return teamsource.ValidateBugParent(p)
	}}
	specs := []graphexec.ActionSpec{BugRecordSpec(), BugAttemptSpec(), BugTestGraphSpec(), BugChecksSpec(), BugPRSpec(), BugMergeSpec(), BugCloseSpec(), {Name: ResearchAgentAction, Workflow: agent.WorkflowBug, Outcomes: []string{"success", "failed"}}}
	for _, n := range []string{graphexec.ResearchIfAction, graphexec.ResearchLoopAction} {
		specs = append(specs, graphexec.ControlSpec(n, bugParentControlPolicy()))
	}
	specs = append(specs, graphexec.ActionSpec{Name: AskAction, Workflow: agent.WorkflowBug, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question", "review", "delivery"}}}}, graphexec.ActionSpec{Name: ResearchCommentAction, Workflow: agent.WorkflowBug, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"notice"}}}})
	for _, s := range specs {
		if e := r.Register(s); e != nil {
			return r, e
		}
	}
	return r, nil
}

// Register binds the complete fixed Bug package to concrete factory handlers.
func (p BugParentPolicy) Register(specs *graphexec.Registry, activities *ActivityRegistry) error {
	if specs == nil || activities == nil || p.Profile == nil || p.Analyst.Resolve == nil || p.Analyst.Prepare == nil || p.Fixer.Resolve == nil || p.Fixer.Prepare == nil || p.Record.Resolve == nil || p.Attempt.Resolve == nil || p.Tests == nil || p.PR == nil || p.Merge == nil || p.Close == nil || p.Reporter == nil || p.RepairRecord == nil {
		return errors.New("bug parent requires every concrete factory binding")
	}
	specs.Policy = teamsource.ValidateBugParent
	if e := teamsource.ValidateBugParent(p.Profile); e != nil {
		return e
	}
	// Domain registrars perform their own concrete binding checks before exposing handlers.
	p.Record.Profile = p.Profile
	p.Attempt.Profile = p.Profile
	p.PR.Profile = p.Profile
	p.Merge.Profile = p.Profile
	p.Close.Profile = p.Profile
	p.PR.Publish = p.publishReview
	for _, register := range []func() error{
		func() error { return RegisterBugRecord(specs, activities, p.Record) }, func() error { return RegisterBugAttempt(specs, activities, p.Attempt) }, func() error { return RegisterBugTestGraph(specs, activities, p.Tests) }, func() error { return RegisterBugPR(specs, activities, p.PR) }, func() error { return RegisterBugMerge(specs, activities, p.Merge) }, func() error { return RegisterBugClose(specs, activities, p.Close) },
	} {
		if e := register(); e != nil {
			return e
		}
	}
	var generic []graphexec.ActionSpec
	generic = append(generic, graphexec.ActionSpec{Name: ResearchAgentAction, Workflow: agent.WorkflowBug, Outcomes: []string{"success", "failed"}})
	for _, n := range []string{graphexec.ResearchIfAction, graphexec.ResearchLoopAction} {
		generic = append(generic, graphexec.ControlSpec(n, bugParentControlPolicy()))
	}
	generic = append(generic, graphexec.ActionSpec{Name: AskAction, Workflow: agent.WorkflowBug, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question", "review", "delivery"}}}}, graphexec.ActionSpec{Name: ResearchCommentAction, Workflow: agent.WorkflowBug, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"notice"}}}})
	for _, s := range generic {
		if e := specs.Register(s); e != nil {
			return e
		}
		if e := activities.Register(s.Name, bugParentActivity{p}); e != nil {
			return e
		}
	}
	return graphexec.Validate(p.Profile, *specs)
}

type bugParentActivity struct{ p BugParentPolicy }

func (h bugParentActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	node, ok := h.p.Profile.Node(req.Node)
	if !ok || opts.DB == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("invalid Bug parent dispatch")
	}
	instance, e := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if e != nil {
		return ActivityResult{}, e
	}
	def, e := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if e != nil {
		return ActivityResult{}, e
	}
	attempt, e := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if e != nil {
		return ActivityResult{}, e
	}
	if def.Digest != h.p.Profile.Digest || def.Workflow != "bug" || attempt.InstanceID != instance.ID || attempt.Node != req.Node || attempt.Status != store.GraphRunning || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("bug parent source binding mismatch")
	}
	switch node.Action {
	case ResearchAgentAction:
		switch node.Attrs["lf_agent_markdown"] {
		case "bug-analyst":
			return h.p.Analyst.Execute(ctx, opts, req)
		case "bug-fixer":
			return h.p.Fixer.Execute(ctx, opts, req)
		}
	case graphexec.ResearchIfAction, graphexec.ResearchLoopAction:
		if req.Node == "record_repair" || req.Node == "fixer_reshape" {
			result, e := h.p.RepairRecord(ctx, opts, req)
			if e != nil {
				return ActivityResult{}, e
			}
			if result.Outcome != "done" && result.Outcome != "reshape" && result.Outcome != "blocked" {
				return ActivityResult{}, errors.New("invalid retained fixer handoff outcome")
			}
			condition, e := graphexec.DecodeCondition(node.Action, node.Attrs, bugParentControlPolicy())
			if e != nil {
				return ActivityResult{}, e
			}
			outcome, e := condition.Evaluate(result.Outcome, true)
			return ActivityResult{Outcome: outcome, Kind: result.Kind, Output: result.Output}, e
		}
		return (bugAnalysisControl{h.p.Profile, bugParentControlPolicy()}).Execute(ctx, opts, req)
	case AskAction:
		if node.Attrs["lf_kind"] == "delivery" {
			return h.publishDelivery(ctx, opts, req, instance)
		}
		out, e := (bugAnalysisComment{h.p.Profile, h.p.Reporter}).Execute(ctx, opts, req)
		if e == nil {
			kind := "question"
			if node.Attrs["lf_kind"] == "review" {
				kind = "analysis-review"
			}
			e = opts.DB.RecordBugLifecycleDisposition(ctx, instance.SessionID, instance.ID, req.Node, kind, string(out.Output))
		}
		return out, e
	case ResearchCommentAction:
		return h.publishNotice(ctx, opts, req, instance)
	}
	return ActivityResult{}, fmt.Errorf("unsupported Bug parent action %s", node.Action)
}

// bugParentOutput reads exactly one JSON document from the explicit successful source.
func bugParentOutput(ctx context.Context, db *store.DB, instance int64, node string, out any) (store.GraphNodeAttempt, error) {
	a, e := db.LatestGraphNodeAttempt(ctx, instance, node)
	if e != nil {
		return a, e
	}
	if a.Status != store.GraphSucceeded {
		return a, errors.New("bug parent source did not succeed")
	}
	values, e := db.ListGraphOutputsBounded(ctx, a.ID, 1<<20, 16)
	if e != nil {
		return a, e
	}
	count := 0
	for _, v := range values {
		if v.Kind == store.GraphOutputJSON {
			count++
			if e = json.Unmarshal(v.Payload, out); e != nil {
				return a, e
			}
		}
	}
	if count != 1 {
		return a, errors.New("bug parent needs one retained source document")
	}
	return a, nil
}
