package agentresult_test

import (
	"encoding/json"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/submission"
)

// These tests pin the pure shared-service permission rules used by MCP. Actual
// guarded dispatch and durable results require the separate real-store tests.
func TestMCPSharedContract_ValidateRetainedOnly(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]byte) []byte
		valid  bool
	}{
		{"retained assessment", func(b []byte) []byte { return b }, true},
		{"fresh execution capture", func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"snapshot_id":"snapshot-1"`, `"snapshot_id":"snapshot-1","capture":{"files":[],"checks":[]}`, 1))
		}, false},
		{"mode override", func(b []byte) []byte { return append([]byte(`{"mode":"implement",`), b[1:]...) }, false},
		{"foreign story", func(b []byte) []byte { return []byte(strings.ReplaceAll(string(b), "US-001", "US-foreign")) }, false},
		{"unretained snapshot", func(b []byte) []byte { return []byte(strings.ReplaceAll(string(b), "snapshot-1", "snapshot-foreign")) }, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			profile, err := submission.NewToolProfile("validate")
			if err != nil {
				t.Fatal(err)
			}
			if !profile.Allows("communication_submit") || profile.Allows("capture_execution") {
				t.Fatal("Validate communication/execution boundary changed")
			}
			result, scope := createFixture("validate", "completed")
			data, err := json.Marshal(ar.CreateInput{Result: result})
			if err != nil {
				t.Fatal(err)
			}
			_, diagnostics := ar.Create(tt.mutate(data), scope)
			if diagnostics.Valid != tt.valid || !diagnostics.Complete {
				t.Fatalf("valid=%v want=%v diagnostics=%+v", diagnostics.Valid, tt.valid, diagnostics)
			}
			if !tt.valid && len(diagnostics.Diagnostics) == 0 {
				t.Fatal("refusal lacks diagnostics")
			}
		})
	}
}
