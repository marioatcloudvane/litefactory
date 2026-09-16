package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// FamilyOwner supplies immutable factory scope; handler implementations retain
// their own business validation and durable operation ownership.
type FamilyOwner interface {
	Binding() store.ResultBinding
	PermissionMaterial() ([]byte, error)
}

// FamilyHandler handles a guarded family request using its stable request identity.
type FamilyHandler func(context.Context, string, []byte) (json.RawMessage, error)

// FamilyRegistration installs a known catalog handler. ReplayCheck must perform
// fresh creation authorization without dispatch; other families recover receipts
// without invoking their handler again. Owners finish authoritative references.
type FamilyRegistration struct {
	Name        string
	Owner       FamilyOwner
	Handle      FamilyHandler
	ReplayCheck func(context.Context, string, []byte) error
}

// FamilyDispatcherOptions supplies factory-owned dependencies and immutable catalog scope.
type FamilyDispatcherOptions struct {
	Service        MCPResultService
	Receipts       *ReceiptService
	Feedback       *FeedbackService
	Evidence       *EvidenceService
	AllowCreation  bool
	CommentTargets []int
	Registrations  []FamilyRegistration
}

// FamilyPermissionDigest is called before binding the attempt lease, with the
// same options later passed to NewFamilyToolDispatcher. It includes all installed
// handlers, their immutable scopes, target grants, receipt history and gate.
func FamilyPermissionDigest(o FamilyDispatcherOptions) (string, error) {
	_, _, digest, err := prepareFamilies(o)
	return digest, err
}

func prepareFamilies(o FamilyDispatcherOptions) (submission.ToolProfile, map[string]FamilyRegistration, string, error) {
	bad := func() (submission.ToolProfile, map[string]FamilyRegistration, string, error) {
		return submission.ToolProfile{}, nil, "", errors.New("family dispatcher requires complete matching immutable dependencies")
	}
	if o.Service == nil || o.Receipts == nil || o.Feedback == nil || o.Service.MCPPermissionDigest() == "" {
		return bad()
	}
	b := o.Service.Binding()
	if o.Receipts.Binding() != b || o.Feedback.Binding() != b || o.Feedback.application.MCPPermissionDigest() != o.Service.MCPPermissionDigest() || o.Feedback.evidence != o.Evidence {
		return bad()
	}
	registrations := []FamilyRegistration{{Name: "feedback_preflight", Owner: o.Feedback, Handle: o.Feedback.HandlePreflight}, {Name: "receipts_get", Owner: o.Receipts, Handle: o.Receipts.HandleGet}}
	if b.Mode == "implement" || b.Mode == "validate" {
		if o.Evidence == nil || o.Evidence.receipts != o.Receipts || o.Receipts.projections["submission"] == nil || o.Receipts.projections["tool:evidence_submit"] == nil {
			return bad()
		}
		registrations = append(registrations, FamilyRegistration{Name: "evidence_submit", Owner: o.Evidence, Handle: o.Evidence.HandleSubmit})
	} else if o.Evidence != nil {
		return bad()
	}
	registrations = append(registrations, o.Registrations...)
	handlers := make(map[string]FamilyRegistration, len(registrations))
	materials := make(map[string]json.RawMessage, len(registrations))
	names := make([]string, 0, len(registrations))
	for _, r := range registrations {
		if r.Owner == nil || r.Handle == nil || r.Owner.Binding() != b {
			return bad()
		}
		if _, found := handlers[r.Name]; found {
			return bad()
		}
		if r.Name == "issues_create_batch" && r.ReplayCheck == nil {
			return bad()
		}
		material, err := r.Owner.PermissionMaterial()
		if err != nil {
			return submission.ToolProfile{}, nil, "", err
		}
		if !json.Valid(material) {
			return bad()
		}
		materials[r.Name] = append(json.RawMessage(nil), material...)
		handlers[r.Name] = r
		names = append(names, r.Name)
	}
	targets := slices.Clone(o.CommentTargets)
	slices.Sort(targets)
	for i, target := range targets {
		if target < 1 || i > 0 && targets[i-1] == target {
			return bad()
		}
	}
	if len(targets) > 100 {
		return bad()
	}
	profile, err := submission.NewFamilyToolProfile(b.Mode, o.AllowCreation, names)
	if err != nil {
		return submission.ToolProfile{}, nil, "", err
	}
	data, err := json.Marshal(struct {
		Catalog        string
		Binding        store.ResultBinding
		Application    string
		Creation       bool
		CommentTargets []int
		Materials      map[string]json.RawMessage
	}{submission.FamilyCatalogVersion, b, o.Service.MCPPermissionDigest(), o.AllowCreation, targets, materials})
	if err != nil {
		return submission.ToolProfile{}, nil, "", err
	}
	hash := sha256.Sum256(data)
	return profile, handlers, hex.EncodeToString(hash[:]), nil
}

// NewFamilyToolDispatcher requires a live guard bound to the complete family permission digest.
func NewFamilyToolDispatcher(guard *AttemptGuard, o FamilyDispatcherOptions) (*ToolDispatcher, error) {
	profile, handlers, digest, err := prepareFamilies(o)
	if err != nil {
		return nil, err
	}
	if guard == nil || guard.Binding() != o.Service.Binding() || guard.Lease().PermissionDigest != digest {
		return nil, errors.New("family dispatcher requires matching immutable lease")
	}
	return &ToolDispatcher{guard: guard, service: o.Service, profile: profile, receipts: o.Receipts, families: handlers}, nil
}

// dispatchFamily admits bounded metadata before profile and argument validation.
// Every replay observes the durable record rather than repeating an effect,
// except the advisory activity read below, which always observes live state.
func (d *ToolDispatcher) dispatchFamily(ctx context.Context, requestID string, call submission.ToolCall, decodeErr error) (json.RawMessage, error) {
	if call.Name == "receipts_get" {
		if decodeErr != nil {
			return nil, decodeErr
		}
		if !d.profile.Allows(call.Name) {
			return nil, errors.New("MCP tool not permitted")
		}
		return d.receipts.HandleGet(ctx, requestID, call.Arguments)
	}
	_, first, err := d.receipts.Begin(ctx, requestID, call.Name, call.Arguments)
	if err != nil {
		return nil, err
	}
	if decodeErr != nil || !d.profile.Allows(call.Name) {
		if first {
			if err := d.receipts.Finish(ctx, requestID, "rejected", store.FamilyReference{}); err != nil {
				return nil, err
			}
		}
		if decodeErr != nil {
			return nil, decodeErr
		}
		return nil, errors.New("MCP tool not permitted")
	}
	handler := d.families[call.Name]
	// The advisory activity read observes the current display on every call,
	// including an identical retry, rather than replaying a stale receipt. It
	// still records a durable receipted outcome via the terminal Finish below.
	if !first && call.Name != "activity_get" {
		if handler.ReplayCheck != nil {
			if err := handler.ReplayCheck(ctx, requestID, call.Arguments); err != nil {
				return nil, err
			}
		}
		result, err := d.receipts.Get(ctx, requestID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
	result, callErr := handler.Handle(ctx, requestID, call.Arguments)
	receipt, err := d.receipts.Get(ctx, requestID)
	if err != nil {
		return nil, errors.Join(callErr, err)
	}
	if receipt.Disposition == "pending" {
		disposition := "confirmed"
		if callErr != nil {
			disposition = "rejected"
			// Owners distinguish validation refusal from an uncertain dispatched write
			// by finishing the ledger themselves. Never guess that an effect was absent.
			if call.Name == "issues_comment" || call.Name == "issues_create_batch" || call.Name == "evidence_submit" || call.Name == "activity_update" {
				disposition = "uncertain"
			}
		}
		if err := d.receipts.Finish(ctx, requestID, disposition, store.FamilyReference{}); err != nil {
			return nil, errors.Join(callErr, err)
		}
	}
	return result, callErr
}

func communicationTool(name string) bool {
	return name == "communication_preflight" || name == "communication_submit" || name == "communication_receipt"
}

// malformedFamilyCall extracts correlation only; this never authorizes a handler.
func malformedFamilyCall(payload []byte) submission.ToolCall {
	var call submission.ToolCall
	if err := json.Unmarshal(payload, &call); err != nil {
		return submission.ToolCall{}
	}
	if strings.TrimSpace(call.Name) == "" || len(call.Name) > 128 {
		return submission.ToolCall{}
	}
	return call
}
