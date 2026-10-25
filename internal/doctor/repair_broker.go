package doctor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// StartRepairBroker binds a private transport to one immutable repair operation.
// No provider currently enables this transport without verified native isolation.
func StartRepairBroker(ctx context.Context, directory, attempt string, results *ResultService, tools *RepairTools) (*submission.Broker, error) {
	if results == nil || tools == nil || results.binding.DoctorAttemptID != attempt || results.repairID != tools.operation {
		return nil, errors.New("doctor repair broker binding mismatch")
	}
	handler := func(ctx context.Context, q submission.Request) (json.RawMessage, error) {
		if q.Method == "tools/list" {
			raw, err := results.handle(ctx, q)
			if err != nil {
				return nil, err
			}
			var list struct {
				Tools []submission.ToolSpec `json:"tools"`
			}
			if err = json.Unmarshal(raw, &list); err != nil {
				return nil, err
			}
			schema := json.RawMessage(`{"type":"object","properties":{"step":{"type":"integer","minimum":1,"maximum":30}},"required":["step"],"additionalProperties":false}`)
			for _, kind := range store.DoctorRepairToolKinds() {
				list.Tools = append(list.Tools, submission.ToolSpec{Name: kind, Description: "Apply only the matching approved numbered tool; graph tools atomically back up, apply, hand off guidance, verify and journal the entire approved graph manifest", InputSchema: schema})
			}
			return json.Marshal(list)
		}
		if q.Method == "tools/call" {
			call, err := submission.DecodeToolCall(q.Payload)
			if err != nil {
				return nil, err
			}
			if store.DoctorRepairToolKind(call.Name) {
				// Shared durable receipts are checked before any replay, including
				// after authority was fenced. Tool and final-result IDs share one ledger.
				if prior, replayErr := results.db.ReplayResultRequest(ctx, results.binding, q.RequestID, q.Payload); replayErr == nil {
					return prior.Diagnostics, nil
				} else if !errors.Is(replayErr, store.ErrNotFound) {
					return nil, replayErr
				}

				if err = results.db.CheckResultWriter(ctx, results.binding); err != nil {
					return nil, err
				}
				var input struct {
					Step int `json:"step"`
				}
				if err = submission.DecodeJSON(call.Arguments, &input); err != nil {
					_, refusal := tools.ApplyTyped(ctx, 0, call.Name)
					return nil, errors.Join(err, refusal)
				}
				_, err = tools.ApplyTyped(ctx, input.Step, call.Name)
				if err != nil {
					return nil, err
				}
				step, err := tools.step(ctx, input.Step)
				if err != nil {
					return nil, err
				}
				reply, marshalErr := json.Marshal(struct {
					Operation string `json:"operation_id"`
					Step      int    `json:"step"`
					State     string `json:"state"`
					Backup    string `json:"backup_reference"`
					Observed  string `json:"observed_after"`
				}{tools.operation, step.Number, step.State, step.BackupReference, step.ObservedAfter})
				if marshalErr != nil {
					return nil, marshalErr
				}
				receipt, recordErr := results.db.RecordResult(ctx, store.ResultRecord{Binding: results.binding, RequestID: q.RequestID, Payload: q.Payload, Diagnostics: reply, Accepted: true, DoctorToolReceipt: true})
				if recordErr != nil {
					return nil, recordErr
				}
				return receipt.Diagnostics, nil
			}
		}
		return results.handle(ctx, q)
	}
	return submission.Start(ctx, submission.Options{Directory: directory, AllowResult: true, AllowTools: true, AttemptReference: attempt, MaxWorkers: 2, RequestTimeout: submission.RepairToolTimeout, RepairTools: true, Handler: handler})
}
