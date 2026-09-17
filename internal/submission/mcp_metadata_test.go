package submission_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestValidateMCPMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"absent", "", true}, {"empty", `{}`, true},
		{"captured Codex token", `{"progressToken":0}`, true},
		{"string token", `{"progressToken":"request-1"}`, true},
		{"fractional token", `{"progressToken":-1.25e2}`, true},
		{"extensions", `{"progressToken":1,"vendor/example":{"mode":"untrusted","nested":[true,null]}}`, true},
		{"at byte limit", `{"x":"` + strings.Repeat("x", (64<<10)-8) + `"}`, true},
		{"over byte limit", `{"x":"` + strings.Repeat("x", (64<<10)-7) + `"}`, false},
		{"null", `null`, false}, {"array", `[]`, false}, {"scalar", `1`, false},
		{"null token", `{"progressToken":null}`, false}, {"boolean token", `{"progressToken":true}`, false},
		{"object token", `{"progressToken":{}}`, false}, {"duplicate", `{"progressToken":0,"progressToken":1}`, false},
		{"trailing document", `{} {}`, false}, {"malformed", `{"progressToken":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := submission.ValidateMCPMetadata(json.RawMessage(tc.input)); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestDecodeToolCall_MetadataPreservesArguments(t *testing.T) {
	t.Parallel()
	args := "{\n  \"result\": {\"comment\":\"spaces  and \\u0061\", \"number\":1e2}\n}"
	for _, meta := range []string{`{}`, `{"progressToken":0}`, `{"progressToken":"token","vendor/test":{"mode":"validate"}}`} {
		got, err := submission.DecodeToolCall([]byte(`{"name":"communication_submit","arguments":` + args + `,"_meta":` + meta + `}`))
		if err != nil || got.Name != "communication_submit" || string(got.Arguments) != args {
			t.Fatalf("metadata %s changed contribution: %+v %v", meta, got, err)
		}
		if _, err := submission.DecodeToolCall([]byte(`{"name":"communication_submit","arguments":` + args + `,"_meta":` + meta + `,"mode":"validate"}`)); err == nil {
			t.Fatal("metadata allowed application field")
		}
	}
}
