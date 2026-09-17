package turn

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestFeedbackSocketSharedApplicationValidationAndRecovery(t *testing.T) {
	db, _, opts := agentResultFixture(t, "research")
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	o := familyResearchOptions(t, db, app)
	client, _, _ := familySocketFixture(t, db, o)
	for _, input := range [][]byte{resultCreate("research"), []byte(`{"result":{"outcome":"completed","payload":{}}}`), []byte(`{"mode":"plan","result":{}}`)} {
		baseline, err := app.Preflight(t.Context(), input)
		mustProgress(t, err)
		args, err := json.Marshal(map[string]any{"kind": "application", "input": json.RawMessage(input)})
		mustProgress(t, err)
		result := mcpToolCall(t, client, "feedback_preflight", args)
		var actual ar.UpdateDiagnostics
		mustProgress(t, json.Unmarshal(result.Result, &actual))
		if !reflect.DeepEqual(actual, baseline) {
			t.Fatalf("validator parity: %+v want %+v", actual, baseline)
		}
		if _, err := db.GetResultReceipt(t.Context(), app.Binding(), app.Binding().RunID, result.RequestID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("preflight recorded submission: %v", err)
		}
		repeat := mcpToolCall(t, client, "feedback_preflight", args)
		if repeat.RequestID != result.RequestID {
			t.Fatal("retry lost journal identity")
		}
		var receipt FamilyReceipt
		mustProgress(t, json.Unmarshal(repeat.Result, &receipt))
		if receipt.Disposition != "confirmed" || receipt.Tool != "feedback_preflight" || receipt.Reference.Kind != "" {
			t.Fatalf("preflight retry: %+v", receipt)
		}
		lookup := mcpToolCall(t, client, "receipts_get", mcpReceiptArgs(t, result.RequestID))
		if !bytes.Equal(lookup.Result, repeat.Result) {
			t.Fatalf("receipt differs: %s %s", lookup.Result, repeat.Result)
		}
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), app.Binding())
	mustProgress(t, err)
	if len(revisions) != 0 {
		t.Fatal("preflight persisted application data")
	}
	// Communication remains usable with family binding and retains its original journal identity.
	data := resultCreate("research")
	first := mcpToolCall(t, client, "communication_submit", data)
	again := mcpToolCall(t, client, "communication_submit", data)
	if first.RequestID != again.RequestID {
		t.Fatal("communication retry identity changed")
	}
	revisions, err = db.ResultRevisionNumbers(t.Context(), app.Binding())
	mustProgress(t, err)
	if !reflect.DeepEqual(revisions, []int64{1}) {
		t.Fatalf("communication revisions: %v", revisions)
	}
}

func TestFeedbackSocketCanonicalCreationParity(t *testing.T) {
	db, _, opts := agentResultFixture(t, "research")
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	o := familyResearchOptions(t, db, app)
	client, _, _ := familySocketFixture(t, db, o)
	base := submission.CreationEnvelope{Version: 1, Repository: "example/results", SourceTask: app.Binding().Task, Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Reviewed title", Body: "Reviewed body", LinkIssues: []int{263}}}}
	for _, name := range []string{"valid", "empty", "long title", "relationship", "foreign"} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(base)
			mustProgress(t, err)
			var envelope submission.CreationEnvelope
			mustProgress(t, json.Unmarshal(raw, &envelope))
			switch name {
			case "empty":
				envelope.Artifacts = nil
			case "long title":
				envelope.Artifacts[0].Title = strings.Repeat("x", 257)
			case "relationship":
				envelope.Relationship = "attach"
			case "foreign":
				envelope.Repository = "other/repo"
				envelope.SourceTask = "github:other/repo#9"
			}
			_, _, canonicalErr := submission.CanonicalCreation(envelope)
			args, err := json.Marshal(map[string]any{"kind": "creation", "input": envelope})
			mustProgress(t, err)
			response := mcpToolCall(t, client, "feedback_preflight", args)
			var result struct {
				Valid       bool     `json:"valid"`
				Diagnostics []string `json:"diagnostics"`
			}
			mustProgress(t, json.Unmarshal(response.Result, &result))
			want := canonicalErr == nil && name != "foreign"
			if result.Valid != want {
				t.Fatalf("canonical parity=%+v err=%v", result, canonicalErr)
			}
			if canonicalErr != nil && !slices.Contains(result.Diagnostics, canonicalErr.Error()) {
				t.Fatalf("shared diagnostic missing: %+v", result)
			}
		})
	}
	// Valid batch feedback leaves the creation gate closed and cannot publish.
	raw, err := json.Marshal(base)
	mustProgress(t, err)
	denied, err := client.Call(t.Context(), "issues_create_batch", raw)
	mustProgress(t, err)
	if denied.Error == "" {
		t.Fatal("preflight enabled creation")
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), app.Binding())
	mustProgress(t, err)
	if len(revisions) != 0 {
		t.Fatal("creation preflight retained domain proposal")
	}
}
