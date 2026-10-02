package secrets

import (
	"context"
	"fmt"
	"time"
)

// WriteError reports a credential that could not be stored, naming the
// reference. It never contains the value.
type WriteError struct {
	// Ref is the keychain item that was being written.
	Ref Reference
	// Err is the cause: [ErrInteractionNotAllowed], [ErrIncomplete], a
	// timeout, or a failure of the tool.
	Err error
}

func (e *WriteError) Error() string {
	return fmt.Sprintf("store secret: %s: %v", e.Ref, e.Err)
}

func (e *WriteError) Unwrap() error { return e.Err }

// Writer stores a value in a keychain generic-password item. Like [Resolver],
// its function field is the test seam: nil uses /usr/bin/security.
type Writer struct {
	// Keychain adds or updates a generic password.
	Keychain func(ctx context.Context, service, account, value string) error
	// KeychainTimeout bounds one write. Zero uses [DefaultKeychainTimeout].
	KeychainTimeout time.Duration
}

// Write stores value under ref, which must be a keychain reference. An existing
// item is updated. The value is never placed in a process argument.
func (w Writer) Write(ctx context.Context, ref Reference, value string) error {
	if ref.Source != SourceKeychain || ref.Service == "" || ref.Account == "" {
		return &WriteError{Ref: ref, Err: ErrIncomplete}
	}
	timeout := w.KeychainTimeout
	if timeout <= 0 {
		timeout = DefaultKeychainTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	write := w.Keychain
	if write == nil {
		write = addGenericPassword
	}
	if err := write(ctx, ref.Service, ref.Account, value); err != nil {
		return &WriteError{Ref: ref, Err: err}
	}
	return nil
}
