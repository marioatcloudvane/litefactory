package planbrief

import (
	"maps"
	"slices"
	"strings"
)

// PlanNoticeState contains only material owner-facing lifecycle and debt facts.
// Candidate churn and polling revisions are intentionally absent.
type PlanNoticeState struct {
	RequestHolds  map[string]string `json:"request_holds,omitempty"`
	Key           string            `json:"key"`
	State         string            `json:"state,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Reopened      bool              `json:"reopened,omitempty"`
	Retired       bool              `json:"retired,omitempty"`
	Hold          string            `json:"hold,omitempty"`
	LifecycleHold string            `json:"lifecycle_hold,omitempty"`
	Effect        string            `json:"effect,omitempty"`
	Request       string            `json:"request,omitempty"`
	EffectHold    string            `json:"effect_hold,omitempty"`
}

// NoticeState returns an independent snapshot of material reporting facts.
func (s PlanStoryState) NoticeState() PlanNoticeState {
	n := PlanNoticeState{RequestHolds: maps.Clone(s.EffectHolds), Key: s.Key, State: s.Lifecycle.State, Reason: s.Lifecycle.Reason, Reopened: s.Lifecycle.Reopened, Retired: s.Retired, Hold: s.Hold, LifecycleHold: s.LifecycleHold, EffectHold: s.EffectHold}
	if s.Effect != "" && s.Effect != "applied" && s.EffectHold != "" {
		n.Effect = "unresolved"
		n.Request = s.EffectRequest
	}
	return n
}

// Significant excludes initial healthy observation but includes retained causes.
func (s PlanNoticeState) Significant() bool {
	return len(s.RequestHolds) > 0 || s.State == "closed" || s.Reopened || s.Retired || s.Hold != "" || s.LifecycleHold != "" || s.Effect != "" || s.EffectHold != ""
}

// PlanReplyHold explains why authenticated input cannot act on the current story.
func (s *PlanState) PlanReplyHold(key string) string {
	item, err := s.story(key)
	if err != nil {
		return "This story is no longer part of the plan. Review the remaining stories on the parent issue."
	}
	if !item.Active() {
		return "This story is closed or retired, so a reply here cannot approve it. Review the remaining stories; reopening it starts a fresh review."
	}
	if item.Blocked() {
		return "This story has an unresolved problem reported earlier. Resolve it before approving this story."
	}
	if _, err = s.ReviewRevision(key, item.Stage); err != nil {
		return "A story this one depends on is closed or has an unresolved problem. Resolve that story first, then approve this one."
	}
	return ""
}

// PlanCurrentCommand is the exact command that acts on the story's current
// owner wait, or empty when the story waits for no owner command. It reads only
// saved state, so the command it names is the one the latest review shows.
func (s *PlanState) PlanCurrentCommand(key string) string {
	item, err := s.story(key)
	if err != nil || item.Wait == nil || item.Candidate == "" || item.Wait.Candidate != item.Candidate {
		return ""
	}
	switch item.Wait.Kind {
	case "review":
		return "/approve-plan " + key + " " + item.Stage + " " + item.Candidate
	case "retire":
		return "/retire-plan " + key + " " + item.Candidate
	}
	return ""
}

// withCurrentCommand appends the command that works now, or says that nothing
// is waiting for one.
func (s *PlanState) withCurrentCommand(key, reason string) string {
	if command := s.PlanCurrentCommand(key); command != "" {
		return reason + " The command that works now is `" + command + "`."
	}
	return reason + " Nothing on this story is waiting for your approval right now; its next review will show the command to use."
}

// PlanReplyReason checks exact approval commands against the current display.
// Ordinary discussion and mixed prose retain their normal feedback semantics.
func (s *PlanState) PlanReplyReason(key, body string) string {
	if reason := s.PlanReplyHold(key); reason != "" {
		return reason
	}
	item, err := s.story(key)
	if err != nil {
		return "This story could not be found in the plan."
	}
	w := item.Wait
	if w != nil {
		revision, err := s.ReviewRevision(key, item.Stage)
		if err != nil || revision != w.Revision {
			return "This story, a story it depends on, or a shared decision changed after the latest review. Wait for the updated review and use the command it shows."
		}
	}
	parts := strings.Fields(body)
	if len(parts) == 4 && parts[0] == "/approve-plan" {
		if w == nil || w.Kind != "review" || parts[1] != key || parts[2] != item.Stage || parts[3] != item.Candidate || w.Candidate != item.Candidate {
			return s.withCurrentCommand(key, "This approval does not match the latest review of this story, so nothing was approved.")
		}
		if len(item.Draft.Questions) > 0 {
			return "This story still has open questions. Answer them on this issue before approving it."
		}
		for _, stage := range planStages[:max(0, slices.Index(planStages, item.Stage))] {
			ref, err := s.ReviewRevision(key, stage)
			if err != nil || item.Approvals[stage] != ref {
				return "An earlier stage of this story needs your approval again before this stage can be approved."
			}
		}
	}
	if len(parts) > 0 && parts[0] == "/retire-plan" {
		if len(parts) != 3 || w == nil || w.Kind != "retire" || parts[1] != key || parts[2] != item.Candidate || w.Candidate != item.Candidate {
			return s.withCurrentCommand(key, "This retirement reply does not match the latest proposal for this story, so nothing was retired.")
		}
	}
	return ""
}
