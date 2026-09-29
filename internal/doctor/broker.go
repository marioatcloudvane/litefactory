package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/submission"
)

// StartResultsBroker reuses the private authenticated submission transport with
// exactly three doctor communication tools. Its bounded workers are cancelled
// and joined by the invocation owner through Broker.Close.
func StartResultsBroker(ctx context.Context, directory, attempt string, results *ResultService) (*submission.Broker, error) {
	if results == nil || results.binding.DoctorAttemptID != attempt {
		return nil, errors.New("doctor broker attempt mismatch")
	}
	return submission.Start(ctx, submission.Options{Directory: directory, AllowResult: true, AllowTools: true, AttemptReference: attempt, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: results.handle})
}
func (s *ResultService) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	method, payload := q.Method, q.Payload
	if method == "tools/list" {
		if err := s.db.CheckResultWriter(ctx, s.binding); err != nil {
			return nil, err
		}
		input := json.RawMessage(`{"type":"object","properties":{"expected_revision":{"type":"integer"},"diagnosis":{"type":"object","properties":{"findings":{"type":"array"},"coverage_gaps":{"type":"array"},"questions":{"type":"array"},"proposal":{"type":"array"}},"required":["findings","coverage_gaps","questions","proposal"]}},"required":["expected_revision","diagnosis"],"additionalProperties":false}`)
		if s.repairID != "" {
			input = json.RawMessage(`{"type":"object","properties":{"expected_revision":{"type":"integer"},"diagnosis":{"type":"object","properties":{"findings":{"type":"array"},"coverage_gaps":{"type":"array"},"questions":{"type":"array"},"proposal":{"type":"array","maxItems":0},"repair":{"type":"object","properties":{"operation_id":{"type":"string"},"completed_steps":{"type":"array","items":{"type":"integer","minimum":1,"maximum":30}},"observations":{"type":"array","items":{"type":"string"}},"unresolved":{"type":"array","items":{"type":"string"}}},"required":["operation_id","completed_steps","observations","unresolved"],"additionalProperties":false}},"required":["findings","coverage_gaps","questions","proposal","repair"],"additionalProperties":false}},"required":["expected_revision","diagnosis"],"additionalProperties":false}`)
		}

		return json.Marshal(struct {
			Tools []submission.ToolSpec `json:"tools"`
		}{[]submission.ToolSpec{
			{Name: "communication_preflight", Description: "Validate a doctor diagnosis without recording it", InputSchema: input},
			{Name: "communication_submit", Description: "Retain the validated diagnosis; preserve request identity on reply loss", InputSchema: input},
			{Name: "communication_receipt", Description: "Recover the exact durable receipt by request ID", InputSchema: json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)},
		}})
	}
	if method == "tools/call" {
		call, err := submission.DecodeToolCall(payload)
		if err != nil {
			return nil, err
		}
		payload = call.Arguments
		switch call.Name {
		case "communication_preflight":
			method = "preflight"
		case "communication_submit":
			method = "submit"
		case "communication_receipt":
			method = "receipt"
		default:
			return nil, errors.New("doctor tool is not permitted")
		}
	}
	switch method {
	case "preflight":
		result, err := s.Preflight(ctx, payload)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "submit":
		result, err := s.Submit(ctx, q.RequestID, payload)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "receipt":
		var request struct {
			RequestID string `json:"request_id"`
		}
		if err := submission.DecodeJSON(payload, &request); err != nil {
			return nil, err
		}
		if request.RequestID == "" || request.RequestID != q.RequestID {
			return nil, errors.New("doctor receipt request mismatch")
		}
		result, err := s.Receipt(ctx, request.RequestID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	default:
		return nil, errors.New("doctor method is not permitted")
	}
}
