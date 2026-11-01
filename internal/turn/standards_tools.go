package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func standardsConsumerKey(mode, node string) string {
	if node != "" {
		return "node:" + node + ":role:" + mode
	}
	return "role:" + mode
}

func standardsSessionConsumerKey(sessionID int64, mode, node string) string {
	return fmt.Sprintf("session:%d:%s", sessionID, standardsConsumerKey(mode, node))
}

// StandardsToolService offers a role the published standards that apply to it.
// It is installed only for roles whose graph node grants book access; nothing
// requires a call. The book is pinned on first use, so a role that never asks
// costs nothing and an empty or unavailable book only shows in the tool reply.
type StandardsToolService struct {
	runtime standards.Runtime
	binding store.ResultBinding
	role    string
	// mu guards bound: the book is pinned on a call that retains a binding,
	// and every later call reads that same binding.
	mu    sync.Mutex
	bound bool
}

// NewStandardsToolService copies immutable scope; arguments never select a run,
// consumer or role.
func NewStandardsToolService(runtime *standards.Runtime, binding store.ResultBinding, role string) (*StandardsToolService, error) {
	if runtime == nil || runtime.Store == nil || runtime.RootRunID == "" || runtime.ConsumerKey == "" || runtime.AttemptID != binding.Attempt || strings.TrimSpace(role) == "" {
		return nil, errors.New("standards tools require matching trusted attempt scope and role")
	}
	return &StandardsToolService{runtime: *runtime, binding: binding, role: role}, nil
}

// pin retains the current published book for this invocation on first use. An
// existing retained binding is kept, so later calls read the same book. A call
// that could not retain a binding is not latched: the next call tries again,
// and the returned reason names why this one failed ("" once a binding is
// retained).
func (s *StandardsToolService) pin(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bound {
		return ""
	}
	status, err := s.runtime.Bind(ctx, true)
	if _, getErr := s.runtime.Store.GetStandardsRunBinding(ctx, s.runtime.RootRunID); getErr == nil {
		s.bound = true
		return ""
	} else if err == nil && !errors.Is(getErr, store.ErrNotFound) {
		err = getErr
	}
	reason := "standards binding could not be retained"
	switch {
	case err != nil:
		reason += ": " + err.Error()
	case status.StatusPersistenceFailure != "":
		reason += ": " + status.StatusPersistenceFailure
	}
	reason = clipText(reason, 512)
	slog.Warn("standards binding could not be retained", slog.String("root_run", s.runtime.RootRunID), slog.String("attempt", s.binding.Attempt), slog.String("role", s.role), slog.String("reason", reason))
	return reason
}

// Binding returns the immutable factory attempt scope.
func (s *StandardsToolService) Binding() store.ResultBinding { return s.binding }

// PermissionMaterial binds the root, consumer and role to the attempt lease.
func (s *StandardsToolService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(struct {
		Binding                       store.ResultBinding
		Root, Consumer, Role, Version string
	}{s.binding, s.runtime.RootRunID, s.runtime.ConsumerKey, s.role, "retained-standards-v3"})
}

// Registrations exposes the role's category list and read, without management
// authority.
func (s *StandardsToolService) Registrations() []FamilyRegistration {
	return []FamilyRegistration{{Name: "standards_categories", Owner: s, Handle: s.categories}, {Name: "standards_read", Owner: s, Handle: s.read}}
}
func standardsReply(id string, value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded, err := mcpadapter.EncodeToolResult(submission.Response{RequestID: id, Result: raw})
	if err != nil {
		return nil, err
	}
	if len(encoded) > 32<<10 {
		return nil, errors.New("standards encoded result exceeds 32 KiB")
	}
	return raw, nil
}
func (s *StandardsToolService) categories(ctx context.Context, id string, raw []byte) (json.RawMessage, error) {
	var a struct{}
	if err := decodeMCPArguments(raw, &a); err != nil {
		return nil, err
	}
	pinFailure := s.pin(ctx)
	out, err := s.runtime.CategoriesForRole(ctx, s.role)
	if err != nil {
		return s.failure(ctx, id, withPinFailure(err, pinFailure))
	}
	if pinFailure != "" {
		out.Note = clipText(strings.TrimSpace(out.Note+" "+pinFailure+"."), 1024)
		out.Status.StatusPersistenceFailure = pinFailure
	}
	out.Status = s.observeStatus(ctx, out.Status)
	return standardsReply(id, out)
}

func (s *StandardsToolService) read(ctx context.Context, id string, raw []byte) (json.RawMessage, error) {
	var a struct {
		Categories []string `json:"categories"`
		Cursor     string   `json:"cursor"`
	}
	if err := decodeMCPArguments(raw, &a); err != nil {
		return nil, err
	}
	pinFailure := s.pin(ctx)
	if len(a.Cursor) > 4096 || len(a.Categories) > 100 {
		return nil, standards.ErrInvalid
	}
	out, err := s.runtime.ForRole(ctx, s.role, a.Categories, a.Cursor)
	if err != nil {
		return s.failure(ctx, id, withPinFailure(err, pinFailure))
	}
	out.Status = s.observeStatus(ctx, out.Status)
	return standardsReply(id, out)
}

// withPinFailure adds why the binding could not be retained to a tool failure,
// so the reply names the cause and not only the missing binding.
func withPinFailure(err error, pinFailure string) error {
	if pinFailure == "" {
		return err
	}
	return fmt.Errorf("%s: %w", pinFailure, err)
}

func (s *StandardsToolService) failure(ctx context.Context, id string, cause error) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	code := "standards_failure"
	if errors.Is(cause, standards.ErrInvalid) || errors.Is(cause, standards.ErrUnknownCategory) {
		code = "invalid_arguments"
	}
	status, err := s.runtime.Status(ctx)
	if err != nil {
		status = standards.RuntimeStatus{Integrity: "unavailable", SelectionStatus: "unknown", RetrievalStatus: "pending", StatusPersistenceFailure: "status could not be read"}
	}
	status = s.observeStatus(ctx, status)
	return standardsReply(id, struct {
		Valid  bool                    `json:"valid"`
		Code   string                  `json:"code"`
		Reason string                  `json:"reason"`
		Status standards.RuntimeStatus `json:"status"`
	}{false, code, clipText(cause.Error(), 512), standards.BoundedStatus(status)})
}

func (s *StandardsToolService) observeStatus(ctx context.Context, status standards.RuntimeStatus) standards.RuntimeStatus {
	err := s.runtime.Store.AppendStandardsRunObservation(ctx, store.StandardsRunObservation{RootRunID: s.runtime.RootRunID, ConsumerKey: s.runtime.ConsumerKey, AttemptID: s.binding.Attempt, Kind: "verification", Outcome: status.Integrity, Reason: status.Reason})
	if status.SelectionReason != "" {
		version := int64(0)
		if status.Selection != nil {
			version = status.Selection.Version
		}
		err = errors.Join(err, s.runtime.Store.AppendStandardsRunObservation(ctx, store.StandardsRunObservation{RootRunID: s.runtime.RootRunID, ConsumerKey: s.runtime.ConsumerKey, AttemptID: s.binding.Attempt, Kind: "selection", Outcome: status.SelectionStatus, Reason: status.SelectionReason, SelectionVersion: version}))
	}
	if err != nil {
		status.StatusPersistenceFailure = "verification observation could not be retained"
	}
	return status
}
