package graphexec

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// ActionSpec defines membership and outcomes, never executable attribute text.
type ActionSpec struct {
	Name     string
	Mode     agent.Mode
	Outcomes []string
}

// Registry is build-then-freeze: registration finishes before any shared use.
// The zero value is ready for registration. Both boundaries clone outcome slices.
type Registry struct {
	actions map[string]ActionSpec
}

var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validActionName(name string, mode agent.Mode) bool {
	prefix := "lf_" + string(mode) + "_"
	return mode.Valid() && strings.HasPrefix(name, prefix) && capabilityPattern.MatchString(strings.TrimPrefix(name, prefix))
}

// Register adds one valid spec; duplicate names never overwrite earlier specs.
func (r *Registry) Register(spec ActionSpec) error {
	if r == nil {
		return errors.New("register graph action: nil registry")
	}
	if !validActionName(spec.Name, spec.Mode) {
		return fmt.Errorf("register graph action %q: name must be lf_<mode>_<capability> for valid mode %q", spec.Name, spec.Mode)
	}
	if len(spec.Outcomes) == 0 {
		return fmt.Errorf("register graph action %q: outcomes must be nonempty", spec.Name)
	}
	seen := make(map[string]bool)
	for _, outcome := range spec.Outcomes {
		if strings.TrimSpace(outcome) == "" || seen[outcome] {
			return fmt.Errorf("register graph action %q: empty or duplicate outcome %q", spec.Name, outcome)
		}
		seen[outcome] = true
	}
	if _, exists := r.actions[spec.Name]; exists {
		return fmt.Errorf("register graph action %q: already registered", spec.Name)
	}
	if r.actions == nil {
		r.actions = make(map[string]ActionSpec)
	}
	spec.Outcomes = slices.Clone(spec.Outcomes)
	r.actions[spec.Name] = spec
	return nil
}

// Resolve reports membership and returns a spec with independently owned slices.
func (r Registry) Resolve(name string) (ActionSpec, bool) {
	spec, ok := r.actions[name]
	spec.Outcomes = slices.Clone(spec.Outcomes)
	return spec, ok
}
