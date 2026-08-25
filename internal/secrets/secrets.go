// Package secrets resolves the bot forge token and holds it in a type that
// cannot be printed.
//
// litefactory stores no credential of its own. config.yaml holds a *reference*
// — a macOS keychain item, or the name of an environment variable — and this
// package turns that reference into a [Token] once, at process start, which is
// then held for the process lifetime (S-5). Nothing re-reads the keychain
// per request.
//
// # Keychain access prompts, and why every rebuild re-prompts
//
// The keychain lookup shells out to /usr/bin/security rather than linking
// keybase/go-keychain, which needs cgo and would cost litefactory its
// cgo-free single-binary build.
//
// macOS gates keychain access on the *identity of the calling binary*. An
// unsigned binary — which a locally built litefactory is — has no stable
// identity, so the OS shows an allow-access dialog again after every rebuild,
// even if you previously clicked "Always Allow". Click **Always Allow**: it
// suppresses the prompt until the next build.
//
// Under launchd the same dialog may be raised where no one can see it, which
// would wedge the daemon at startup. Two things guard against that: the
// keychain lookup carries its own timeout so the daemon fails loudly instead
// of hanging forever, and the token is resolved exactly once at start rather
// than per tick, so there is no prompt storm.
//
// # Security sessions
//
// A dialog can also be refused outright rather than hung on. Every macOS
// process belongs to a security session, and only a session attached to the
// desktop login — launchctl managername reports Aqua — may put a window on
// screen. A process in a Background session, which is what launchd's system
// domain, an ssh login and some tool-run shells give you, gets
// [ErrInteractionNotAllowed] the moment the keychain wants to ask a question:
// to unlock the keychain, or to confirm that this binary may read the item.
//
// This is not a broken reference, and the fix is never in config.yaml. Run the
// daemon from a session that owns the desktop, unlock the login keychain, or
// use github.token_env, which needs no session at all.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// DefaultKeychainTimeout bounds one /usr/bin/security lookup.
//
// It is long enough for a human to notice the allow-access dialog and click
// Always Allow, and short enough that a daemon facing a dialog nobody can see
// fails with a diagnosable error instead of hanging indefinitely.
const DefaultKeychainTimeout = 2 * time.Minute

// Source names where a token is read from.
type Source string

// The supported credential sources, in the resolution order of S-5.
const (
	// SourceKeychain is a macOS keychain generic-password item. It is the only
	// source a launchd-started daemon supports (D-D).
	SourceKeychain Source = "keychain"
	// SourceEnv is an environment variable of the daemon process, named by
	// config. It is the development path, for a foreground `daemon run`.
	SourceEnv Source = "env"
)

// Reference is the indirection config.yaml stores in place of a credential.
// It is safe to log and to print in an error message: by construction it
// contains no secret material.
type Reference struct {
	// Source selects which of the fields below is meaningful.
	Source Source
	// Service and Account identify a keychain generic-password item.
	Service string
	Account string
	// EnvVar is the name of an environment variable.
	EnvVar string
}

// KeychainRef references a keychain generic-password item.
func KeychainRef(service, account string) Reference {
	return Reference{Source: SourceKeychain, Service: service, Account: account}
}

// EnvRef references an environment variable by name.
func EnvRef(name string) Reference {
	return Reference{Source: SourceEnv, EnvVar: name}
}

// ReferenceFrom builds the reference described by the three configuration
// values, and reports whether any credential is configured at all. Zero
// configured sources is not an error here: `init` treats a missing bot
// credential as a warning, and only the daemon refuses to start without one.
//
// Keychain wins if both are somehow set, matching the S-5 resolution order.
// Configuration validation rejects that combination before this is reached,
// so it is a defensive tie-break rather than a supported mode.
func ReferenceFrom(service, account, envVar string) (Reference, bool) {
	switch {
	case service != "" || account != "":
		return KeychainRef(service, account), true
	case envVar != "":
		return EnvRef(envVar), true
	default:
		return Reference{}, false
	}
}

// String describes the reference in the terms the user configured it with, so
// a failure message names exactly what to go and fix.
func (r Reference) String() string {
	switch r.Source {
	case SourceKeychain:
		return fmt.Sprintf("keychain item (service %q, account %q)", r.Service, r.Account)
	case SourceEnv:
		return fmt.Sprintf("environment variable $%s", r.EnvVar)
	default:
		return "no configured token reference"
	}
}

// Causes of a failed resolution, for errors.Is.
var (
	// ErrNotConfigured means config.yaml names no credential at all.
	ErrNotConfigured = errors.New("no bot token is configured; set github.token_keychain or github.token_env")
	// ErrNotFound means the reference points at something that is not there.
	ErrNotFound = errors.New("not found")
	// ErrEmpty means the reference resolved, to nothing.
	ErrEmpty = errors.New("resolved to an empty value")
	// ErrInteractionNotAllowed means the keychain item is there but macOS
	// would not hand it over without a dialog, and the process asking is in no
	// position to show one. See the note on keychain sessions below.
	ErrInteractionNotAllowed = errors.New("macOS would not open the item without a dialog it could not show; " +
		"the process is not in a desktop login session, or the login keychain is locked")
	// ErrIncomplete means a reference is half-configured.
	ErrIncomplete = errors.New("incomplete reference")
)

// ResolveError reports a credential that could not be read, naming the
// reference that was tried (S-5). It never contains credential material.
type ResolveError struct {
	// Ref is what litefactory looked for.
	Ref Reference
	// Err is the cause, one of the sentinels above or a lookup failure.
	Err error
}

func (e *ResolveError) Error() string {
	if e.Ref.Source == "" {
		return fmt.Sprintf("bot token: %v", e.Err)
	}
	return fmt.Sprintf("bot token: %s: %v", e.Ref, e.Err)
}

func (e *ResolveError) Unwrap() error { return e.Err }

// Resolver reads a [Token] from a [Reference].
//
// The two function fields are the test seam: leave them nil in production and
// the real macOS keychain and process environment are used. There is no
// interface here because there is exactly one thing to substitute and no
// second implementation worth naming.
type Resolver struct {
	// Keychain reads a generic password. Nil uses /usr/bin/security.
	Keychain func(ctx context.Context, service, account string) (string, error)
	// LookupEnv reads an environment variable. Nil uses os.LookupEnv.
	LookupEnv func(name string) (string, bool)
	// KeychainTimeout bounds one keychain lookup. Zero uses
	// [DefaultKeychainTimeout].
	KeychainTimeout time.Duration
}

// Resolve reads the credential ref points at.
//
// A reference that is missing, incomplete or resolves to an empty value is a
// [ResolveError] naming the reference, which is what lets the daemon refuse to
// start with a message the user can act on rather than "unauthorized" from the
// first API call.
func (r Resolver) Resolve(ctx context.Context, ref Reference) (Token, error) {
	switch ref.Source {
	case SourceKeychain:
		return r.fromKeychain(ctx, ref)
	case SourceEnv:
		return r.fromEnv(ref)
	default:
		return Token{}, &ResolveError{Ref: ref, Err: ErrNotConfigured}
	}
}

func (r Resolver) fromKeychain(ctx context.Context, ref Reference) (Token, error) {
	if ref.Service == "" || ref.Account == "" {
		return Token{}, &ResolveError{Ref: ref, Err: ErrIncomplete}
	}

	timeout := r.KeychainTimeout
	if timeout <= 0 {
		timeout = DefaultKeychainTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	lookup := r.Keychain
	if lookup == nil {
		lookup = findGenericPassword
	}
	value, err := lookup(ctx, ref.Service, ref.Account)
	if err != nil {
		return Token{}, &ResolveError{Ref: ref, Err: err}
	}
	if value == "" {
		return Token{}, &ResolveError{Ref: ref, Err: ErrEmpty}
	}
	return NewToken(value), nil
}

func (r Resolver) fromEnv(ref Reference) (Token, error) {
	if ref.EnvVar == "" {
		return Token{}, &ResolveError{Ref: ref, Err: ErrIncomplete}
	}
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(ref.EnvVar)
	if !ok {
		return Token{}, &ResolveError{Ref: ref, Err: ErrNotFound}
	}
	if value == "" {
		return Token{}, &ResolveError{Ref: ref, Err: ErrEmpty}
	}
	return NewToken(value), nil
}
