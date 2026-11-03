package messaging

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
)

// CommandEffect retains the factory-owned command classification.
type CommandEffect = owneractions.CommandEffect

// Invocation retains the shared validated command value.
type Invocation = owneractions.Invocation

// ArgumentError identifies one rejected argument without disclosing its value.
type ArgumentError = owneractions.ArgumentError

// Catalogue effects retain their existing wire values.
const (
	ReadEffect   = owneractions.ReadEffect
	ChangeEffect = owneractions.ChangeEffect
)

// ErrInvalidInvocation retains the existing invalid-input error identity.
var ErrInvalidInvocation = owneractions.ErrInvalidInvocation

// NaturalCommands returns only explicitly classified live catalogue entries.
// Declarations continue to include entries without routing metadata.
func (s *Service) NaturalCommands() []CommandDefinition {
	var out []CommandDefinition
	for _, def := range s.Declarations() {
		if def.NaturalLanguage && (def.Effect == ReadEffect || def.Effect == ChangeEffect) {
			out = append(out, def)
		}
	}
	return out
}

// ValidateInvocation checks a suggestion against the live catalogue and copies
// its arguments. It neither resolves a target nor calls a command handler.
func (s *Service) ValidateInvocation(in Invocation) (Invocation, CommandEffect, error) {
	for _, def := range s.NaturalCommands() {
		if def.Name == in.Command {
			if _, ok := owneractions.ChangeDefinition(in.Command); ok {
				return s.actionAvailability().ValidateInvocation(in)
			}
			return owneractions.ValidateDefinition(def, in)
		}
	}
	return Invocation{}, "", fmt.Errorf("%w: command unavailable", ErrInvalidInvocation)
}

// validateInvocation keeps protocol parsing on the same factory-owned schemas.
func validateInvocation(def CommandDefinition, in Invocation) (Invocation, CommandEffect, error) {
	return owneractions.ValidateDefinition(def, in)
}

func argumentLimit(command, name string) int { return owneractions.ArgumentLimit(command, name) }

// DialogueTurn contains only admitted owner text. Intake must filter identity
// and channel before constructing it; unrelated channel history is excluded.
type DialogueTurn struct {
	SourceID string
	Text     string
}
