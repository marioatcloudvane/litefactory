package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/submission"
)

// StartResultsBroker reuses the private authenticated submission transport with
// exactly five diagnosis tools. Repair retains its separate catalog. Workers are cancelled
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
		input := diagnosisInputSchema(false)
		if s.repairID != "" {
			input = diagnosisInputSchema(true)
		}

		tools := []submission.ToolSpec{
			{Name: "communication_preflight", Description: "Validate a doctor diagnosis without recording it", InputSchema: input},
			{Name: "communication_submit", Description: "Retain the validated diagnosis; preserve request identity on reply loss", InputSchema: input},
			{Name: "communication_receipt", Description: "Recover the exact durable receipt by request ID", InputSchema: json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)},
		}
		if s.repairID == "" {
			tools = append(tools,
				submission.ToolSpec{Name: "database_query", Description: "Run exactly one SELECT on a separate read-only connection; bounded, redacted columns and rows with truncation", InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string","minLength":1,"maxLength":16384}},"required":["sql"],"additionalProperties":false}`)},
				submission.ToolSpec{Name: "issue_comments", Description: "Read one redacted page of this attempt's target issue comments; target cannot be changed", InputSchema: json.RawMessage(`{"type":"object","properties":{"page":{"type":"integer","minimum":1,"maximum":10000}},"additionalProperties":false}`)},
			)
		}
		return json.Marshal(struct {
			Tools []submission.ToolSpec `json:"tools"`
		}{tools})
	}
	if method == "tools/call" {
		call, err := submission.DecodeToolCall(payload)
		if err != nil {
			return nil, err
		}
		payload = call.Arguments
		switch call.Name {
		case "database_query", "issue_comments":
			if s.repairID != "" {
				return nil, errors.New("diagnosis read tools are unavailable to repair")
			}
			if err := s.db.CheckResultWriter(ctx, s.binding); err != nil {
				return nil, err
			}
			if call.Name == "database_query" {
				return s.readResponse(ctx, s.query, payload)
			}
			return s.readResponse(ctx, s.commentsPage, payload)
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

// Item shapes mirror Diagnosis and are checked again by ValidateDiagnosis. An
// untyped array made the agent guess, and every guess failed preflight.
const (
	findingSchema = `{"type":"object","description":"One conclusion.","properties":{"explanation":{"type":"string","minLength":1,"maxLength":8192,"description":"Plain-language conclusion"},"evidence":{"type":"array","maxItems":30,"items":{"type":"string"},"description":"Cite a live source: file path and lines, database query/table/row, git commit or comment ID. Legacy attempts require their supplied kind:id handles. May be empty only when coverage_gaps is not."},"uncertainty":{"type":"string","minLength":1,"maxLength":8192,"description":"What is not known or not observed"}},"required":["explanation","evidence","uncertainty"],"additionalProperties":false}`
	gapsSchema    = `{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":8192},"description":"Include every supplied coverage gap verbatim, and report read failures, unavailable sources and truncation."}`
	textsSchema   = `{"type":"array","maxItems":30,"items":{"type":"string","minLength":1,"maxLength":8192}}`
	stepSchema    = `{"type":"object","description":"One inert proposed step; never executed by diagnosis.","properties":{"number":{"type":"integer","minimum":1,"description":"1-based position; steps are numbered 1, 2, 3 in order"},"target":{"type":"string","description":"Evidence handle of the observed issue-owned record to change"},"precondition":{"type":"string"},"change":{"type":"string"},"reason":{"type":"string"},"reversibility":{"type":"string"},"rollback":{"type":"string"},"action":{"type":"object","properties":{"kind":{"type":"string"},"before":{"type":"string","description":"Observed current value"},"after":{"type":"string"}},"required":["kind","before","after"],"additionalProperties":false}},"required":["number","target","precondition","change","reason","reversibility","rollback"],"additionalProperties":false}`
)

func diagnosisInputSchema(repair bool) json.RawMessage {
	proposal := `{"type":"array","maxItems":30,"items":` + stepSchema + `}`
	required := `"findings","coverage_gaps","questions","proposal"`
	extra := ""
	if repair {
		proposal = `{"type":"array","maxItems":0}`
		required += `,"repair"`
		extra = `,"repair":{"type":"object","properties":{"operation_id":{"type":"string"},"completed_steps":{"type":"array","items":{"type":"integer","minimum":1,"maximum":30}},"observations":{"type":"array","items":{"type":"string"}},"unresolved":{"type":"array","items":{"type":"string"}}},"required":["operation_id","completed_steps","observations","unresolved"],"additionalProperties":false}`
	}
	return json.RawMessage(`{"type":"object","properties":{"expected_revision":{"type":"integer","minimum":0},"diagnosis":{"type":"object","properties":{"withdraw_proposal":{"type":"boolean","description":"Explicitly withdraw the current proposal; cannot accompany a replacement."},"findings":{"type":"array","minItems":1,"maxItems":30,"items":` + findingSchema + `},"coverage_gaps":` + gapsSchema + `,"questions":` + textsSchema + `,"proposal":` + proposal + extra + `},"required":[` + required + `],"additionalProperties":false}},"required":["expected_revision","diagnosis"],"additionalProperties":false}`)
}
