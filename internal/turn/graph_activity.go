package turn

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// ActivityRequest is one bounded dispatch. Attrs belongs to the handler;
// ResumeRef is runtime-only and conveys no implemented session-resume behavior.
type ActivityRequest struct {
	Team                             string
	Mode                             agent.Mode
	Node                             string
	Attrs                            map[string]string
	InstanceID, NodeAttemptID, RunID int64
	ResumeRef                        string
}

// ActivityResult distinguishes a conducted failure outcome from inability to
// conduct work. Kind uses store's closed graph output vocabulary. Diagnostic is
// retained as opaque diagnostic output; future publishers must redact it.
type ActivityResult struct {
	// ProcessResult retains subprocess facts independently from private node validation.
	ProcessResult *agent.RunResult
	Outcome       string
	Kind          string
	Output        []byte
	Diagnostic    string
}

// ActivityHandler is owned by the consumer. Conducted business failures return
// declared outcomes and nil error. Errors mean work could not be conducted or
// recorded. Implementations must not retain Options or mutate its dependencies.
type ActivityHandler interface {
	Execute(context.Context, Options, ActivityRequest) (ActivityResult, error)
}

// ActivityRegistry is constructed once and frozen before sharing. Registration
// never replaces an existing handler. The zero value is ready for registration.
type ActivityRegistry struct{ handlers map[string]ActivityHandler }

// Register adds one non-nil handler under an action name.
func (r *ActivityRegistry) Register(name string, handler ActivityHandler) error {
	if r == nil || strings.TrimSpace(name) == "" || nilActivityHandler(handler) {
		return fmt.Errorf("register activity %q: registry, name and handler are required", name)
	}
	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("register activity %q: already registered", name)
	}
	if r.handlers == nil {
		r.handlers = make(map[string]ActivityHandler)
	}
	r.handlers[name] = handler
	return nil
}

// Resolve refuses missing or nil handlers rather than permitting nil dispatch.
func (r ActivityRegistry) Resolve(name string) (ActivityHandler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok && !nilActivityHandler(handler)
}

func nilActivityHandler(handler ActivityHandler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
