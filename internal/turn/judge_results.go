package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/resultservice"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// JudgeResultService gives one advisory attempt append-only shared result authority.
// It cannot read or update another application result or a workflow decision.
type JudgeResultService struct {
	db      *store.DB
	binding store.ResultBinding
}

func bindJudgeResults(ctx context.Context, db *store.DB, attempt string) (*JudgeResultService, error) {
	b, err := db.BindJudgeResultAttempt(ctx, attempt)
	if err != nil {
		return nil, err
	}
	return &JudgeResultService{db: db, binding: b}, nil
}

// Preflight checks active authority and advisory validity without recording a result.
func (s *JudgeResultService) Preflight(ctx context.Context, data []byte) (agentresult.Diagnostics, error) {
	if err := s.db.CheckResultWriter(ctx, s.binding); err != nil {
		return agentresult.Diagnostics{}, err
	}
	_, d := agentresult.DecodeJudge(data)
	if _, err := s.db.GetResultRevision(ctx, s.binding, 0); err == nil {
		d.Valid = false
		d.Diagnostics = append(d.Diagnostics, agentresult.Diagnostic{Path: "$", Code: "immutable", Message: "This advisory result has already been saved; recover its receipt."})
	} else if !errors.Is(err, store.ErrNotFound) {
		return d, err
	}
	return d, nil
}

// Submit retains one append-only report or replays the identical durable request.
func (s *JudgeResultService) Submit(ctx context.Context, requestID string, data []byte) (*store.ResultReceipt, error) {
	receipt, _, err := resultservice.Submit(ctx, s.db, s.binding, requestID, data, func(ctx context.Context) (*store.ResultRecord, error) {
		d, err := s.Preflight(ctx, data)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(d)
		if err != nil {
			return nil, err
		}
		record := &store.ResultRecord{Diagnostics: encoded, ExpectedRevision: 0, Accepted: d.Valid, Retired: []byte(`[]`)}
		if d.Valid {
			r, _ := agentresult.DecodeJudge(data)
			record.Snapshot, err = json.Marshal(r)
		}
		return record, err
	})
	return receipt, err
}

// Receipt recovers an attributed request after response loss, including after execution.
func (s *JudgeResultService) Receipt(ctx context.Context, id string) (store.ResultReceipt, error) {
	return s.db.GetResultReceipt(ctx, s.binding, 0, id)
}

func (s *JudgeResultService) accepted(ctx context.Context) (agentresult.JudgeReport, error) {
	revision, err := s.db.GetResultRevision(ctx, s.binding, 0)
	if err != nil {
		return agentresult.JudgeReport{}, err
	}
	var r agentresult.JudgeReport
	if err := json.Unmarshal(revision.Snapshot, &r); err != nil {
		return r, err
	}
	d := agentresult.ValidateJudge(r)
	if !d.Valid || !d.Complete {
		return r, errors.New("retained judge report no longer validates")
	}
	return r, nil
}

func (s *JudgeResultService) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	method, data := q.Method, q.Payload
	if method == "tools/list" {
		input := json.RawMessage(`{"type":"object","properties":{"report":{"type":"object","description":"Version 1 advisory report: verdict, summary, optional coverage (paths of the files read), uncertainty strings, findings with title, severity, concern, evidence citations ({path, start_line, end_line}: a path relative to the repository root or a context file name), impact and correction."}},"required":["report"],"additionalProperties":false}`)
		receipt := json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)
		return json.Marshal(map[string]any{"tools": []submission.ToolSpec{
			{Name: "communication_preflight", Description: "Validate the advisory report without saving", InputSchema: input},
			{Name: "communication_submit", Description: "Save the advisory report in the shared durable result ledger", InputSchema: input},
			{Name: "communication_receipt", Description: "Recover a durable result receipt by request_id", InputSchema: receipt},
		}})
	}
	if method == "tools/call" {
		call, err := submission.DecodeToolCall(data)
		if err != nil {
			return nil, err
		}
		method, data = call.Name, call.Arguments
	}
	switch method {
	case "communication_preflight", "preflight":
		d, err := s.Preflight(ctx, data)
		if err != nil {
			return nil, err
		}
		return json.Marshal(d)
	case "communication_submit", "submit":
		r, err := s.Submit(ctx, q.RequestID, data)
		if err != nil {
			return nil, err
		}
		return json.Marshal(r)
	case "communication_receipt", "receipt":
		var in struct {
			RequestID string `json:"request_id"`
		}
		if err := submission.DecodeJSON(data, &in); err != nil {
			return nil, err
		}
		r, err := s.Receipt(ctx, in.RequestID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(r)
	default:
		return nil, fmt.Errorf("unsupported judge method")
	}
}
