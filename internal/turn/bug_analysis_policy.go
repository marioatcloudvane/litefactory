package turn

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"strings"
)

// BugWorkflowPolicy registers Bug's strict shared-result contract. The complete
// parent package supplies execution bindings separately through BugAnalysisPolicy;
// this descriptor does not admit an otherwise unavailable parent workflow.
func BugWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{
		Workflow:                  agent.WorkflowBug,
		AcceptAfterProcessFailure: true,
		Respond: func(r agentresult.Result) (string, error) {
			if r.Payload.Bug == nil || r.Payload.Bug.Response == "" {
				return "", errors.New("bug result has no validated response")
			}
			return r.Payload.Bug.Response, nil
		},
		Control:  graphexec.ControlPolicy{Workflow: agent.WorkflowBug, Responses: []string{"ask", "done", "approved", "blocked"}, Defaults: []string{"ask"}, Ask: "ask", Approved: "approved"},
		Decode:   agentresult.DecodeBugNode,
		Retain:   RetainBugNode,
		Guidance: "Bug analyst results use the version-1 shared result helper and service: preflight, correct diagnostics, submit and read the matching receipt. Lost replies retain request identity and identical bytes; corrected content needs a new request. Terminal text never determines application response; end with a short plain-language note. Only factory-authenticated exact owner approval authorizes repair.",
	}
}

// renderBugAnalysis lays one recorded analysis revision out for the owner:
// what goes wrong, why, the proposed fix and its limits, and the measurable
// criteria the repair will be held to. Every sentence is the analysis's own.
func renderBugAnalysis(revision int64, digest string, a buganalysis.BugAnalysis) string {
	var b strings.Builder
	short := digest
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Fprintf(&b, "## Bug analysis, revision %d\n\n", revision)
	fmt.Fprintf(&b, "**Expected:** %s\n\n**Actual:** %s\n\n", oneLine(a.Expected), oneLine(a.Actual))
	observation := map[string]string{"observed": "reproduced", "owner_reported": "reported by the owner, not reproduced", "not_observed": "not observed"}[a.Reproduction.Observation]
	if observation == "" {
		observation = a.Reproduction.Observation
	}
	fmt.Fprintf(&b, "### Reproduction (%s)\n\n", observation)
	for i, step := range a.Reproduction.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, oneLine(step))
	}
	if env := strings.TrimSpace(a.Reproduction.Environment); env != "" {
		fmt.Fprintf(&b, "\n**Environment:** %s\n", oneLine(env))
	}
	fmt.Fprintf(&b, "\n### Cause\n\n%s\n", strings.TrimSpace(a.Cause))
	bullets(&b, "Evidence", a.Evidence)
	fmt.Fprintf(&b, "\n### Proposed fix\n\n%s\n", strings.TrimSpace(a.Remedy))
	if a.Boundaries != nil {
		codes := func(paths []string) []string {
			out := make([]string, len(paths))
			for i, p := range paths {
				out[i] = "`" + p + "`"
			}
			return out
		}
		bullets(&b, "Files the fix is expected to change", codes(a.Boundaries.AllowedPaths))
		bullets(&b, "Files the fix should leave alone", codes(a.Boundaries.ExcludedPaths))
	}
	bullets(&b, "Not included", a.Exclusions)
	if len(a.Criteria) > 0 {
		b.WriteString("\n### Success criteria\n\n| ID | Criterion | How it is measured |\n|---|---|---|\n")
		for _, c := range a.Criteria {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", c.ID, bugCell(c.Requirement), bugCell(c.Measurement))
		}
	}
	bullets(&b, "Decisions", a.Decisions)
	bullets(&b, "Assumptions", a.Assumptions)
	if len(a.Questions) > 0 {
		b.WriteString("\n### Open questions\n\n")
		for _, q := range a.Questions {
			blocking := ""
			if q.Material {
				blocking = " (needs an answer before the fix)"
			}
			fmt.Fprintf(&b, "- `%s`%s: %s\n", q.ID, blocking, oneLine(q.Text))
		}
	}
	fmt.Fprintf(&b, "\n<sub>Recorded analysis `%s`.</sub>\n", short)
	return b.String()
}

// bugAnalysisLead is what the owner scans first: whether the bug was
// reproduced, what goes wrong and when, why, the proposed fix, and what they
// need to do next. It only condenses the analysis's own wording.
func bugAnalysisLead(kind string, revision int64, a buganalysis.BugAnalysis) string {
	var b strings.Builder
	switch a.Reproduction.Observation {
	case "observed":
		b.WriteString("**I traced the bug and reproduced it.**\n\n")
	case "owner_reported":
		b.WriteString("**I traced the bug from your report; I did not reproduce it myself.**\n\n")
	default:
		b.WriteString("**I traced the bug but could not observe it happening.**\n\n")
	}
	fmt.Fprintf(&b, "- **What goes wrong:** %s\n", firstSentence(a.Actual))
	if len(a.Reproduction.Steps) > 0 {
		steps := make([]string, len(a.Reproduction.Steps))
		for i, step := range a.Reproduction.Steps {
			steps[i] = strings.TrimSuffix(oneLine(step), ".")
		}
		fmt.Fprintf(&b, "- **When:** %s\n", strings.Join(steps, " → "))
	}
	fmt.Fprintf(&b, "- **Why:** %s\n", firstSentence(a.Cause))
	fmt.Fprintf(&b, "- **Proposed fix:** %s\n", firstSentence(a.Remedy))
	open := 0
	for _, q := range a.Questions {
		if q.Material {
			open++
		}
	}
	switch {
	case kind == "review":
		fmt.Fprintf(&b, "\n**Next:** if this is right, reply `/approve-bug analysis %d` and the fix starts; otherwise reply with what should change.\n", revision)
	case open == 1:
		b.WriteString("\n**Next:** one question below needs your answer before the fix can start.\n")
	case open > 1:
		fmt.Fprintf(&b, "\n**Next:** %d questions below need your answers before the fix can start.\n", open)
	}
	return b.String()
}

// firstSentence keeps the lead short; the full text follows in the details.
func firstSentence(text string) string {
	line := oneLine(text)
	for i := 0; i+1 < len(line); i++ {
		if (line[i] == '.' || line[i] == '!' || line[i] == '?') && line[i+1] == ' ' && i >= 20 {
			return line[:i+1]
		}
	}
	return line
}

func bullets(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n### %s\n\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", oneLine(item))
	}
}

// oneLine keeps an agent sentence on one Markdown line.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// bugCell keeps agent text inside one Markdown table cell.
func bugCell(text string) string { return strings.ReplaceAll(oneLine(text), "|", `\|`) }

type bugRetainedNode struct {
	ResultID       int64                    `json:"result_id"`
	ResultRevision int64                    `json:"result_revision"`
	Node           agentresult.ResearchNode `json:"node"`
}

// RetainBugNode links only validated accepted shared submissions to graph output.
func RetainBugNode(binding store.ResultBinding, revision int64, node agentresult.ResearchNode) ([]byte, error) {
	if binding.Workflow != "bug" || binding.Mode != "" || binding.ResultID <= 0 || revision <= 0 || node.Result.Payload.Bug == nil {
		return nil, errors.New("bug retained output requires matching shared result")
	}
	return json.Marshal(bugRetainedNode{binding.ResultID, revision, node})
}
