package report

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func planNoticeValue(value string) string {
	text, _ := truncateEnd(oneLine(value), "…", 1024)
	return text
}

// PlanStoryNoticeText renders only retained material facts. Dynamic fences keep
// external error strings literal, while the delivery renderer applies redaction.
func PlanStoryNoticeText(n planbrief.PlanNoticeState) string {
	var data strings.Builder
	fmt.Fprintf(&data, "Story: %s\n", planNoticeValue(n.Key))
	switch {
	case n.State == "closed":
		reason := n.Reason
		if reason == "" {
			reason = "unspecified"
		}
		fmt.Fprintf(&data, "GitHub closure adopted. Actual reason: %s. This story is outside active shaping.\n", planNoticeValue(reason))
	case n.Reopened:
		data.WriteString("GitHub reopening adopted. Current content, dependencies and approvals require reevaluation. Frozen managed scope remains frozen.\n")
	default:
		data.WriteString("The retained lifecycle and recovery state changed.\n")
	}
	if n.Retired {
		data.WriteString("Owner-confirmed retirement is recorded.\n")
	}
	for _, cause := range []struct{ name, value string }{{"Lifecycle hold", n.LifecycleHold}, {"Request hold", n.EffectHold}, {"Plan hold", n.Hold}} {
		if cause.value != "" {
			fmt.Fprintf(&data, "%s: %s\n", cause.name, planNoticeValue(cause.value))
		}
	}
	keys := slices.Sorted(maps.Keys(n.RequestHolds))
	for _, key := range keys[:min(8, len(keys))] {
		if key == n.Request {
			continue
		}
		fmt.Fprintf(&data, "Retained request %s: %s\n", planNoticeValue(key), planNoticeValue(n.RequestHolds[key]))
	}
	if len(keys) > 8 {
		fmt.Fprintf(&data, "%d additional original request causes remain retained.\n", len(keys)-8)
	}
	if n.Effect != "" {
		fmt.Fprintf(&data, "Unresolved request: %s (%s).\n", planNoticeValue(n.Request), planNoticeValue(n.Effect))
	}
	action := "Review the remaining active Plan and its dependencies. Closure supplies no approval, engineering evidence or acceptance."
	if len(n.RequestHolds) > 0 || n.Hold != "" || n.LifecycleHold != "" || n.Effect != "" || n.EffectHold != "" {
		action = "Resolve the listed binding, dependency or original-request cause before this story can advance. Independent eligible stories remain available."
	}
	text := strings.TrimSpace(data.String())
	fence := fenceFor(text)
	return "Plan story update\n\n" + fence + "text\n" + text + "\n" + fence + "\n\n" + action
}

// PlanReplyExplanationText renders a bounded, plain-language explanation for
// one reply on a story sub-issue that could not act. It is posted on that
// sub-issue; the reason carries the command that works now when there is one.
func PlanReplyExplanationText(key string, child int, _ int64, reason string, reference ...string) string {
	details := planNoticeValue(reason)
	fence := fenceFor(details)
	label := fmt.Sprintf("#%d", child)
	if len(reference) > 0 {
		label = reference[0]
	}
	return fmt.Sprintf("Your reply on %s did not change story %s, and nothing was approved.\n\n", label, planNoticeValue(key)) + fence + "text\n" + details + "\n" + fence + "\n\nReply on this issue again when you are ready."
}
