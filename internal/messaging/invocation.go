package messaging

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CommandEffect is factory-owned metadata. A suggestion cannot supply it.
type CommandEffect string

// Catalogue effects separate reads from actions requiring owner confirmation.
const (
	ReadEffect   CommandEffect = "read"
	ChangeEffect CommandEffect = "change"
)

// Invocation is a validated suggestion, not an execution authorization.
// Domain resolution and confirmation must happen before calling a handler.
type Invocation struct {
	Command   string
	Arguments map[string]string
}

// ErrInvalidInvocation covers unavailable commands and invalid scalar input.
var ErrInvalidInvocation = errors.New("messaging: invalid natural-language invocation")

// NaturalCommands returns only explicitly classified live catalogue entries.
// Slash declarations continue to include entries without routing metadata.
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
			return validateInvocation(def, in)
		}
	}
	return Invocation{}, "", fmt.Errorf("%w: command unavailable", ErrInvalidInvocation)
}

func validateInvocation(def CommandDefinition, in Invocation) (Invocation, CommandEffect, error) {
	out := Invocation{Command: in.Command, Arguments: make(map[string]string)}
	declared := make(map[string]Argument, len(def.Arguments))
	var faults []error
	for _, arg := range def.Arguments {
		declared[arg.Name] = arg
		if arg.Required && strings.TrimSpace(in.Arguments[arg.Name]) == "" {
			faults = append(faults, fmt.Errorf("%w: missing %s", ErrInvalidInvocation, arg.Name))
		}
	}
	// Stable diagnostics disclose field names, never executable owner content.
	names := make([]string, 0, len(in.Arguments))
	for name := range in.Arguments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := in.Arguments[name]
		arg, known := declared[name]
		valid := known && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= argumentLimit(def.Name, name)
		switch arg.Type {
		case TextArgument:
		case IntegerArgument:
			n, err := strconv.ParseInt(value, 10, 64)
			valid = valid && err == nil && n > 0 && strconv.FormatInt(n, 10) == value
		case BooleanArgument:
			valid = valid && (value == "true" || value == "false")
		default:
			valid = false
		}
		if !valid {
			// Unknown field names are untrusted too; do not echo them.
			faults = append(faults, fmt.Errorf("%w: invalid argument", ErrInvalidInvocation))
		}
		out.Arguments[name] = value
	}
	if err := errors.Join(faults...); err != nil {
		return Invocation{}, "", err
	}
	if (def.Name == "confirm" && len(out.Arguments["code"]) != 8) ||
		(def.Name == "answer" && len(out.Arguments["revision"]) != 12) {
		return Invocation{}, "", fmt.Errorf("%w: invalid revision code", ErrInvalidInvocation)
	}
	return out, def.Effect, nil
}

func argumentLimit(command, name string) int {
	switch name {
	case "question":
		return 8192
	case "text":
		return 65536
	case "note":
		if command == "diagnose" {
			return 8192
		}
		return 2000
	default:
		return 256
	}
}

// DialogueTurn contains only admitted owner text. Intake must filter identity
// and channel before constructing it; unrelated channel history is excluded.
type DialogueTurn struct {
	SourceID string
	Text     string
}

// InterpretationRequest supplies bounded data and the factory's command choices.
// It exposes no store, forge client, runner resume state or execution capability.
type InterpretationRequest struct {
	Turns    []DialogueTurn
	Commands []CommandDefinition
}

// Interpreter suggests one action or asks for clarification. Its output is
// untrusted until strict decoding, source binding and domain validation succeed.
type Interpreter interface {
	Interpret(context.Context, InterpretationRequest) ([]byte, error)
}

// Interpretation is a versioned suggestion. Confirmation is deliberately not
// an output kind: only a separate deterministic owner protocol can authorize it.
type Interpretation struct {
	Version   int           `json:"version"`
	Kind      string        `json:"kind"` // ignore, unsupported, clarify, propose
	Question  string        `json:"question,omitempty"`
	Command   string        `json:"command,omitempty"`
	Arguments []SourceValue `json:"arguments,omitempty"`
}

// SourceValue identifies original owner bytes rather than model-authored
// executable content. Start is inclusive, End exclusive, measured in UTF-8 bytes.
// A span is evidence of origin; it is never evidence of permission or intent.
type SourceValue struct {
	Name     string `json:"name"`
	SourceID string `json:"source_id"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}
