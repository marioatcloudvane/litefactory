package render

import (
	"errors"
	"fmt"
)

// ErrUnsupportedField is the sentinel every [*UnsupportedFieldError] wraps,
// so a caller can branch with errors.Is without matching on message text.
var ErrUnsupportedField = errors.New("frontmatter field not supported by harness")

// UnsupportedFieldError reports that a [Renderer] was given a frontmatter
// field it cannot express in its target harness's output format. AC-2
// requires this to fail loudly: a Renderer MUST return this (or an error
// wrapping it) instead of silently dropping the field or writing a partial
// result, and the caller MUST discard every [RenderedFile] the renderer
// produced once it returns any error — there is no partial write.
type UnsupportedFieldError struct {
	// Agent is the agent this frontmatter belongs to — [Frontmatter].Name,
	// the same key the agent is registered under in internal/team's Index.
	Agent string
	// Harness is the registry key of the renderer that rejected the field
	// (e.g. "claude", "codex", "opencode").
	Harness string
	// Field is the dotted path of the offending frontmatter field, e.g.
	// "model" or "tools.network_access".
	Field string
}

func (e *UnsupportedFieldError) Error() string {
	return fmt.Sprintf("agent %q: harness %q cannot express frontmatter field %q", e.Agent, e.Harness, e.Field)
}

func (e *UnsupportedFieldError) Unwrap() error { return ErrUnsupportedField }

// ErrUnknownHarness is the sentinel [Lookup] wraps when asked for a harness
// name that no package has registered a [Renderer] under.
var ErrUnknownHarness = errors.New("unknown renderer harness")

// UnknownHarnessError names the harness that was looked up and the harness
// names that are actually registered, sorted, so a caller can report both
// what was asked for and what exists.
type UnknownHarnessError struct {
	Harness    string
	Registered []string
}

func (e *UnknownHarnessError) Error() string {
	return fmt.Sprintf("unknown renderer harness %q (registered: %v)", e.Harness, e.Registered)
}

func (e *UnknownHarnessError) Unwrap() error { return ErrUnknownHarness }
