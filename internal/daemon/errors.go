package daemon

import (
	"errors"
	"fmt"
)

// ErrInvalidOptions reports a daemon that cannot be built from the values it
// was given: a nil dependency, an interval outside the D-C clamp, a missing
// instance id.
var ErrInvalidOptions = errors.New("invalid daemon options")

func invalidOptions(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidOptions, fmt.Sprintf(format, args...))
}

// AlreadyRunningError reports that another litefactory daemon holds the
// singleton lock (AC-2.6).
//
// It is a type rather than a sentinel because the message has to name the file:
// "a daemon is already running" sends the reader looking for a process, and the
// useful next step is knowing which lock file is held and therefore which
// $LF_HOME the other instance is using.
type AlreadyRunningError struct {
	// Path is the lock file that could not be acquired.
	Path string
}

func (e *AlreadyRunningError) Error() string {
	return fmt.Sprintf("another litefactory daemon is already running: %s is locked by it", e.Path)
}
