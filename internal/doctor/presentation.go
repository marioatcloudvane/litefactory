package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

// RenderConversation retains full findings and immutable proposal review details.
// The caller redacts before retaining publication intent or writing output.
func RenderConversation(c Conversation) string {
	var body strings.Builder
	if c.Repair == nil {
		body.WriteString("Doctor diagnosis\n\n")
	}
	if c.Diagnosis == nil && c.Repair == nil {
		body.WriteString("No accepted diagnosis is available.\n")
	} else if c.Diagnosis != nil {
		for _, finding := range c.Diagnosis.Findings {
			fmt.Fprintf(&body, "%s\n\n", finding.Explanation)
			if finding.Uncertainty != "" {
				fmt.Fprintf(&body, "%s\n\n", finding.Uncertainty)
			}
			if len(finding.Evidence) > 0 {
				fmt.Fprintf(&body, "<details>\n<summary>Evidence</summary>\n\n- %s\n\n</details>\n\n", strings.Join(finding.Evidence, "\n- "))
			}
		}
		if len(c.Diagnosis.CoverageGaps) > 0 {
			fmt.Fprintf(&body, "<details>\n<summary>Coverage gaps</summary>\n\n- %s\n\n</details>\n\n", strings.Join(c.Diagnosis.CoverageGaps, "\n- "))
		}
		for _, question := range c.Diagnosis.Questions {
			fmt.Fprintf(&body, "%s\n\n", question)
		}
	}
	if len(c.CurrentProposal) > 0 {
		body.WriteString("\nCurrent repair proposal:\n")
		for _, step := range c.CurrentProposal {
			fmt.Fprintf(&body, "\n%d. Target: %s\nPrecondition: %s\nChange: %s\nReason: %s\nReversibility: %s\nRollback: %s\n", step.Number, step.Target, step.Precondition, step.Change, step.Reason, step.Reversibility, step.Rollback)
			if step.Action != nil {
				data, _ := json.Marshal(step.Action)
				fmt.Fprintf(&body, "Exact proposed action: %s\n", data)
			}
		}
	}
	if c.Repair != nil {
		fmt.Fprintf(&body, "\nRepair %s: %s\n", c.Repair.ID, c.Repair.State)
		outcome := "rechecked"
		if c.Repair.Failure != "" || c.Repair.State == "stopped" || c.Repair.State == "needs_review" || c.Repair.State == "failed" {
			outcome = "failed"
		} else if c.Repair.State == "recovered" || c.Repair.State == "completed" {
			outcome = "repaired"
		}
		graphRepair := len(c.RepairSteps) > 0
		for _, step := range c.RepairSteps {
			graphRepair = graphRepair && store.DoctorGraphRepairKind(step.Kind)
		}
		if graphRepair && c.Repair.State == "completed" {
			outcome = "stale blocker removed; approved graph repair verified"
		}
		fmt.Fprintf(&body, "Overall: %s\n", outcome)
		if graphRepair {
			body.WriteString("Graph repair and any separately approved inverse commit all dependent rows, guidance handoff, verification and numbered journal entries together, or no repair changes. Repair alone does not establish continuation admitted or work completed.\n")
		}
		if c.Repair.Failure != "" {
			fmt.Fprintf(&body, "Unresolved: %s\n", c.Repair.Failure)
		}
		if c.Diagnosis != nil && c.Diagnosis.Repair != nil {
			for _, note := range c.Diagnosis.Repair.Unresolved {
				fmt.Fprintf(&body, "Note: %s\n", note)
			}
		}
		for _, step := range c.RepairSteps {
			recheck := "not completed"
			if step.State == "verified" {
				recheck = "passed"
			}
			after := step.ObservedAfter
			if after == "" {
				after = step.After
			}
			backup := step.BackupReference
			if backup == "" {
				backup = "none"
			}
			fmt.Fprintf(&body, "Step %d %s %s: %s (%s → %s), recheck %s, backup %s\n", step.Number, step.Kind, step.Target, step.State, step.Before, after, recheck, backup)
		}
		if c.Restart != nil {
			fmt.Fprintf(&body, "Restart: %s\n%s\n", c.Restart.State, c.Restart.Report)
		}
	}

	if !c.ApplyAvailable && c.Repair == nil {
		body.WriteString("\nApply is unavailable. No repair or restart was performed by diagnosis.\n")
	}
	if c.Repair != nil {
		body.WriteString("\nNext: `/doctor Recheck the repair outcome and recommend the next action` starts a fresh diagnosis from the retained journal.\n")
	}
	body.WriteString("\nContinue with /doctor <instructions>; end the conversation with /doctor stop.\n")
	return body.String()
}

// PresentationParts splits without losing any byte or breaking a UTF-8 rune.
// Numbering and stable markers are added by the retained delivery adapter.
func PresentationParts(body string) ([]string, error) {
	if body == "" || len(body) > 262144 || !utf8.ValidString(body) {
		return nil, errors.New("doctor presentation is empty, invalid UTF-8 or exceeds total byte budget")
	}
	var parts []string
	for len(body) > 0 {
		size := min(len(body), 48000)
		for size < len(body) && !utf8.RuneStart(body[size]) {
			size--
		}
		parts = append(parts, body[:size])
		body = body[size:]
	}
	return parts, nil
}
