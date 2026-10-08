package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// RenderConversation retains full findings and immutable proposal review details.
// The caller redacts before retaining publication intent or writing output.
func RenderConversation(c Conversation) string {
	var body strings.Builder
	body.WriteString("Doctor diagnosis\n\n")
	if c.Diagnosis == nil {
		body.WriteString("No accepted diagnosis is available.\n")
	} else {
		for _, finding := range c.Diagnosis.Findings {
			fmt.Fprintf(&body, "%s\nEvidence: %s\nUncertainty: %s\n\n", finding.Explanation, strings.Join(finding.Evidence, ", "), finding.Uncertainty)
		}
		for _, gap := range c.Diagnosis.CoverageGaps {
			fmt.Fprintf(&body, "Coverage gap: %s\n", gap)
		}
		for _, question := range c.Diagnosis.Questions {
			fmt.Fprintf(&body, "Question: %s\n", question)
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
			outcome = "worked"
		}
		fmt.Fprintf(&body, "Overall: %s\n", outcome)
		if c.Repair.Failure != "" {
			fmt.Fprintf(&body, "Unresolved: %s\n", c.Repair.Failure)
		}
		for _, step := range c.RepairSteps {
			recheck := "not completed"
			if step.State == "verified" {
				recheck = "passed: " + step.ObservedAfter
			}
			fmt.Fprintf(&body, "Step %d: %s; backup: %s; recheck: %s\n", step.Number, step.State, step.BackupReference, recheck)
		}
		if c.Restart != nil {
			fmt.Fprintf(&body, "Restart: %s\n%s\n", c.Restart.State, c.Restart.Report)
		}
	}

	terminalTypedRepair := c.Repair != nil && (c.Repair.State == "completed" || c.Repair.State == "failed")
	if !c.ApplyAvailable && !terminalTypedRepair {
		body.WriteString("\nApply is unavailable. No repair or restart was performed by diagnosis.\n")
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
