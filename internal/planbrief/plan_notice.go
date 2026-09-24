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
		return "This story is no longer in the current Plan. Review the current active scope."
	}
	if !item.Active() {
		return "This child is closed or retired. Its reply cannot approve an inactive candidate. Review the remaining Plan; reopening requires a fresh review."
	}
	if item.Blocked() {
		return "This story has unresolved lifecycle or request debt. Resolve the reported holds before approving its current candidate."
	}
	if _, err = s.ReviewRevision(key, item.Stage); err != nil {
		return "A dependency is inactive or unresolved. Resolve its reported state before approving this candidate."
	}
	return ""
}

// PlanReplyReason checks exact approval commands against the current display.
// Ordinary discussion and mixed prose retain their normal feedback semantics.
func (s *PlanState) PlanReplyReason(key, body string) string {
	if reason := s.PlanReplyHold(key); reason != "" {
		return reason
	}
	item, err := s.story(key)
	if err != nil {
		return "The current story could not be verified."
	}
	w := item.Wait
	if w != nil {
		revision, err := s.ReviewRevision(key, item.Stage)
		if err != nil || revision != w.Revision {
			return "This review refers to changed content, dependencies or shared decisions. Use a fresh review of the current candidate."
		}
	}
	parts := strings.Fields(body)
	if len(parts) == 4 && parts[0] == "/approve-plan" {
		if w == nil || w.Kind != "review" || parts[1] != key || parts[2] != item.Stage || parts[3] != item.Candidate || w.Candidate != item.Candidate {
			return "This approval names a stale candidate or review. Use the current story review prompt."
		}
		if len(item.Draft.Questions) > 0 {
			return "Open questions still require an owner response before this candidate can be approved."
		}
		for _, stage := range planStages[:max(0, slices.Index(planStages, item.Stage))] {
			ref, err := s.ReviewRevision(key, stage)
			if err != nil || item.Approvals[stage] != ref {
				return "An earlier stage needs current approval before this candidate can advance."
			}
		}
	}
	if len(parts) > 0 && parts[0] == "/retire-plan" {
		if len(parts) != 3 || w == nil || w.Kind != "retire" || parts[1] != key || parts[2] != item.Candidate || w.Candidate != item.Candidate {
			return "This retirement reply names a stale candidate or proposal. Review the current story before confirming retirement."
		}
	}
	return ""
}
