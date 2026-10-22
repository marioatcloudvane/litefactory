package mcpadapter

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestEncodeToolResultMatchesDelivery(t *testing.T) {
	for _, r := range []submission.Response{
		{RequestID: "read", Result: json.RawMessage(`{"text":"quoted \"value\" and <html>"}`)},
		{RequestID: "read", Error: "unavailable"},
		{RequestID: "read", Result: json.RawMessage(`{"valid":false}`)},
	} {
		encoded, err := EncodeToolResult(r)
		if err != nil {
			t.Fatal(err)
		}
		actual := toolResponse(json.RawMessage(`1`), r)
		if !bytes.Equal(actual.Result, encoded) {
			t.Fatalf("sizing and delivery differ: %s / %s", encoded, actual.Result)
		}
		var result struct {
			Content    []textContent       `json:"content"`
			Structured submission.Response `json:"structuredContent"`
			IsError    bool                `json:"isError"`
		}
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Content) != 1 || result.Structured.RequestID != r.RequestID {
			t.Fatal("missing duplicate representation")
		}
		if (r.Error != "" || string(r.Result) == `{"valid":false}`) != result.IsError {
			t.Fatal("incorrect rejection flag")
		}
	}
}
