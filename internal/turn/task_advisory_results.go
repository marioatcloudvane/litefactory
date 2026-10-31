package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

type taskAdvisoryResults struct {
	db      *store.DB
	binding store.AdvisoryResultBinding
}

func bindTaskAdvisoryResults(ctx context.Context, db *store.DB, execution string) (*taskAdvisoryResults, error) {
	b, err := db.BindAdvisoryResultAttempt(ctx, execution)
	if err != nil {
		return nil, err
	}
	return &taskAdvisoryResults{db: db, binding: b}, nil
}

func decodeTaskAdvisoryAnswer(raw []byte) (taskdiscussion.Answer, error) {
	var answer taskdiscussion.Answer
	if len(raw) > 128*1024 || !utf8.Valid(raw) {
		return answer, errors.New("invalid advisory result bounds")
	}
	if err := submission.DecodeJSON(raw, &answer); err != nil {
		return answer, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return answer, err
	}
	for name := range fields {
		if name != "answer" && name != "sources" && name != "gaps" && name != "summary_suggestion" {
			return answer, errors.New("unknown advisory result field")
		}
	}
	if value, present := fields["summary_suggestion"]; present {
		var summary map[string]json.RawMessage
		if err := json.Unmarshal(value, &summary); err != nil {
			return answer, err
		}
		for name := range summary {
			switch name {
			case "question", "conclusion", "alternatives_or_uncertainty", "evidence_basis", "attribution":
			default:
				return answer, errors.New("unknown advisory summary field")
			}
		}
	}
	for _, name := range []string{"answer", "sources", "gaps"} {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return answer, errors.New("required advisory result field missing or null")
		}
	}
	if value, present := fields["summary_suggestion"]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return answer, errors.New("advisory summary suggestion must be an object when supplied")
	}
	if strings.TrimSpace(answer.Answer) == "" || len(answer.Sources) == 0 || len(answer.Sources) > 256 || len(answer.Gaps) > 256 {
		return answer, errors.New("advisory answer and sources are required within bounds")
	}
	for _, list := range [][]string{answer.Sources, answer.Gaps} {
		for _, value := range list {
			if strings.TrimSpace(value) == "" || len(value) > 8192 {
				return answer, errors.New("invalid advisory source or gap")
			}
		}
	}
	if s := answer.SummarySuggestion; s != nil {
		for _, value := range []string{s.Question, s.Conclusion, s.AlternativesOrUncertainty, s.EvidenceBasis, s.Attribution} {
			if strings.TrimSpace(value) == "" || len(value) > 8192 {
				return answer, errors.New("incomplete advisory summary suggestion")
			}
		}
	}
	return answer, nil
}
func (s *taskAdvisoryResults) accepted(ctx context.Context) (taskdiscussion.Answer, error) {
	_, raw, err := s.db.ReadAdvisoryResult(ctx, s.binding, "")
	if err != nil {
		return taskdiscussion.Answer{}, err
	}
	return decodeTaskAdvisoryAnswer(raw)
}
func (s *taskAdvisoryResults) preflight(ctx context.Context, raw []byte) (agentresult.Diagnostics, error) {
	if err := s.db.CheckAdvisoryResultWriter(ctx, s.binding); err != nil {
		return agentresult.Diagnostics{}, err
	}
	_, err := decodeTaskAdvisoryAnswer(raw)
	d := agentresult.Diagnostics{Valid: err == nil, Complete: err == nil}
	if err != nil {
		d.Diagnostics = []agentresult.Diagnostic{{Path: "$", Code: "advisory", Message: err.Error()}}
	}
	return d, nil
}
func (s *taskAdvisoryResults) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	method, data := q.Method, q.Payload
	if method == "tools/list" {
		schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"},"sources":{"type":"array","items":{"type":"string"}},"gaps":{"type":"array","items":{"type":"string"}},"summary_suggestion":{"type":"object","properties":{"question":{"type":"string"},"conclusion":{"type":"string"},"alternatives_or_uncertainty":{"type":"string"},"evidence_basis":{"type":"string"},"attribution":{"type":"string"}},"required":["question","conclusion","alternatives_or_uncertainty","evidence_basis","attribution"],"additionalProperties":false}},"required":["answer","sources","gaps"],"additionalProperties":false}`)
		receipt := json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)
		return json.Marshal(map[string]any{"tools": []submission.ToolSpec{{Name: "communication_preflight", Description: "Validate this private advisory answer", InputSchema: schema}, {Name: "communication_submit", Description: "Retain this execution's private advisory answer", InputSchema: schema}, {Name: "communication_receipt", Description: "Recover this execution's advisory receipt", InputSchema: receipt}}})
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
		d, err := s.preflight(ctx, data)
		if err != nil {
			return nil, err
		}
		return json.Marshal(d)
	case "communication_submit", "submit":
		if _, err := decodeTaskAdvisoryAnswer(data); err != nil {
			return nil, err
		}
		replay, err := s.db.SubmitAdvisoryResult(ctx, s.binding, q.RequestID, data)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"request_id": q.RequestID, "accepted": true, "replay": replay})
	case "communication_receipt", "receipt":
		var input struct {
			RequestID string `json:"request_id"`
		}
		var fields map[string]json.RawMessage
		if err := submission.DecodeJSON(data, &input); err != nil || input.RequestID == "" {
			return nil, errors.New("invalid advisory receipt identity")
		}
		if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 1 || fields["request_id"] == nil {
			return nil, errors.New("invalid advisory receipt fields")
		}
		id, _, err := s.db.ReadAdvisoryResult(ctx, s.binding, input.RequestID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"request_id": id, "accepted": true})
	default:
		return nil, fmt.Errorf("unsupported advisory method")
	}
}
