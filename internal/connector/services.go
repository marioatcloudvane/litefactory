package connector

import (
	"context"
	"reflect"
	"sync/atomic"

	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

// OwnerActions exposes reviewed owner operations, without worker execution,
// domain configuration, delivery or recovery authority.
type OwnerActions interface {
	Propose(context.Context, owneraction.Principal, owneraction.Invocation, string) (owneraction.Proposal, error)
	Confirm(context.Context, owneraction.Principal, string) (owneraction.Proposal, error)
	Cancel(context.Context, owneraction.Principal, string) (owneraction.Proposal, error)
	Inspect(context.Context, owneraction.Principal, string) (owneraction.Proposal, error)
	HistoryFiltered(context.Context, owneraction.Principal, owneraction.Filters, string, int) (owneraction.Page, error)
	Catalog() []owneraction.ActionDefinition
}

// Workflows exposes scoped discovery, proposal and inspection operations.
// Workflow execution and domain registration remain with the factory owner.
type Workflows interface {
	List(context.Context, owneraction.Principal, string, string, int) (durableworkflow.Page, error)
	Describe(context.Context, owneraction.Principal, string, string, string) (durableworkflow.Summary, error)
	Propose(context.Context, owneraction.Principal, string, string, string, map[string]any, string) (durableworkflow.Proposal, error)
	Continue(context.Context, owneraction.Principal, durableworkflow.Continuation, string) (durableworkflow.Proposal, error)
	Get(context.Context, owneraction.Principal, string, string, int) (durableworkflow.JobView, error)
}

// StageDecisions exposes reviewed stage proposals and their durable status.
type StageDecisions interface {
	Ready() bool
	Propose(context.Context, owneraction.Principal, string, stagedecision.Request) (owneraction.Proposal, error)
	Get(context.Context, owneraction.Principal, string) (stagedecision.View, error)
	Cancel(context.Context, owneraction.Principal, string, string) (stagedecision.View, error)
}

// TaskDiscussion exposes separately consented advisory and publication requests.
// Its background execution and pending publication recovery stay factory-owned.
type TaskDiscussion interface {
	Open(context.Context, owneraction.Principal, string, string) (taskdiscussion.Conversation, error)
	Ask(context.Context, owneraction.Principal, taskdiscussion.Request) (taskdiscussion.JobView, error)
	Get(context.Context, owneraction.Principal, string, string) (taskdiscussion.JobView, error)
	Cancel(context.Context, owneraction.Principal, string) (taskdiscussion.JobView, error)
	Publish(context.Context, owneraction.Principal, taskdiscussion.PublicationRequest) (taskdiscussion.PublicationView, error)
	Publication(context.Context, owneraction.Principal, string, bool) (taskdiscussion.PublicationView, error)
	PublicationAvailable() bool
}

// serviceSlot publishes a complete interface value during lazy composition.
// An immutable holder preserves atomic replacement without copying a service.
type serviceSlot[T any] struct {
	value atomic.Pointer[serviceValue[T]]
}

type serviceValue[T any] struct{ service T }

func (s *serviceSlot[T]) Store(service T) {
	value := reflect.ValueOf(service)
	// Concrete pointer dependencies previously represented unavailability with
	// nil. Preserve that behavior when trusted composition supplies typed nil.
	if !value.IsValid() || value.Kind() == reflect.Pointer && value.IsNil() {
		s.value.Store(nil)
		return
	}
	s.value.Store(&serviceValue[T]{service: service})
}

func (s *serviceSlot[T]) Load() T {
	if value := s.value.Load(); value != nil {
		return value.service
	}
	var unavailable T
	return unavailable
}
