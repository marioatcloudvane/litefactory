// Package resultservice owns mode-independent durable application submission.
// Workflow and doctor callers retain their own semantic validation and authority.
package resultservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/store"
)

// Ledger is the common receipt/revision store, never a second result ledger.
type Ledger interface {
	ReplayResultRequest(context.Context, store.ResultBinding, string, []byte) (store.ResultReceipt, error)
	RecordResult(context.Context, store.ResultRecord) (store.ResultReceipt, error)
}

// Submit recovers exact previous input before invoking validation or recording a
// revision. A nil prepared record returns diagnostics without a durable request.
// This keeps response-loss semantics identical across independent applications.
func Submit(ctx context.Context, ledger Ledger, binding store.ResultBinding, requestID string, payload []byte, prepare func(context.Context) (*store.ResultRecord, error)) (*store.ResultReceipt, bool, error) {
	if len(payload) > store.MaxResultInputBytes {
		return nil, false, fmt.Errorf("application result input exceeds contribution ceiling")
	}
	old, err := ledger.ReplayResultRequest(ctx, binding, requestID, payload)
	if err == nil {
		return &old, true, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, false, err
	}
	q, err := prepare(ctx)
	if err != nil || q == nil {
		return nil, false, err
	}
	// Transport correlation and trusted binding cannot be chosen by the semantic
	// candidate, including through a callback returning a reused value.
	q.Binding = binding
	q.RequestID = requestID
	q.Payload = append([]byte(nil), payload...)
	receipt, err := ledger.RecordResult(ctx, *q)
	if err != nil {
		return nil, false, err
	}
	return &receipt, false, nil
}
