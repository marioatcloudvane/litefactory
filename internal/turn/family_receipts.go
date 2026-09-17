package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// ReceiptDimensions are independent observed workflow states. No authored
// payload, permit, grant or descriptor is part of the public projection.
type ReceiptDimensions struct {
	Input       string `json:"input,omitempty"`
	Engineering string `json:"engineering,omitempty"`
	Acceptance  string `json:"acceptance,omitempty"`
	Effects     string `json:"effects,omitempty"`
}

// PublicationIdentity contains only confirmed remote identifiers and bounded
// relationship state; an unresolved artifact has no invented remote identity.
type PublicationIdentity struct {
	IssueID      int64  `json:"issue_id,omitempty"`
	IssueNumber  int    `json:"issue_number,omitempty"`
	CommentID    int64  `json:"comment_id,omitempty"`
	Relationship string `json:"relationship,omitempty"`
}

// FamilyReceipt observes one request, without dispatching an effect or recording
// another receipt. Missing means the factory has no retained in-scope record.
type FamilyReceipt struct {
	RequestID    string                `json:"request_id"`
	Tool         string                `json:"tool,omitempty"`
	Disposition  string                `json:"disposition"`
	Reference    store.FamilyReference `json:"reference,omitempty"`
	States       ReceiptDimensions     `json:"states"`
	Publications []PublicationIdentity `json:"publications,omitempty"`
}

// FamilyProjection is an observation-only seam for domain owners. The ledger's
// scope has already been authorized; callbacks must never retry an effect.
type FamilyProjection func(context.Context, store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error)

// ReceiptService binds copied factory reader scopes and immutable projections.
// Construction does not mint authority. Its PermissionMaterial must be included
// by the factory in the complete dispatcher permission digest before lease bind.
type ReceiptService struct {
	db          *store.DB
	binding     store.ResultBinding
	scope       store.FamilyScope
	readers     []store.FamilyScope
	projections map[string]FamilyProjection
}

// NewReceiptService copies factory reader scopes and projection registrations for immutable binding.
func NewReceiptService(db *store.DB, b store.ResultBinding, historical []store.FamilyScope, projections map[string]FamilyProjection) (*ReceiptService, error) {
	ref, err := naming.ParseRef(b.Task)
	if err != nil || db == nil || b.RunID < 1 || b.Attempt == "" || len(historical) > 63 {
		return nil, errors.New("receipts require immutable factory scope")
	}
	scope := store.FamilyScope{Repository: ref.Owner + "/" + ref.Repo, Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}
	readers := []store.FamilyScope{scope}
	for _, s := range historical {
		// This feature's history grants remain repository/task local. A current
		// reader can inspect an old attempt without inheriting its dispatch rights.
		if s.Repository != scope.Repository || s.Task != scope.Task || s.RunID < 1 || s.Attempt == "" || len(s.Attempt) > 256 {
			return nil, store.ErrResultAuthority
		}
		if !slices.Contains(readers, s) {
			readers = append(readers, s)
		}
	}
	slices.SortFunc(readers, func(a, b store.FamilyScope) int {
		if a.RunID < b.RunID {
			return -1
		}
		if a.RunID > b.RunID {
			return 1
		}
		return strings.Compare(a.Attempt, b.Attempt)
	})
	copied := make(map[string]FamilyProjection, len(projections))
	for kind, p := range projections {
		if kind == "" || len(kind) > 64 || p == nil {
			return nil, errors.New("invalid receipt projection")
		}
		copied[kind] = p
	}
	return &ReceiptService{db: db, binding: b, scope: scope, readers: readers, projections: copied}, nil
}

// Binding returns the current reader binding without restoring historical authority.
func (s *ReceiptService) Binding() store.ResultBinding { return s.binding }

// PermissionMaterial returns a fresh, deterministic snapshot for catalog binding.
func (s *ReceiptService) PermissionMaterial() ([]byte, error) {
	kinds := make([]string, 0, len(s.projections))
	for kind := range s.projections {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return json.Marshal(struct {
		Scopes      []store.FamilyScope
		Projections []string
	}{s.readers, kinds})
}

// Begin records authenticated calls before domain handling, including calls that
// are subsequently rejected by field/profile validation. It stores no payload.
func (s *ReceiptService) Begin(ctx context.Context, requestID, tool string, payload []byte) (store.FamilyRequest, bool, error) {
	hash := sha256.Sum256(payload)
	return s.db.BeginFamilyRequest(ctx, s.binding, store.FamilyRequest{Scope: s.scope, RequestID: requestID, Tool: tool, PayloadHash: hex.EncodeToString(hash[:])})
}

// Finish retains a bounded disposition and reference to the authoritative family outcome.
func (s *ReceiptService) Finish(ctx context.Context, requestID, disposition string, ref store.FamilyReference) error {
	return s.db.FinishFamilyRequest(ctx, s.binding, requestID, disposition, ref)
}

// Get performs bounded observation only. Guard.Context must stamp the live lease
// at the dispatcher boundary; the store also rechecks the current reader binding.
func (s *ReceiptService) Get(ctx context.Context, requestID string) (FamilyReceipt, error) {
	result := FamilyReceipt{RequestID: requestID, Disposition: "missing"}
	r, err := s.db.ReadFamilyRequest(ctx, s.binding, s.readers, requestID)
	if errors.Is(err, store.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Tool = r.Tool
	result.Disposition = r.Disposition
	result.Reference = r.Reference
	{
		projection := s.projections[r.Reference.Kind]
		if projection == nil && r.Reference.Kind == "" {
			// A domain may recover a crash before ledger linking through the same
			// underlying stable request identity, without redispatching it.
			projection = s.projections["tool:"+r.Tool]
		}
		if projection != nil {
			result.States, result.Publications, err = projection(ctx, r)
			if err != nil {
				return FamilyReceipt{}, err
			}
		}
	}
	if len(result.Publications) > 5 || len(result.States.Input) > 128 || len(result.States.Engineering) > 128 || len(result.States.Acceptance) > 128 || len(result.States.Effects) > 128 {
		return FamilyReceipt{}, errors.New("receipt projection exceeds bounds")
	}
	for _, p := range result.Publications {
		if p.IssueID < 0 || p.IssueNumber < 0 || p.CommentID < 0 || len(p.Relationship) > 128 {
			return FamilyReceipt{}, errors.New("invalid publication receipt projection")
		}
	}
	// Projection completion is still subject to current authority after I/O.
	if err := s.db.CheckResultWriter(ctx, s.binding); err != nil {
		return FamilyReceipt{}, err
	}
	return result, nil
}

// HandleGet is a strict callable registration seam for the family dispatcher.
func (s *ReceiptService) HandleGet(ctx context.Context, requestID string, arguments []byte) (json.RawMessage, error) {
	var q struct {
		RequestID string `json:"request_id"`
	}
	if err := decodeMCPArguments(arguments, &q); err != nil {
		return nil, err
	}
	if q.RequestID == "" || len(q.RequestID) > 128 || q.RequestID != requestID {
		return nil, fmt.Errorf("invalid receipt correlation")
	}
	result, err := s.Get(ctx, q.RequestID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// SubmissionReceiptProjection observes the shared checkpoint engine's retained
// receipt. A lost ledger completion can use the same stable underlying identity.
func SubmissionReceiptProjection(db *store.DB) FamilyProjection {
	return func(ctx context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		requestID := r.Reference.RequestID
		if requestID == "" {
			requestID = r.RequestID
		}
		receipt, err := db.GetSubmissionReceipt(ctx, r.Scope.RunID, requestID)
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		if receipt.Attempt != r.Scope.Attempt || (r.Reference.ID != 0 && receipt.ID != r.Reference.ID) {
			return ReceiptDimensions{}, nil, store.ErrResultAuthority
		}
		return ReceiptDimensions{Input: receipt.InputState, Engineering: receipt.EngineeringState, Acceptance: receipt.AcceptanceState, Effects: receipt.EffectState}, nil, nil
	}
}

// ResultReceiptProjection references existing application receipt dimensions.
// The current result binding remains required independently of ledger access.
func ResultReceiptProjection(db *store.DB, binding store.ResultBinding) FamilyProjection {
	return func(ctx context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		requestID := r.Reference.RequestID
		if requestID == "" {
			requestID = r.RequestID
		}
		receipt, err := db.GetResultReceipt(ctx, binding, r.Scope.RunID, requestID)
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		if receipt.Attempt != r.Scope.Attempt || (r.Reference.ID != 0 && receipt.ID != r.Reference.ID) {
			return ReceiptDimensions{}, nil, store.ErrResultAuthority
		}
		return ReceiptDimensions{Input: receipt.InputState, Engineering: receipt.Latest.Engineering, Acceptance: receipt.Latest.Acceptance, Effects: receipt.Latest.Publication}, nil, nil
	}
}

// CreationReceiptProjection reads existing operation truth. The ledger reference
// was installed by the authorized family handler; no permit is returned.
func CreationReceiptProjection(creation *store.CreationStore) FamilyProjection {
	return func(ctx context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		if r.Reference.ID < 1 {
			return ReceiptDimensions{}, nil, store.ErrNotFound
		}
		op, err := creation.GetCreationOperation(ctx, r.Reference.ID)
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		return ReceiptDimensions{Effects: op.State}, nil, nil
	}
}
