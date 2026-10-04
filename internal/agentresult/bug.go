package agentresult

import (
	"encoding/json"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/bugrepair"
)

// BugBinding is factory-owned invocation context. The factory knows which
// invocation and revision a result belongs to; agents never echo them back.
type BugBinding struct {
	GraphInstanceID  int64
	Role, Invocation string
	BaseRevision     int64
	ApprovedRevision int64
}

// Bug carries an analysis proposal or a repair report. No execution counters,
// permissions, recipes, approvals or delivery state are writable.
type Bug struct {
	Repair   *bugrepair.Evidence      `json:"repair,omitempty"`
	Response string                   `json:"response"`
	Analysis *buganalysis.BugAnalysis `json:"analysis,omitempty"`
	Evidence []string                 `json:"evidence,omitempty"`
	// Invocation and BaseRevision are accepted for older agents and ignored.
	Invocation   string `json:"invocation,omitempty"`
	BaseRevision int64  `json:"base_revision,omitempty"`
}

func validateBug(r Result, ctx Context, d *Diagnostics) {
	p := r.Payload.Bug
	if ctx.Bug != nil && ctx.Bug.Role == "bug-fixer" {
		validateBugRepair(r, ctx, d)
		return
	}
	if p.Repair != nil {
		add(d, "$.result.payload.bug.repair", "forbidden", "analysis only", "repair present", "The analyst cannot submit repair evidence.")
	}
	if ctx.Workflow != agent.WorkflowBug || ctx.Mode != "" || ctx.Bug == nil || ctx.Bug.Role != "bug-analyst" || ctx.Bug.Invocation == "" {
		add(d, "$.result.payload.bug", "scope", "factory-bound Bug analyst invocation", "missing or different binding", "Use the current factory analyst binding.")
		return
	}
	if r.Recommendation != nil {
		add(d, "$.result.recommendation", "forbidden", "no mode recommendation", "present", "Bug stays in its admitted workflow.")
	}
	switch p.Response {
	case "ask":
		if r.Outcome != "incomplete" || len(r.Questions) == 0 || p.Analysis == nil || len(p.Analysis.Questions) == 0 {
			add(d, "$.result.payload.bug.response", "consistency", "incomplete ask with retained partial analysis and batched questions", "mismatch", "Retain partial findings and the material question batch.")
		}
		if p.Analysis != nil {
			questions := make(map[string]string)
			for _, q := range p.Analysis.Questions {
				questions[q.ID] = q.Text
			}
			if len(questions) != len(r.Questions) {
				add(d, "$.result.questions", "consistency", "same retained question batch", "mismatch", "Display exactly the retained questions.")
			}
			for i, q := range r.Questions {
				if questions[q.ID] != q.Text {
					add(d, fmt.Sprintf("$.result.questions[%d]", i), "consistency", "matching question ID and text", "mismatch", "Display exactly the retained question.")
				}
			}
		}
	case "done":
		if r.Outcome != "completed" || len(r.Questions) != 0 || p.Analysis == nil {
			add(d, "$.result.payload.bug.response", "consistency", "complete reviewable analysis without questions", "mismatch", "Done presents analysis for owner review; it grants no repair.")
		}
	case "blocked":
		if r.Outcome != "blocked" || len(p.Evidence) == 0 || len(r.Questions) != 0 {
			add(d, "$.result.payload.bug.response", "consistency", "blocked outcome with obstacle and evidence", "mismatch", "Retain the obstacle and evidence; ask separately for owner questions.")
		}
	default:
		add(d, "$.result.payload.bug.response", "enum", "ask, done or blocked", "unsupported response", "Use the analyst response contract.")
	}
	if p.Analysis != nil {
		// Notices are policy-class observations the authoritative store write
		// records as facts (internal/store/bug_analyses.go); this consistency
		// pass only judges whether the submission decodes, so only a hard
		// fault is surfaced here.
		if _, err := p.Analysis.Validate(p.Response == "done"); err != nil {
			add(d, "$.result.payload.bug.analysis", "content", "valid analysis snapshot", "invalid", err.Error())
		}
	}
	for i, e := range p.Evidence {
		required(d, fmt.Sprintf("$.result.payload.bug.evidence[%d]", i), e)
	}
}

// DecodeBugNode judges terminal consistency only; AgentActivity still requires
// retained shared-service input. There is deliberately no prose extraction.
func DecodeBugNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	var node ResearchNode
	d := Decode(data, &node)
	if !d.Valid {
		return ResearchNode{}, d
	}
	if node.Version != 1 {
		add(&d, "$.version", "version", "1", "unsupported", "Use version 1.")
	}
	mergeDiagnostics(&d, ValidateResult(node.Result, ctx), "$")
	if node.Result.Payload.Bug == nil || node.Response != node.Result.Payload.Bug.Response {
		add(&d, "$.response", "consistency", "response matching retained Bug result", "mismatch", "Confirm the exact accepted result and response.")
	}
	// Create shares presence-sensitive validation with every application route.
	b, err := json.Marshal(CreateInput{Result: node.Result})
	if err != nil {
		add(&d, "$", "encoding", "JSON", "invalid", "Correct the result.")
	} else {
		_, checked := Create(b, ctx)
		mergeDiagnostics(&d, checked, "$")
	}
	finish(&d)
	if !d.Valid {
		return ResearchNode{}, d
	}
	return node, d
}

func resultWorkflowMatches(meta Metadata, ctx Context) bool {
	left, err := agent.ResolveWorkflow(meta.Workflow, agent.Mode(meta.Mode))
	if err != nil {
		return false
	}
	right, err := agent.ResolveWorkflow(ctx.Workflow, agent.Mode(ctx.Mode))
	return err == nil && left == right
}
