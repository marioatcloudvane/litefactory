package report

import (
	"errors"
	"fmt"
	"strings"
)

// BugDeliveryReview contains already-serialized evidence from the factory's
// immutable domain records. Rendering performs no remote read or authorization.
type BugDeliveryReview struct {
	Task, Digest, Head, Target, Recipe string
	PR                                 int
	Analysis, Repair                   string
	Phases                             [3]string
	ApprovalCommand                    string
	Clarifications                     []string
}

// RenderBugDeliveryReview requires all three phases and the exact owner control.
func RenderBugDeliveryReview(r BugDeliveryReview) (string, error) {
	if r.PR <= 0 || len(r.Digest) != 64 || len(r.Head) != 40 || len(r.Target) != 40 || len(r.Recipe) != 64 {
		return "", errors.New("invalid Bug delivery review identities")
	}
	for _, s := range []string{r.Digest, r.Head, r.Target, r.Recipe} {
		if strings.Trim(s, "0123456789abcdef") != "" {
			return "", errors.New("invalid Bug delivery review digest")
		}
	}
	if strings.TrimSpace(r.Task) == "" || strings.TrimSpace(r.Analysis) == "" || strings.TrimSpace(r.Repair) == "" {
		return "", errors.New("incomplete Bug delivery review evidence")
	}
	command := fmt.Sprintf("/approve-bug delivery %s %d %s main %s", r.Digest, r.PR, r.Head, r.Target)
	if r.ApprovalCommand != command {
		return "", errors.New("bug delivery approval command mismatch")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Bug delivery review for %s\n\nPR #%d; tested head `%s`; main at `%s`.\nRecipe `%s`; review `%s`.\n\nApproved analysis and evidence:\n%s\n\nRepair and criterion coverage:\n%s\n", r.Task, r.PR, r.Head, r.Target, r.Recipe, r.Digest, r.Analysis, r.Repair)
	for n, phase := range []string{"regression", "focused", "full"} {
		if strings.TrimSpace(r.Phases[n]) == "" {
			return "", errors.New("missing Bug check phase")
		}
		fmt.Fprintf(&b, "\n%s checks:\n%s\n", phase, r.Phases[n])
	}
	for _, c := range r.Clarifications {
		if strings.TrimSpace(c) == "" {
			return "", errors.New("blank Bug clarification")
		}
		fmt.Fprintf(&b, "\nClarification from retained evidence:\n%s\n", c)
	}
	fmt.Fprintf(&b, "\nTo approve this exact delivery, reply:\n`%s`\n\nFor feedback, reply:\n`/bug-review %s clarify|repair|reshape <text>`\nClarification uses retained evidence; unanswered questions remain pending. Repair remains within the approved remedy; material changes require renewed analysis approval.\n", command, r.Digest)
	if b.Len() > 60000 {
		return "", errors.New("complete Bug delivery review exceeds publication bound")
	}
	return b.String(), nil
}
