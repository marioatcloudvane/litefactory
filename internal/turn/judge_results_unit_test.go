package turn

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func judgeResultUnitFixture(t *testing.T) (*JudgeResultService, *AgentResultService) {
	t.Helper()
	db, session, options := agentResultFixture(t, "research")
	sourceWriter, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	source := store.JudgeSource{Reference: "result:7:1:digest", Task: session.TaskRef, Kind: "result", Snapshot: []byte(`{"saved":"research"}`)}
	q, err := db.AdmitJudgeRequest(t.Context(), store.JudgeRequest{Task: session.TaskRef, TicketKey: "qualified-ticket", CommentID: "comment-9", SessionID: session.ID, TriggerCommentID: 9, Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.StartJudgeAttempt(t.Context(), q.ID, "codex", "model", "high")
	if err != nil {
		t.Fatal(err)
	}
	service, err := bindJudgeResults(t.Context(), db, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return service, sourceWriter
}

func TestJudgeResultPreflightRejectsInvalidOutputWithoutSaving(t *testing.T) {
	service, source := judgeResultUnitFixture(t)
	for _, raw := range []string{
		`{"report":`,
		`{"report":{"version":1,"verdict":"concerns_found","summary":"Concern","coverage":["source"],"uncertainty":[],"findings":[{"severity":"high","concern":"claim","impact":"risk","correction":"fix","evidence":[{"path":"/etc/passwd"}]}]}}`,
	} {
		d, err := service.Preflight(t.Context(), []byte(raw))
		if err != nil || d.Valid {
			t.Fatalf("invalid preflight: %+v %v", d, err)
		}
	}
	if _, err := service.db.GetResultRevision(t.Context(), service.binding, 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preflight saved revision: %v", err)
	}
	if err := service.db.CheckResultWriter(t.Context(), source.Binding()); err != nil {
		t.Fatalf("judge revoked source writer: %v", err)
	}
}

func TestJudgeResultAppendOnlyReplayAndReceipt(t *testing.T) {
	service, source := judgeResultUnitFixture(t)
	raw := []byte(`{"report":{"version":1,"verdict":"no_material_concerns","summary":"Examined captured source","coverage":["result.md"],"uncertainty":[],"findings":[]}}`)
	d, err := service.Preflight(t.Context(), raw)
	if err != nil || !d.Valid {
		t.Fatalf("preflight: %+v %v", d, err)
	}
	first, err := service.Submit(t.Context(), "advisory-1", raw)
	if err != nil || first == nil || first.InputState != "accepted" || first.AppliedRevision != 1 {
		t.Fatalf("submit: %+v %v", first, err)
	}
	replay, err := service.Submit(t.Context(), "advisory-1", raw)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	receipt, err := service.Receipt(t.Context(), "advisory-1")
	if err != nil || receipt.ID != first.ID || !bytes.Equal(receipt.Payload, raw) {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	if _, err = service.Submit(t.Context(), "advisory-1", bytes.Replace(raw, []byte("Examined"), []byte("Changed"), 1)); err == nil {
		t.Fatal("same request accepted different bytes")
	}
	second, err := service.Submit(t.Context(), "advisory-2", raw)
	if err == nil && (second == nil || second.InputState == "accepted" || second.AppliedRevision != 0) {
		t.Fatalf("second write: %+v %v", second, err)
	}
	accepted, err := service.accepted(t.Context())
	if err != nil || accepted.Summary != "Examined captured source" {
		t.Fatalf("accepted report changed: %+v %v", accepted, err)
	}
	if err := service.db.CheckResultWriter(t.Context(), source.Binding()); err != nil {
		t.Fatalf("judge revoked source writer: %v", err)
	}
}

// The broker serves only the result tools; files are read with the provider's
// own read-only tools, so no evidence-reading method remains.
func TestJudgeBrokerServesOnlyResultTools(t *testing.T) {
	service, _ := judgeResultUnitFixture(t)
	raw, err := service.handle(t.Context(), submission.Request{Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err = json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, agent.JudgeToolNames()) {
		t.Fatalf("broker tools %v, want %v", names, agent.JudgeToolNames())
	}
	for _, method := range []string{"evidence_list", "evidence_read"} {
		if _, err := service.handle(t.Context(), submission.Request{Method: method, Payload: json.RawMessage(`{"id":"source","start":1,"count":1}`)}); err == nil {
			t.Fatalf("%s still served", method)
		}
	}
}
