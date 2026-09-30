package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

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
			list.Tools = append(list.Tools, []submission.ToolSpec{{Name: "doctor_backup", Description: "Create and retain the exact declared original backup", InputSchema: schema}, {Name: "doctor_write", Description: "Apply only the immutable numbered step after all backups; never replay an uncertain write", InputSchema: schema}, {Name: "doctor_verify", Description: "Reread one step and return its durable state; no repair or restart", InputSchema: schema}}...)
			return json.Marshal(list)
		}
		if q.Method == "tools/call" {
			call, err := submission.DecodeToolCall(q.Payload)
			if err != nil {
				return nil, err
			}
			switch call.Name {
			case "doctor_backup", "doctor_write", "doctor_verify":
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
					return nil, err
				}
				if input.Step < 1 || input.Step > 30 {
					return nil, errors.New("doctor repair step outside manifest")
				}
				switch call.Name {
				case "doctor_backup":
					err = tools.Backup(ctx, input.Step)
				case "doctor_write":
					step, readErr := tools.step(ctx, input.Step)
					switch {
					case readErr != nil:
						err = readErr
					case step.State == "verified":
						err = tools.Verify(ctx, input.Step)
					default:
						err = tools.Write(ctx, input.Step)
					}
				case "doctor_verify":
					err = tools.Verify(ctx, input.Step)
				}
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
	return submission.Start(ctx, submission.Options{Directory: directory, AllowResult: true, AllowTools: true, AttemptReference: attempt, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: handler})
}
