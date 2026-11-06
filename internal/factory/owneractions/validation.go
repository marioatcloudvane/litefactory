package owneractions

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ArgumentType describes values without transport-specific types.
type ArgumentType string

// Supported argument kinds map to scalar text, whole numbers and yes/no values.
const (
	TextArgument    ArgumentType = "text"
	IntegerArgument ArgumentType = "integer"
	BooleanArgument ArgumentType = "boolean"
)

// Argument declares an accepted command option.
type Argument struct {
	Name        string
	Type        ArgumentType
	Required    bool
	Description string
}

// CommandDefinition is the transport-neutral command catalogue entry.
type CommandDefinition struct {
	Name, Description string
	Arguments         []Argument
	// NaturalLanguage and Effect opt into suggestion routing, never execution.
	NaturalLanguage bool
	Effect          CommandEffect
}

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

// ArgumentError names one rejected argument and why, never its value, so a
// correction can fix exactly that argument. It matches ErrInvalidInvocation.
// An empty Name is an undeclared argument; its untrusted name is not echoed.
type ArgumentError struct {
	Name, Why string
	// Limit is the byte limit an oversized value exceeded, else 0.
	Limit int
}

func (e *ArgumentError) Error() string { return ErrInvalidInvocation.Error() + ": " + e.Detail() }

// Detail is the argument name and reason without the shared prefix.
func (e *ArgumentError) Detail() string {
	if e.Name == "" {
		return "an undeclared argument: " + e.Why
	}
	return e.Name + ": " + e.Why
}

// Is matches the shared invalid-invocation error.
func (e *ArgumentError) Is(target error) bool { return target == ErrInvalidInvocation }

// ValidateDefinition validates a declared read or presentation command.
// Changing operations use the fixed factory schema through ValidateChange.
func ValidateDefinition(def CommandDefinition, in Invocation) (Invocation, CommandEffect, error) {
	if canonical, ok := ChangeDefinition(in.Command); ok {
		def = canonical
	}
	out := Invocation{Command: in.Command, Arguments: make(map[string]string)}
	declared := make(map[string]Argument, len(def.Arguments))
	var faults []error
	for _, arg := range def.Arguments {
		declared[arg.Name] = arg
		if arg.Required && strings.TrimSpace(in.Arguments[arg.Name]) == "" {
			faults = append(faults, &ArgumentError{Name: arg.Name, Why: "missing"})
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
		limit := ArgumentLimit(def.Name, name)
		var fault *ArgumentError
		switch {
		case !known:
			// Unknown field names are untrusted too; do not echo them.
			fault = &ArgumentError{Why: "not an argument of this command"}
		case !utf8.ValidString(value) || strings.ContainsRune(value, 0):
			fault = &ArgumentError{Name: name, Why: "not valid text"}
		case len(value) > limit:
			fault = &ArgumentError{Name: name, Why: fmt.Sprintf("exceeds %d bytes", limit), Limit: limit}
		}
		switch arg.Type {
		case TextArgument:
		case IntegerArgument:
			n, err := strconv.ParseInt(value, 10, 64)
			if fault == nil && (err != nil || n <= 0 || strconv.FormatInt(n, 10) != value) {
				fault = &ArgumentError{Name: name, Why: "not a positive whole number"}
			}
		case BooleanArgument:
			if fault == nil && value != "true" && value != "false" {
				fault = &ArgumentError{Name: name, Why: "must be true or false"}
			}
		default:
			if fault == nil {
				fault = &ArgumentError{Name: name, Why: "has no supported type"}
			}
		}
		if fault != nil {
			faults = append(faults, fault)
		}
		out.Arguments[name] = value
	}
	if err := errors.Join(faults...); err != nil {
		return Invocation{}, "", err
	}
	if (def.Name == "confirm" && len(out.Arguments["code"]) != 8) ||
		(def.Name == "answer" && len(out.Arguments["revision"]) != 12) {
		name := "code"
		if def.Name == "answer" {
			name = "revision"
		}
		return Invocation{}, "", &ArgumentError{Name: name, Why: "not a valid revision code"}
	}
	return out, def.Effect, nil
}

// ArgumentLimit returns the existing scalar input budget.
func ArgumentLimit(command, name string) int {
	if command == "actions" && name == "cursor" {
		return 4096
	}
	if StandardsCommand(command) && protectedArgument(name) {
		return 32768
	}
	if command == BugFileCommand {
		switch name {
		case "description":
			return 8192
		case "repository":
			return 1024
		case "title":
			return 1024
		}
	}
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

func protectedArgument(name string) bool {
	switch name {
	case "text", "note", "intent", "title", "body", "operations", "request", "reason", "config", "departure", "name", "rule", "description", "evidence", "scope":
		return true
	}
	return false
}
