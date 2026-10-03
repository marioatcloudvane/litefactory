package bugrepair

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Validation binds one coherent final run to the factory's approved obligations.
// Actual command outcomes are agent-reported; independent source comparison
// checks freshness without executing the recipe a second time.
type Validation struct {
	Generation   string             `json:"generation"`
	SourceDigest string             `json:"source_digest"`
	TargetMain   string             `json:"target_main"`
	InputsDigest string             `json:"inputs_digest"`
	Commands     []CommandOutcome   `json:"commands"`
	Conditions   []ConditionOutcome `json:"conditions"`
}

// LogReference identifies contained stable evidence bytes by path and digest.
type LogReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// CommandOutcome records the observed execution of one required validation command.
type CommandOutcome struct {
	ID        string       `json:"id"`
	Command   string       `json:"command"`
	Directory string       `json:"directory"`
	ExitCode  *int         `json:"exit_code"`
	Outcome   string       `json:"outcome"`
	Log       LogReference `json:"log"`
}

// ConditionOutcome records evidence for one approved verification condition.
type ConditionOutcome struct {
	ID      string       `json:"id"`
	Outcome string       `json:"outcome"`
	Log     LogReference `json:"log"`
}

// ValidationRequirement is supplied by repository policy, never by the result.
type ValidationRequirement struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	Directory string `json:"directory"`
}

func digest(value string, size int) bool {
	decoded, e := hex.DecodeString(value)
	return e == nil && len(decoded) == size && strings.ToLower(value) == value
}

// Validate refuses escaping paths and malformed evidence digests.
func (l LogReference) Validate() error {
	if !fs.ValidPath(l.Path) || l.Path == "." || strings.ContainsAny(l.Path, "\\\x00\r\n") || !digest(l.SHA256, 32) {
		return errors.New("invalid stable validation log reference")
	}
	return nil
}

// Validate refuses unknown, duplicate, missing, skipped and failed obligations.
// All commands and conditions must belong to this generation's immutable inputs.
func (v Validation) Validate(commands []ValidationRequirement, conditions []string) error {
	if v.Generation == "" || len(v.Generation) > 256 || !digest(v.SourceDigest, 32) || !digest(v.InputsDigest, 32) || (!digest(v.TargetMain, 20) && !digest(v.TargetMain, 32)) {
		return errors.New("incomplete validation generation binding")
	}
	if len(commands) == 0 || len(commands) > 128 || len(conditions) == 0 || len(conditions) > 128 {
		return errors.New("invalid factory validation obligations")
	}
	expected := make(map[string]ValidationRequirement, len(commands))
	seen := make(map[string]bool)
	for _, c := range commands {
		if c.ID == "" || strings.TrimSpace(c.Command) == "" || (!fs.ValidPath(c.Directory) && c.Directory != ".") || expected[c.ID].ID != "" {
			return errors.New("invalid factory command set")
		}
		expected[c.ID] = c
	}
	for _, c := range v.Commands {
		r, ok := expected[c.ID]
		if !ok || seen[c.ID] {
			return fmt.Errorf("unknown or duplicate validation command %q", c.ID)
		}
		seen[c.ID] = true
		if c.Command != r.Command || c.Directory != r.Directory || c.ExitCode == nil || *c.ExitCode != 0 || c.Outcome != "passed" {
			return fmt.Errorf("validation command %s did not pass the required recipe", c.ID)
		}
		if e := c.Log.Validate(); e != nil {
			return e
		}
	}
	if len(seen) != len(expected) {
		return errors.New("missing required validation command")
	}
	conditionSet := make(map[string]bool, len(conditions))
	seen = make(map[string]bool)
	for _, id := range conditions {
		if id == "" || conditionSet[id] {
			return errors.New("invalid factory verification conditions")
		}
		conditionSet[id] = true
	}
	for _, c := range v.Conditions {
		if !conditionSet[c.ID] || seen[c.ID] {
			return fmt.Errorf("unknown or duplicate verification condition %q", c.ID)
		}
		seen[c.ID] = true
		if c.Outcome != "passed" {
			return errors.New("unmet approved verification condition")
		}
		if e := c.Log.Validate(); e != nil {
			return e
		}
	}
	if len(seen) != len(conditionSet) {
		return errors.New("missing approved verification condition")
	}
	return nil
}
