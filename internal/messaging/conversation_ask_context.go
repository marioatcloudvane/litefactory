package messaging

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
)

// conversationAskContext preserves the prepared history alongside current facts.
// History is explanatory captured text, never a fact catalog or owner authority.
// The encoded envelope, including gap metadata, fits the existing lookup limit.
func conversationAskContext(prepared ask.Prepared) (string, []string, error) {
	type historyView struct {
		Turns      []ask.Turn `json:"turns"`
		Coverage   string     `json:"coverage"`
		Limitation string     `json:"limitation"`
	}
	view := struct {
		Bundle  ask.Bundle  `json:"bundle"`
		History historyView `json:"history"`
		Gaps    []string    `json:"gaps"`
	}{Bundle: prepared.Bundle, Gaps: append([]string(nil), prepared.Bundle.Gaps...)}
	view.History.Coverage = "partial"
	view.History.Limitation = "Delivered local Ask replies only; question text is the stored excerpt. Older or uncaptured history is unknown. Historical replies do not establish current facts or owner approval."
	switch {
	case prepared.Bundle.TargetIssue > 0:
		view.History.Coverage = "policy_omission"
		view.Gaps = append(view.Gaps, "Prior Ask history is excluded for an explicit issue request.")
	case prepared.History.Unavailable:
		view.History.Coverage = "unavailable"
		view.Gaps = append(view.Gaps, "Prior Ask history is unavailable.")
	default:
		for _, turn := range prepared.History.Turns {
			if strings.EqualFold(turn.Project, prepared.Repository.Slug()) {
				view.History.Turns = append(view.History.Turns, turn)
			} else {
				view.Gaps = appendUniqueGap(view.Gaps, "Ask replies for other projects were excluded.")
			}
		}
		if prepared.History.OlderLeftOut {
			view.Gaps = append(view.Gaps, "Older Ask history was left out by the existing history limits.")
		}
	}
	// Reserve gap metadata before dropping content; never slice encoded JSON.
	const limit = 16384
	for {
		raw, err := json.Marshal(view)
		if err != nil {
			return "", nil, err
		}
		if len(raw) <= limit {
			return string(raw), view.Gaps, nil
		}
		view.Gaps = appendUniqueGap(view.Gaps, "Some captured context was omitted from this lookup (16 KiB result limit); this is a partial view.")
		if len(view.History.Turns) > 0 {
			// Keep newest eligible turns, which are already newest first.
			view.History.Turns = view.History.Turns[:len(view.History.Turns)-1]
			continue
		}
		if len(view.Bundle.Facts) > 0 {
			view.Bundle.Facts = view.Bundle.Facts[:len(view.Bundle.Facts)-1]
			continue
		}
		// Bundle gaps are collector-owned bounded text. This branch only protects
		// against a malformed custom adapter while keeping an explicit terminal gap.
		return "", nil, errors.New("conversation: Ask context metadata exceeds lookup limit")
	}
}
