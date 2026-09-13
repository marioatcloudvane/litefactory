package harness

import (
	"bytes"
	"encoding/json"
	"strings"
)

// provenZeroCostRefusal requires a complete, solitary terminal refusal. Extra
// events, malformed data, interrupted pipes or contrary usage retain uncertainty.
// The broader fallback classifier intentionally remains unchanged.
func provenZeroCostRefusal(resuming bool, result *RunResult, stream *eventStream) bool {
	return resumeRefused(resuming, result) && result.HasResult && result.IsError &&
		result.HasCost && result.CostUSD == 0 && result.HasNumTurns && result.NumTurns == 0 &&
		result.StreamError == nil && stream.events == 1 && stream.malformed == 0 &&
		stream.result != nil && zeroUsageEvidence(stream.result.Usage) && zeroUsageEvidence(stream.result.ModelUsage)
}

// Unknown shapes fail closed; absent usage is not invented as measured zero.
// Nested numeric counters may affirm zero but any nonzero counter contradicts it.
func zeroUsageEvidence(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return true
	}
	if raw[0] == '{' {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return false
		}
		for key, value := range fields {
			if key == "service_tier" || key == "inference_geo" || key == "speed" {
				var metadata string
				if json.Unmarshal(value, &metadata) != nil {
					return false
				}
				continue
			}
			if !zeroUsageEvidence(value) {
				return false
			}
		}
		return true
	}
	if raw[0] == '[' {
		var values []json.RawMessage
		return json.Unmarshal(raw, &values) == nil && len(values) == 0
	}
	if raw[0] == '"' {
		return false
	}
	var value json.Number
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	// Check the significand exactly; floating-point underflow is not zero work.
	significand := strings.FieldsFunc(string(value), func(r rune) bool { return r == 'e' || r == 'E' })
	return len(significand) > 0 && strings.Trim(significand[0], "+-.0") == ""
}
