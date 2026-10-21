package prevention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

const resultSchema = `{"type":"object","properties":{"result":{"type":"object","properties":{"schema_version":{"type":"integer","const":1},"findings":{"type":"array","minItems":1,"maxItems":5,"items":{"type":"object","properties":{"issue":{"type":"integer","minimum":1},"expected":{"type":"string","minLength":1,"maxLength":4096},"actual":{"type":"string","minLength":1,"maxLength":4096},"cause":{"type":"string","maxLength":4096},"cause_basis":{"type":"string","enum":["recorded","inference",""]},"uncertainty":{"type":"string","minLength":1,"maxLength":4096},"prevention_rationale":{"type":"string","maxLength":4096},"bug_evidence":{"type":"array","maxItems":100,"items":{"type":"string"}},"cause_evidence":{"type":"array","maxItems":100,"items":{"type":"string"}},"missing_evidence":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":4096}},"outcome":{"type":"string","enum":["proposed","no_warranted_change","insufficient_evidence"]},"proposals":{"type":"array","maxItems":5,"items":{"type":"object","properties":{"kind":{"type":"string","enum":["standard.add","standard.replace"]},"standard":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},"category_ids":{"type":"array","items":{"type":"string"}}},"required":["id","title","body","category_ids"],"additionalProperties":false}},"required":["kind","standard"],"additionalProperties":false}}},"required":["issue","expected","actual","cause","cause_basis","uncertainty","prevention_rationale","bug_evidence","cause_evidence","missing_evidence","outcome","proposals"],"additionalProperties":false}}},"required":["schema_version","findings"],"additionalProperties":false}},"required":["result"],"additionalProperties":false}`

func toolSpecs() []submission.ToolSpec {
	names := agent.PreventionToolNames()
	schemas := map[string]string{
		"communication_preflight": resultSchema, "communication_submit": resultSchema,
		"communication_receipt": `{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`,
		"evidence_read":         `{"type":"object","properties":{"id":{"type":"string"},"offset":{"type":"integer","minimum":0}},"required":["id","offset"],"additionalProperties":false}`,
		"standards_categories":  `{"type":"object","properties":{"size":{"type":"integer","minimum":1,"maximum":5},"cursor":{"type":"string"}},"required":["size","cursor"],"additionalProperties":false}`,
		"standards_read":        `{"type":"object","properties":{"version":{"type":"integer","minimum":1},"digest":{"type":"string"},"cursor":{"type":"string"},"ack":{"type":"boolean"}},"required":["version","digest","cursor","ack"],"additionalProperties":false}`,
		"standards_select":      `{"type":"object","properties":{"expected":{"type":"integer","minimum":0},"ids":{"type":"array","maxItems":100,"items":{"type":"string"}},"reason":{"type":"string","maxLength":2048}},"required":["expected","ids","reason"],"additionalProperties":false}`,
		"standards_status":      `{"type":"object","properties":{},"additionalProperties":false}`,
	}
	out := []submission.ToolSpec{}
	for _, n := range names {
		out = append(out, submission.ToolSpec{Name: n, Description: "Bounded standalone prevention analysis: " + n, InputSchema: json.RawMessage(schemas[n])})
	}
	return out
}
func (s *Session) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	out, err := s.dispatch(ctx, q)
	if err == nil {
		return out, nil
	}
	// Every refusal, including decoding and payload limits, shares the same
	// charged terminal reserve and never repeats caller-controlled text.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	safe := map[string]string{"error": "Analysis access, validation or output limit refused; retained coverage and limits are inspectable through the CLI"}
	size := encodedSize(safe)
	if s.used+size > (128<<10)-(2<<10) {
		s.degraded = true
	}
	if s.used+size > 128<<10 {
		s.closed = true
		_ = s.service.DB.SetPreventionOutput(ctx, s.analysis.ID, s.analysis.AttemptID, s.used, true)
		if s.revoke != nil {
			s.revoke()
		}
		return nil, context.Canceled
	}
	if e := s.service.DB.SetPreventionOutput(ctx, s.analysis.ID, s.analysis.AttemptID, s.used+size, s.degraded); e != nil {
		return nil, context.Canceled
	}
	s.used += size
	return marshal(safe), nil
}
func (s *Session) dispatch(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	if len(q.Payload) > 64<<10 {
		return nil, errors.New("analysis request exceeds 64 KiB")
	}
	method, payload := q.Method, q.Payload
	if method == "tools/list" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.check(ctx); err != nil {
			return nil, err
		}
		// Fixed host tool metadata contains no evidence or model diagnostics.
		return json.Marshal(struct {
			Tools []submission.ToolSpec `json:"tools"`
		}{toolSpecs()})
	}
	if method == "tools/call" {
		call, err := submission.DecodeToolCall(payload)
		if err != nil {
			return nil, err
		}
		method = call.Name
		payload = call.Arguments
	}
	return s.call(ctx, method, q.RequestID, payload)
}

func (s *Session) call(ctx context.Context, method, requestID string, payload json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "evidence_read":
		var q struct {
			ID     string `json:"id"`
			Offset int    `json:"offset"`
		}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		v, e := s.Evidence(ctx, q.ID, q.Offset)
		return marshal(v), e
	case "standards_categories":
		var q struct {
			Size   int    `json:"size"`
			Cursor string `json:"cursor"`
		}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		v, e := s.Categories(ctx, q.Size, q.Cursor)
		return marshal(v), e
	case "standards_select":
		var q struct {
			Expected int64    `json:"expected"`
			IDs      []string `json:"ids"`
			Reason   string   `json:"reason"`
		}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		v, e := s.Select(ctx, q.Expected, q.IDs, q.Reason)
		return marshal(v), e
	case "standards_read":
		var q struct {
			Version int64  `json:"version"`
			Digest  string `json:"digest"`
			Cursor  string `json:"cursor"`
			Ack     bool   `json:"ack"`
		}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		v, e := s.ReadStandards(ctx, q.Version, q.Digest, q.Cursor, q.Ack)
		return marshal(v), e
	case "standards_status":
		var q struct{}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		v, e := s.Status(ctx)
		return marshal(v), e
	case "communication_preflight", "preflight", "communication_submit", "submit":
		var q struct {
			Result Result `json:"result"`
		}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		if method == "communication_preflight" || method == "preflight" {
			v, e := s.Preflight(ctx, q.Result)
			return marshal(struct {
				Diagnostics []string `json:"diagnostics"`
			}{v}), e
		}
		v, e := s.Submit(ctx, requestID, q.Result)
		return marshal(v), e
	case "communication_receipt", "receipt":
		var q struct {
			RequestID string `json:"request_id"`
		}
		if err := submission.DecodeJSON(payload, &q); err != nil {
			return nil, err
		}
		v, e := s.Receipt(ctx, q.RequestID)
		return marshal(v), e
	}
	return nil, fmt.Errorf("unknown prevention tool")
}
