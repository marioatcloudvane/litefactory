package report

import (
	"errors"
	"fmt"
	"strings"
)

// BugDeliveryReview contains already-rendered Markdown from the factory's
// immutable domain records. Rendering performs no remote read or authorization.
// Lead is what the owner scans first; Phases are one check-table row each.
// Facts is every recorded fact already laid out one per line, in stage order
// ("[stage] text"); nil or empty renders as "None". SelfCheck is the one-line
// fixer self-check evidence for the repair attempt behind this delivery
// (AC-009), computed from factory-recorded source=fixer bug_checks rows.
type BugDeliveryReview struct {
	Task, Digest, Head, Target, Recipe string
	PR                                 int
	Lead, Analysis, Repair             string
	Phases                             [3]string
	Facts                              []string
	SelfCheck                          string
	ApprovalCommand                    string
	Clarifications                     []string
}

// RenderBugFactsSection lays out the owner-facing "What the factory noticed"
// block shared by the analysis and delivery reviews: every recorded fact
// (already formatted one per line, in stage order), or "None" when there are
// none (AC-006).
func RenderBugFactsSection(lines []string) string {
	var b strings.Builder
	b.WriteString("## What the factory noticed\n\n")
	if len(lines) == 0 {
		b.WriteString("None\n")
		return b.String()
	}
	for _, l := range lines {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
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
	if strings.TrimSpace(r.Task) == "" || strings.TrimSpace(r.Lead) == "" || strings.TrimSpace(r.Analysis) == "" || strings.TrimSpace(r.Repair) == "" || strings.TrimSpace(r.SelfCheck) == "" {
		return "", errors.New("incomplete Bug delivery review evidence")
	}
	command := fmt.Sprintf("/approve-bug delivery %s %d %s main %s", r.Digest, r.PR, r.Head, r.Target)
	if r.ApprovalCommand != command {
		return "", errors.New("bug delivery approval command mismatch")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n---\n\n## Repair\n\n%s\n\n## Checks\n\n| Phase | Result | Commands | Time |\n|---|---|---|---|\n", strings.TrimSpace(r.Lead), strings.TrimSpace(r.Repair))
	for n := range r.Phases {
		if strings.TrimSpace(r.Phases[n]) == "" {
			return "", errors.New("missing Bug check phase")
		}
		fmt.Fprintf(&b, "%s\n", strings.TrimSpace(r.Phases[n]))
	}
	fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(r.SelfCheck))
	fmt.Fprintf(&b, "\n%s", RenderBugFactsSection(r.Facts))
	fmt.Fprintf(&b, "\n<details><summary>Approved analysis</summary>\n\n%s\n</details>\n", strings.TrimSpace(r.Analysis))
	for _, c := range r.Clarifications {
		if strings.TrimSpace(c) == "" {
			return "", errors.New("blank Bug clarification")
		}
		fmt.Fprintf(&b, "\n### Clarification from retained evidence\n\n%s\n", c)
	}
	fmt.Fprintf(&b, "\nTo approve this exact delivery and merge PR #%d, reply:\n`%s`\n\nFor feedback, reply:\n`/bug-review %s clarify|repair|reshape <text>`\nClarification uses retained evidence; unanswered questions remain pending. Repair remains within the approved remedy; material changes require renewed analysis approval.\n", r.PR, command, r.Digest)
	fmt.Fprintf(&b, "\n<sub>%s · PR #%d · tested head `%s` · main `%s` · recipe `%s` · review `%s`</sub>\n", r.Task, r.PR, r.Head, r.Target, r.Recipe, r.Digest)
	if b.Len() > 60000 {
		return "", errors.New("complete Bug delivery review exceeds publication bound")
	}
	return b.String(), nil
}
