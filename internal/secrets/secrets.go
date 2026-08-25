// Package secrets resolves the bot forge token and holds it in a type that
// cannot be printed.
//
// litefactory stores no credential of its own. config.yaml holds a *reference*
// — a macOS keychain item, or the name of an environment variable — and this
// package turns that reference into a [Token] once, at process start, which is
// then held for the process lifetime (S-5). Nothing re-reads the keychain
// per request.
//
// # A locked keychain, and why it looks like a permissions problem
//
// The keychain lookup shells out to /usr/bin/security rather than linking
// keybase/go-keychain, which needs cgo and would cost litefactory its
// cgo-free single-binary build. One consequence is worth stating plainly:
// keychain access control gates the process that calls the keychain API, and
// that process is /usr/bin/security, not litefactory. An item added with
// `security add-generic-password` already trusts security to read it back, so
// a locally built, unsigned litefactory raises no allow-access dialog and
// rebuilding it changes nothing.
//
// What does stop a lookup is a locked keychain. Reading from a locked one
// needs a password, asking for it needs a dialog, and a process that cannot
// show one gets [ErrInteractionNotAllowed] — OSStatus -25308, which `security`
// reports as exit 36 with an empty stderr. The login keychain unlocks at
// console login and re-locks on sleep, so this arrives as "it worked this
// morning" rather than as a configuration error. `security unlock-keychain`
// prompts on the terminal rather than on screen, so it fixes this from ssh and
// from a background session too.
//
// The token is resolved exactly once at process start rather than per tick, so
// one unlock covers a whole daemon lifetime and there is no prompt storm.
// Under launchd a dialog could still be raised where nobody can see it, so the
// lookup carries its own timeout and the daemon fails loudly instead of
// hanging forever.
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
// It is long enough for a human at the console to answer an unlock dialog, and
// short enough that a daemon facing a dialog nobody can see fails with a
// diagnosable error instead of hanging indefinitely.
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
	// ErrInteractionNotAllowed means the keychain item is there but the
	// keychain holding it is locked, and unlocking needs a password nobody
	// offered. See the note on locked keychains above.
	ErrInteractionNotAllowed = errors.New("the login keychain is locked; unlock it with `security unlock-keychain`")
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
