package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// The Spike graph's own steps. Each does one measurable thing and returns an
// outcome; the DOT file, never this package, decides what follows it.
const (
	SpikeNoteAction  = "lf_spike_note"
	SpikeGateAction  = "lf_spike_gate"
	SpikeCheckAction = "lf_spike_check"
	SpikeIssueAction = "lf_spike_issue"
)

// SpikeCheckOutcomes are the only outcomes the check can return.
var SpikeCheckOutcomes = []string{"passed", "failed", "too_big", "blocked"}

// SpikeIssueOutcomes are the only outcomes issue filing can return.
var SpikeIssueOutcomes = []string{"filed", "failed"}

// spikePhases is the closed list of recipe phases a graph may name.
var spikePhases = []string{"regression", "focused", "full"}

// SpikeGateOutcomes are the only outcomes the gate can return.
var SpikeGateOutcomes = []string{"none", "small", "too_big", "blocked"}

// spikeForbidKinds are the change kinds a graph may forbid.
var spikeForbidKinds = []string{"package", "dependency", "schema", "api"}

// SpikeWorkflowPolicy supplies Spike's response words, result decoder, report
// renderer and failure text to the shared graph mechanisms. It carries no flow:
// order, branches, limits, thresholds and recorded statuses are declared in the
// graph file, and the generic control validator is the only structural check.
func SpikeWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Mode:            agent.ModeSpike,
		Control:         SpikeControlPolicy(),
		Specs:           []graphexec.ActionSpec{SpikeNoteSpec(), SpikeGateSpec(), SpikeCheckSpec(), SpikeIssueSpec()},
		RegisterEffects: registerSpikeEffects,
		Decode:          agentresult.DecodeSpikeNode,
		Guidance:        spikeNodeGuidance(),
		Contract:        validateSpikeComment,
		Render:          renderSpikeComment,
		NextStep:        spikeNextStep,
		Failure:         spikeFailureCandidate,
		Annotate:        annotateSpike,
		Handoff:         spikeHandoff,
		ResultPerNode:   true,
		Questions:       true,
		DefaultAccess:   "read",
	}
}

// SpikeControlPolicy is Spike's closed response vocabulary: ask or done, never
// approved. Its measure actions may sit on an answer path.
func SpikeControlPolicy() graphexec.ControlPolicy {
	return graphexec.ControlPolicy{Mode: agent.ModeSpike, Responses: []string{"ask", "done"}, Defaults: []string{"ask", "done"}, Ask: "ask", MeasureActions: []string{SpikeNoteAction, SpikeGateAction, SpikeCheckAction, SpikeIssueAction}, FindingActions: []string{SpikeCheckAction, SpikeIssueAction}}
}

// SpikeSpecs is the closed registry for prelaunch built-in asset validation.
func SpikeSpecs() (graphexec.Registry, error) {
	return WorkflowSpecs(SpikeWorkflowPolicy())
}

// SpikeNoteSpec declares the note step and the statuses a graph may record.
func SpikeNoteSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: SpikeNoteAction, Mode: agent.ModeSpike, Outcomes: []string{"success", "failed"}, Attributes: map[string]graphexec.AttributeSpec{"lf_status": {Required: true, Values: slices.Clone(agentresult.SpikeStatuses)}}}
}

// SpikeGateSpec declares the measuring step, its closed file limit and the
// change kinds a graph may forbid.
func SpikeGateSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: SpikeGateAction, Mode: agent.ModeSpike, Outcomes: slices.Clone(SpikeGateOutcomes), Attributes: map[string]graphexec.AttributeSpec{
		"lf_max_files": {Required: true, Values: []string{"1", "2", "3", "4", "5"}},
		"lf_forbid":    {Required: true, List: true, Values: slices.Clone(spikeForbidKinds)},
	}}
}

// SpikeCheckSpec declares the check that measures the real change and runs the
// pinned recipe phases the graph names.
func SpikeCheckSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: SpikeCheckAction, Mode: agent.ModeSpike, Outcomes: slices.Clone(SpikeCheckOutcomes), Attributes: map[string]graphexec.AttributeSpec{
		"lf_max_files": {Required: true, Values: []string{"1", "2", "3", "4", "5"}},
		"lf_forbid":    {Required: true, List: true, Values: slices.Clone(spikeForbidKinds)},
		"lf_phases":    {Required: true, List: true, Values: slices.Clone(spikePhases)},
	}}
}

// SpikeIssueSpec declares the step that files one plain repository issue.
func SpikeIssueSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: SpikeIssueAction, Mode: agent.ModeSpike, Outcomes: slices.Clone(SpikeIssueOutcomes)}
}

func registerSpikeEffects(specs *graphexec.Registry, activities *ActivityRegistry, profile *graphexec.Profile, scope func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), destinations func() (string, string, error), env WorkflowEnv) error {
	for _, spec := range []graphexec.ActionSpec{SpikeNoteSpec(), SpikeGateSpec(), SpikeCheckSpec(), SpikeIssueSpec()} {
		if err := specs.Register(spec); err != nil {
			return err
		}
	}
	for name, handler := range map[string]ActivityHandler{
		SpikeNoteAction:  SpikeNoteActivity{Profile: profile, Context: scope, Destinations: destinations},
		SpikeGateAction:  SpikeGateActivity{Profile: profile, Context: scope, Worktree: env.Worktree},
		SpikeCheckAction: SpikeCheckActivity{Profile: profile, Context: scope, Destinations: destinations, Worktree: env.Worktree},
		SpikeIssueAction: SpikeIssueActivity{Profile: profile, Context: scope, Ref: env.Ref},
	} {
		if err := activities.Register(name, handler); err != nil {
			return err
		}
	}
	return nil
}

const spikeNodeInstructions = "\nFactory invocation: private Spike graph node. Deliver the complete application result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request_id (the shared result helper's preflight, submit and receipt methods are the same route). The accepted submission is this step's result, and the factory derives the step's response from it. Do not print a JSON result: terminal text is never read as a result, so end with a short plain-language note. The result is limited to 1 MiB. Submitting retains private progress only; the factory records the Spike note and publishes the one report.\n"

const spikeNodeHardRules = "\nResult shapes: to ask the owner, once, submit result.outcome incomplete with a nonblank result.reason and a nonempty result.questions, each naming audience business or technical; when the work is done, submit result.outcome completed with no result.questions. Any other shape fails the step. Never include a note status in your result; the factory records it. Read the spike-result skill's node-result resource for the worked result of each shape and the correction contract before you submit.\n"

func spikeNodeGuidance() string { return spikeNodeInstructions + spikeNodeHardRules }

// spikeFailureCandidate authors the owner-facing account of a failed Spike step.
// It claims no findings: the outcome is blocked and the reason carries the cause.
func spikeFailureCandidate(req Request, cause error) agentresult.Result {
	reason := "Spike failed before it produced a reportable result."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = boundedFailureReason(strings.TrimSpace(cause.Error()))
	}
	request := strings.TrimSpace(req.Trigger.Body)
	// A stage the factory continued has no comment of its own to quote.
	if request == "" || store.IsGraphTrigger(req.Trigger.CommentID) {
		request = "The owner's Spike request is unavailable on this attempt."
	}
	return agentresult.Result{
		Outcome: "blocked",
		Comment: "Spike did not finish. No answer is claimed. The reason is recorded below; comment again to start a fresh attempt.",
		Reason:  reason,
		Payload: agentresult.Payload{Spike: &agentresult.Spike{Request: request}},
	}
}

// validateSpikeComment judges a candidate before it is reported. A reportable
// candidate carries the status the factory recorded when it wrote the note; the
// authored contract itself never carries one, so it is checked without it.
func validateSpikeComment(source store.GraphNodeAttempt, kind string, candidate agentresult.Result, scope agentresult.Context) error {
	checked := candidate
	status := ""
	if candidate.Payload.Spike != nil {
		spike := *candidate.Payload.Spike
		status, spike.Status = spike.Status, ""
		spike.Check, spike.Issue = nil, nil
		checked.Payload.Spike = &spike
	}
	if d := agentresult.ValidateResult(checked, scope); !d.Valid || !d.Complete {
		return resultDiagnosticError(d)
	}
	if kind == "failure" {
		if candidate.Outcome == "completed" || source.Status != store.GraphFailed || candidate.Reason == "" || status != "" {
			return errors.New("failure requires failed attempt and actionable incomplete candidate")
		}
		return nil
	}
	if candidate.Payload.Spike == nil || !slices.Contains(agentresult.SpikeStatuses, status) {
		return errors.New("spike report requires a recorded note status")
	}
	if source.Status != store.GraphSucceeded {
		return errors.New("spike report requires a successful source")
	}
	switch kind {
	case "question":
		if candidate.Outcome != "incomplete" || len(candidate.Questions) == 0 || status != "asking" {
			return errors.New("question requires an incomplete Spike with questions and an asking note")
		}
	case "final":
		finished := candidate.Outcome == "completed" && len(candidate.Questions) == 0 && status != "asking"
		// A step that wanted to ask but has nobody to ask is reported as blocked:
		// its questions were kept as work left undone, and none was put to anyone.
		stopped := candidate.Outcome == "incomplete" && len(candidate.Questions) == 0 && candidate.Reason != "" && status == "blocked"
		if !finished && !stopped {
			return errors.New("final requires a completed Spike without questions and a recorded outcome note, or a blocked one that asked nobody")
		}
	default:
		return errors.New("unknown Spike comment kind")
	}
	return nil
}

var spikeStatusLine = map[string]string{
	"asking":         "waiting for your answer",
	"answered":       "answered; nothing was changed",
	"hand_back":      "too large for a Spike; recommended to hand back",
	"blocked":        "blocked; the blocker and the decision needed are recorded",
	"fix_proposed":   "one small change is proposed; nothing was changed",
	"fixed":          "one small change was made and the repository checks passed",
	"fix_unverified": "one small change was made but the checks did not pass; the change is not ready",
	"fix_too_big":    "the change turned out too big; it is not ready and was handed back",
}

// renderSpikeComment lays out a Spike report from the candidate and the status
// the factory recorded for it. It never chooses what happens next.
func renderSpikeComment(candidate agentresult.Result, next string, _ naming.Ref, _ string) string {
	var head strings.Builder
	spike := candidate.Payload.Spike
	switch {
	case spike != nil && spike.Status != "":
		fmt.Fprintf(&head, "**Spike status:** `%s`, %s\n\n", spike.Status, spikeStatusLine[spike.Status])
	case candidate.Outcome != "completed":
		head.WriteString("**Spike did not finish.**\n\n")
	}
	body := renderResearchComment(candidate, "", nil)
	var detail strings.Builder
	if spike != nil && len(candidate.Questions) == 0 {
		for _, group := range []struct {
			title string
			items []string
		}{{"What was done", spike.WorkDone}, {"What was found", spike.Findings}, {"Decisions worth remembering", spike.Decisions}, {"Not done", spike.NotDone}} {
			if len(group.items) == 0 {
				continue
			}
			fmt.Fprintf(&detail, "\n\n## %s\n", group.title)
			for _, item := range group.items {
				fmt.Fprintf(&detail, "\n- %s", strings.TrimSpace(item))
			}
		}
		if fix := spike.ProposedFix; fix != nil {
			fmt.Fprintf(&detail, "\n\n## Proposed change\n\n- Defect: %s\n- Files: %s\n- Kinds of change: %s\n- Regression test: %s", strings.Join(fix.Defects, "; "), strings.Join(fix.Files, ", "), strings.Join(fix.ChangeKinds, ", "), fix.RegressionTest)
		}
		if fix := spike.FixReport; fix != nil {
			fmt.Fprintf(&detail, "\n\n## Change made\n\n- Defect: %s\n- Files claimed: %s\n- Regression test: %s\n- Summary: %s", strings.TrimSpace(fix.Defect), strings.Join(fix.Files, ", "), fix.RegressionTest, strings.TrimSpace(fix.Summary))
		}
		if check := spike.Check; check != nil {
			detail.WriteString("\n\n## Measured change and checks\n\n" + renderSpikeCheck(check))
		}
		if issue := spike.Issue; issue != nil {
			detail.WriteString("\n\n## Follow-up issue\n\n" + renderSpikeIssue(issue))
		}
		if candidate.Recommendation != nil && strings.TrimSpace(candidate.Recommendation.Text) != "" {
			fmt.Fprintf(&detail, "\n\n## Recommendation\n\n%s", strings.TrimSpace(candidate.Recommendation.Text))
		}
	}
	out := head.String() + body + detail.String()
	if next != "" {
		out += "\n\n---\n\n**Next:** " + next
	}
	return out
}

// renderSpikeCheck states what the factory measured, from the retained check
// record and never from the fixer's claim.
func renderSpikeCheck(c *agentresult.SpikeCheckView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Result: `%s`", c.Outcome)
	if c.Reason != "" {
		fmt.Fprintf(&b, " (%s)", c.Reason)
	}
	fmt.Fprintf(&b, "\n- Files changed (%d): %s", len(c.Files), blankFallback(strings.Join(c.Files, ", "), "none"))
	if len(c.Forbidden) > 0 {
		fmt.Fprintf(&b, "\n- Kinds of change that are not allowed: %s", strings.Join(c.Forbidden, ", "))
	}
	if len(c.Phases) > 0 {
		fmt.Fprintf(&b, "\n- Check phases run: %s", strings.Join(c.Phases, ", "))
	} else {
		b.WriteString("\n- No repository checks were run.")
	}
	if c.Outcome != "passed" {
		b.WriteString("\n- The change is in the working branch and is **not ready**; nothing was discarded.")
	}
	return b.String()
}

// renderSpikeIssue states the filing outcome from the retained issue record.
func renderSpikeIssue(i *agentresult.SpikeIssueView) string {
	if i.State == "filed" && i.URL != "" {
		return fmt.Sprintf("Filed as %s (#%d).", i.URL, i.Number)
	}
	return "Filing a follow-up issue could not be confirmed. Nothing was filed that this report can point to; check the repository before filing it again by hand."
}

func spikeNextStep(profile *graphexec.Profile, node, kind string) string {
	if kind == "final" {
		return "this Spike is finished and its note is saved on the branch. A hand-back is advice only: comment `/research <what to look into>` or `/plan <what to plan>` if you want it taken further."
	}
	return researchNextStep(profile, node, kind)
}
