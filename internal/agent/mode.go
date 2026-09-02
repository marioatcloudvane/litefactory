package agent

import (
	"context"
	"strings"
)

// Mode is a workflow phase selected by litefactory before a work run starts.
type Mode string

const (
	// ModeResearch investigates a requested feature or problem.
	ModeResearch Mode = "research"
	// ModePlan turns approved research into an implementation plan.
	ModePlan Mode = "plan"
	// ModeImplement executes an approved plan.
	ModeImplement Mode = "implement"
	// ModeSpike is a small, time-boxed experiment or ad-hoc change.
	ModeSpike Mode = "spike"
)

// Valid reports whether m belongs to litefactory's closed mode vocabulary.
func (m Mode) Valid() bool {
	switch m {
	case ModeResearch, ModePlan, ModeImplement, ModeSpike:
		return true
	default:
		return false
	}
}

// Label returns the canonical GitHub label for m.
func (m Mode) Label() string {
	if !m.Valid() {
		return ""
	}
	s := string(m)
	return "Mode: " + strings.ToUpper(s[:1]) + s[1:]
}

// ParseMode accepts a mode's canonical lower-case value.
func ParseMode(value string) (Mode, bool) {
	m := Mode(strings.TrimSpace(value))
	return m, m.Valid()
}

// ParseModeLabel accepts an exact canonical GitHub label.
func ParseModeLabel(label string) (Mode, bool) {
	for _, m := range []Mode{ModeResearch, ModePlan, ModeImplement, ModeSpike} {
		if label == m.Label() {
			return m, true
		}
	}
	return "", false
}

// Classification is the deliberately small input to a tool-free routing run.
type Classification struct {
	WorkingDirectory string
	Prompt           string
}

// Classifier resolves one instruction to a concrete mode.
type Classifier interface {
	Classify(ctx context.Context, req Classification) (Mode, error)
}
