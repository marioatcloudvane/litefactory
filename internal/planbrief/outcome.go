package planbrief

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// OutcomeFileName is reserved transient transport alongside proposal v1.
	OutcomeFileName = ".litefactory-plan-outcome.json"
	// ContextFileName is committed working context inside the task's session.
	ContextFileName = "PLAN_CONTEXT.json"
	// MaxOutcomeBytes bounds the per-attempt outcome envelope.
	MaxOutcomeBytes = 4096
	// MaxContextBytes bounds saved working context.
	MaxContextBytes = 256 * 1024
)

// PlanOutcome explicitly distinguishes a discussion from a publishable plan.
// Attempt is a factory-generated nonce, renewed for every subprocess launch.
type PlanOutcome struct {
	Version    int    `json:"version"`
	Parent     string `json:"parent"`
	Generation int    `json:"generation"`
	RunID      int64  `json:"run_id"`
	Attempt    string `json:"attempt"`
	Outcome    string `json:"outcome"`
}

// WorkingContext preserves unfinished decisions without claiming plan readiness.
// Its identity is compared with admitted task, branch and feature by the factory.
type WorkingContext struct {
	Version     int    `json:"version"`
	Parent      string `json:"parent"`
	Branch      string `json:"branch"`
	Feature     string `json:"feature"`
	Scope       string `json:"scope"`
	Decisions   string `json:"decisions"`
	Assumptions string `json:"assumptions"`
	Questions   string `json:"questions"`
}

// ParseOutcome rejects missing, duplicate, null, unknown and trailing fields.
func ParseOutcome(data []byte) (PlanOutcome, error) {
	var outcome PlanOutcome
	if err := strictPlanObject(data, &outcome, MaxOutcomeBytes, "version", "parent", "generation", "run_id", "attempt", "outcome"); err != nil {
		return outcome, fmt.Errorf("parse plan outcome: %w", err)
	}
	if outcome.Version != 1 || strings.TrimSpace(outcome.Parent) == "" || outcome.Generation < 1 || outcome.RunID < 1 || strings.TrimSpace(outcome.Attempt) == "" || (outcome.Outcome != "discussion" && outcome.Outcome != "completed") {
		return PlanOutcome{}, fmt.Errorf("invalid plan outcome envelope")
	}
	return outcome, nil
}

// ParseWorkingContext validates the bounded, identity-bearing working artifact.
func ParseWorkingContext(data []byte) (WorkingContext, error) {
	var working WorkingContext
	if err := strictPlanObject(data, &working, MaxContextBytes, "version", "parent", "branch", "feature", "scope", "decisions", "assumptions", "questions"); err != nil {
		return working, fmt.Errorf("parse planning context: %w", err)
	}
	if working.Version != 1 {
		return WorkingContext{}, fmt.Errorf("unsupported planning context version")
	}
	for _, value := range []string{working.Parent, working.Branch, working.Feature, working.Scope, working.Decisions, working.Assumptions, working.Questions} {
		if strings.TrimSpace(value) == "" {
			return WorkingContext{}, fmt.Errorf("planning context requires nonempty identity, scope, decisions, assumptions and questions")
		}
	}
	return working, nil
}

// Both formats are flat objects. Checking their exact field set before typed
// decoding avoids encoding/json's duplicate and case-insensitive key aliases.
func strictPlanObject[T any](data []byte, target *T, limit int, fields ...string) error {
	if len(data) > limit {
		return fmt.Errorf("size limit exceeded")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	missing := make(map[string]bool, len(fields))
	for _, field := range fields {
		missing[field] = true
	}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || !missing[key] {
			return fmt.Errorf("unknown or duplicate field %v", token)
		}
		delete(missing, key)
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("null field %s", key)
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if len(missing) != 0 {
		return fmt.Errorf("required fields missing")
	}
	if err := d.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing data")
	}
	return json.Unmarshal(data, target)
}
