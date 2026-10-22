package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// StandardsToolService exposes advisory retrieval only, under factory identities.
// It is installed only for roles whose graph node grants book access; nothing
// requires a call. The book is pinned on first use, so a role that never asks
// costs nothing and an empty or unavailable book only shows in the tool reply.
type StandardsToolService struct {
	runtime standards.Runtime
	binding store.ResultBinding
	bind    sync.Once
}

// NewStandardsToolService copies immutable scope; arguments never select a run or consumer.
func NewStandardsToolService(runtime *standards.Runtime, binding store.ResultBinding) (*StandardsToolService, error) {
	if runtime == nil || runtime.Store == nil || runtime.RootRunID == "" || runtime.ConsumerKey == "" || runtime.AttemptID != binding.Attempt {
		return nil, errors.New("standards tools require matching trusted attempt scope")
	}
	return &StandardsToolService{runtime: *runtime, binding: binding}, nil
}

// pin retains the current published book for this invocation on first use. An
// existing retained binding is kept, so later calls read the same book.
func (s *StandardsToolService) pin(ctx context.Context) {
	s.bind.Do(func() {
		_, _ = s.runtime.Bind(ctx, true)
	})
}

// Binding returns the immutable factory attempt scope.
func (s *StandardsToolService) Binding() store.ResultBinding { return s.binding }

// PermissionMaterial binds the root and consumer to the attempt lease.
func (s *StandardsToolService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(struct {
		Binding                 store.ResultBinding
		Root, Consumer, Version string
	}{s.binding, s.runtime.RootRunID, s.runtime.ConsumerKey, "retained-standards-v1"})
}

// Registrations exposes retrieval operations without management authority.
func (s *StandardsToolService) Registrations() []FamilyRegistration {
	return []FamilyRegistration{{Name: "standards_categories_list", Owner: s, Handle: s.categories}, {Name: "standards_select", Owner: s, Handle: s.selectCategories}, {Name: "standards_read", Owner: s, Handle: s.read}}
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
		return nil, errors.New("standards encoded result exceeds 32 KiB; retained selection or metadata is too large")
	}
	return raw, nil
}
func (s *StandardsToolService) categories(ctx context.Context, id string, raw []byte) (json.RawMessage, error) {
	var a struct {
		PageSize int    `json:"page_size"`
		Cursor   string `json:"cursor"`
	}
	if err := decodeMCPArguments(raw, &a); err != nil {
		return nil, err
	}
	s.pin(ctx)
	if len(a.Cursor) > 4096 {
		return nil, standards.ErrInvalid
	}
	out, err := s.runtime.Categories(ctx, a.PageSize, a.Cursor)
	if err != nil {
		return s.failure(ctx, id, err)
	}
	out.Status = s.observeStatus(ctx, out.Status)
	return standardsReply(id, out)
}
func (s *StandardsToolService) selectCategories(ctx context.Context, id string, raw []byte) (json.RawMessage, error) {
	var a struct {
		ExpectedVersion *int64    `json:"expected_version"`
		CategoryIDs     *[]string `json:"category_ids"`
		Reason          string    `json:"reason"`
	}
	if err := decodeMCPArguments(raw, &a); err != nil {
		return nil, err
	}
	s.pin(ctx)
	if a.ExpectedVersion == nil || a.CategoryIDs == nil || len(*a.CategoryIDs) > 100 || strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 512 {
		return nil, standards.ErrInvalid
	}
	for _, id := range *a.CategoryIDs {
		if id == "" || len(id) > 256 {
			return nil, standards.ErrInvalid
		}
	}
	out, err := s.runtime.Select(ctx, *a.ExpectedVersion, *a.CategoryIDs, a.Reason)
	if err != nil {
		return s.failure(ctx, id, err)
	}
	out = s.observeStatus(ctx, out)
	return standardsReply(id, standards.BoundedStatus(out))
}
func (s *StandardsToolService) read(ctx context.Context, id string, raw []byte) (json.RawMessage, error) {
	var a struct {
		SelectionVersion int64  `json:"selection_version"`
		SelectionDigest  string `json:"selection_digest"`
		Cursor           string `json:"cursor"`
		Complete         bool   `json:"complete"`
	}
	if err := decodeMCPArguments(raw, &a); err != nil {
		return nil, err
	}
	s.pin(ctx)
	if a.SelectionVersion < 1 || len(a.SelectionDigest) == 0 || len(a.SelectionDigest) > 128 || len(a.Cursor) > 4096 {
		return nil, standards.ErrInvalid
	}
	out, err := s.runtime.Read(ctx, a.SelectionVersion, a.SelectionDigest, a.Cursor, a.Complete)
	if err != nil {
		return s.failure(ctx, id, err)
	}
	out.Status = s.observeStatus(ctx, out.Status)
	return standardsReply(id, out)
}

func (s *StandardsToolService) failure(ctx context.Context, id string, cause error) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	code := "standards_failure"
	if errors.Is(cause, store.ErrStandardsConflict) {
		code = "selection_conflict"
	}
	if errors.Is(cause, standards.ErrInvalid) {
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
