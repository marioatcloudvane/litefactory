package team

import (
	"errors"
	"fmt"
	"strings"
)

// The sentinel errors this package returns. Every error value returned by
// team wraps exactly one of these, so callers can branch with errors.Is
// without matching on message text.
var (
	// ErrUnknownField reports a JSON key in the index document that is not
	// part of its schema at that path, found during the pass-one generic
	// walk (see doc.go).
	ErrUnknownField = errors.New("unknown field in team index")

	// ErrInvalidKey reports an agent key or team name that fails
	// [KeyPattern].
	ErrInvalidKey = errors.New("invalid team or agent key")

	// ErrEmptyField reports an agent entry whose Source or Dest is empty.
	ErrEmptyField = errors.New("empty source or dest")

	// ErrDuplicateKey reports an agent key that appears in more than one
	// location across the index — two teams, or a team and shared.
	ErrDuplicateKey = errors.New("duplicate agent key")
)

// UnknownFieldError reports one JSON key that has no place in the index
// schema. Path is the dotted location of the offending key, for example
// "shared.go-architect.souce" or "teams.go-team.agents.go-architect.souce" —
// the team segment is the team's name when the pass-one walk could read one,
// its array index otherwise (see doc.go).
type UnknownFieldError struct {
	Path string
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("%s: unknown field", e.Path)
}

func (e *UnknownFieldError) Unwrap() error { return ErrUnknownField }

// KeyError reports an agent key or team name that does not match
// [KeyPattern]. Path is where it was found; Key is the offending value.
type KeyError struct {
	Path string
	Key  string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("%s: %q does not match %s", e.Path, e.Key, KeyPattern)
}

func (e *KeyError) Unwrap() error { return ErrInvalidKey }

// EmptyFieldError reports an agent entry field that must not be empty.
type EmptyFieldError struct {
	Path  string
	Field string
}

func (e *EmptyFieldError) Error() string {
	return fmt.Sprintf("%s.%s: must not be empty", e.Path, e.Field)
}

func (e *EmptyFieldError) Unwrap() error { return ErrEmptyField }

// DuplicateKeyError reports an agent key found in more than one location.
// Locations lists every place it was found, sorted, not just the second.
type DuplicateKeyError struct {
	Key       string
	Locations []string
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("agent key %q appears in more than one place: %s", e.Key, strings.Join(e.Locations, ", "))
}

func (e *DuplicateKeyError) Unwrap() error { return ErrDuplicateKey }
