package turn

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// ActivityOptions supplies immutable scope and an initial factory time. Only
// factory lifecycle input advances that time; tool arguments cannot supply it.
type ActivityOptions struct {
	Receipts *ReceiptService
	Now      time.Time
}

// ActivityService owns advisory, process-local display state, never workflow or
// authority. The dispatcher owns durable first invocation election and recovery.
type ActivityService struct {
	receipts *ReceiptService
	mu       sync.Mutex
	state    agentresult.ProgressState
	now      time.Time
	terminal bool
	cleared  bool
}

// NewActivityService binds advisory display state to receipts and factory time.
func NewActivityService(o ActivityOptions) (*ActivityService, error) {
	if o.Receipts == nil || o.Now.IsZero() {
		return nil, errors.New("activity requires bound receipts and factory time")
	}
	return &ActivityService{receipts: o.Receipts, now: o.Now}, nil
}

// Binding returns the immutable attempt scope used by the family dispatcher.
func (s *ActivityService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PermissionMaterial binds the advisory contract version and attempt scope into the catalog digest.
func (s *ActivityService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(struct {
		Binding store.ResultBinding
		Version string
	}{s.Binding(), "advisory-progress-v1"})
}

// ActivityView exposes only a copied latest message and bounded counters. A
// retained update receipt does not promise that this display survives restart.
type ActivityView struct {
	Status             string                        `json:"status"`
	HighestSequence    int64                         `json:"highest_sequence"`
	AcceptedUpdates    int                           `json:"accepted_updates"`
	RetainedIdentities int                           `json:"retained_identities"`
	Latest             *agentresult.AdvisoryProgress `json:"latest,omitempty"`
	TerminalAt         *time.Time                    `json:"terminal_at,omitempty"`
}

// ActivityUpdateResponse separates update disposition from the current advisory display.
type ActivityUpdateResponse struct {
	Disposition string       `json:"disposition"`
	Code        string       `json:"code,omitempty"`
	Activity    ActivityView `json:"activity"`
}

// ActivityLifecycle is trusted factory input, not a tool request. Terminal and
// Clear are irreversible for this service instance; cleanup cannot reset the
// update budget and reopen the same attempt for more authored updates.
type ActivityLifecycle struct {
	Now      time.Time
	Terminal bool
	Clear    bool
}

// Lifecycle applies factory time and terminal cleanup without renewing authority.
func (s *ActivityService) Lifecycle(input ActivityLifecycle) (ActivityView, error) {
	if input.Now.IsZero() {
		return ActivityView{}, errors.New("activity lifecycle requires factory time")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.Now.After(s.now) {
		s.now = input.Now
	}
	if input.Terminal || input.Clear {
		s.terminal = true
		s.state = agentresult.MarkProgressTerminal(s.state, input.Now)
	}
	before := s.state.View()
	s.state = agentresult.CleanTerminalProgress(s.state, s.now, input.Clear)
	if input.Clear || before.Latest != nil && s.state.View().Latest == nil {
		s.cleared = true
	}
	return s.viewLocked(), nil
}

func (s *ActivityService) viewLocked() ActivityView {
	v := s.state.View()
	status := "unavailable"
	if v.Latest != nil {
		status = "available"
	} else if s.cleared {
		status = "cleared"
	}
	return ActivityView{Status: status, HighestSequence: v.HighestSequence, AcceptedUpdates: v.AcceptedUpdates, RetainedIdentities: v.RetainedIdentities, Latest: v.Latest, TerminalAt: v.TerminalAt}
}

// HandleUpdate runs only after the bound dispatcher elects a first invocation.
// Replays recover ledger disposition without reapplying an update or cleanup.
func (s *ActivityService) HandleUpdate(ctx context.Context, requestID string, arguments []byte) (json.RawMessage, error) {
	var update agentresult.AdvisoryProgress
	if err := decodeMCPArguments(arguments, &update); err != nil {
		return nil, errors.Join(err, s.receipts.Finish(ctx, requestID, "rejected", store.FamilyReference{}))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.receipts.db.CheckResultWriter(ctx, s.Binding()); err != nil {
		return nil, err
	}
	b := s.Binding()
	next, disposition := agentresult.ReduceProgress(s.state, agentresult.ProgressBinding{RunID: b.RunID, Attempt: b.Attempt, Active: !s.terminal}, s.now, update)
	s.state = next
	retained := "confirmed"
	if disposition.Kind == "rejected" {
		retained = "rejected"
	}
	if err := s.receipts.Finish(ctx, requestID, retained, store.FamilyReference{}); err != nil {
		return nil, err
	}
	return json.Marshal(ActivityUpdateResponse{Disposition: disposition.Kind, Code: disposition.Code, Activity: s.viewLocked()})
}

// HandleGet returns the current bounded display under the live attempt binding.
func (s *ActivityService) HandleGet(ctx context.Context, _ string, arguments []byte) (json.RawMessage, error) {
	var q struct{}
	if err := decodeMCPArguments(arguments, &q); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.receipts.db.CheckResultWriter(ctx, s.Binding()); err != nil {
		return nil, err
	}
	return json.Marshal(s.viewLocked())
}
