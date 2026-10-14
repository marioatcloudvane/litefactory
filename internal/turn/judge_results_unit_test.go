package turn

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
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
	evidence := JudgeEvidenceSnapshot{Evidence: []agentresult.JudgeEvidence{{ID: "source", Kind: "source", Available: true, Essential: true, Lines: 3}}, Contents: map[string]string{"source": "one\ntwo\nthree"}}
	service, err := bindJudgeResults(t.Context(), db, a.ID, evidence)
	if err != nil {
		t.Fatal(err)
	}
	return service, sourceWriter
}

func TestJudgeResultPreflightRejectsInvalidOutputWithoutSaving(t *testing.T) {
	service, source := judgeResultUnitFixture(t)
	for _, raw := range []string{
		`{"report":`,
		`{"report":{"version":1,"verdict":"concerns_found","summary":"Concern","coverage":["source"],"uncertainty":[],"findings":[{"severity":"high","concern":"claim","impact":"risk","correction":"fix","evidence":[{"evidence_id":"missing"}]}]}}`,
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
	raw := []byte(`{"report":{"version":1,"verdict":"no_material_concerns","summary":"Examined captured source","coverage":["source"],"uncertainty":[],"findings":[]}}`)
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

func TestJudgeEvidenceToolReadsOnlyCapturedBytes(t *testing.T) {
	service, _ := judgeResultUnitFixture(t)
	raw, err := service.handle(t.Context(), submission.Request{Method: "evidence_read", Payload: json.RawMessage(`{"id":"source","start":2,"count":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Body      string `json:"body"`
		Untrusted bool   `json:"untrusted"`
	}
	if err = json.Unmarshal(raw, &reply); err != nil || reply.Body != "two" || !reply.Untrusted {
		t.Fatalf("read: %s %v", raw, err)
	}
	raw, err = service.handle(t.Context(), submission.Request{Method: "evidence_read", Payload: json.RawMessage(`{"id":"../../secrets","start":1,"count":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	var denied struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err = json.Unmarshal(raw, &denied); err != nil || denied.Valid || denied.Error == "" {
		t.Fatalf("arbitrary path read: %s %v", raw, err)
	}
}

func TestJudgeSnapshotRejectsSymlinkEvidenceAndPreservesCapturedBytes(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("src/main.go", "original source")
	repo.WriteFile("ancestor/file.go", "tracked bytes")
	repo.WriteFile("secret-target/file.go", "private bytes")
	repo.WriteFile(".env", "credential value")
	repo.Commit("capture fixture")
	if err := os.RemoveAll(filepath.Join(repo.Dir, "ancestor")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("secret-target", filepath.Join(repo.Dir, "ancestor")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("secret-target/file.go", filepath.Join(repo.Dir, "link.go")); err != nil {
		t.Fatal(err)
	}
	repo.WriteFile("large.go", strings.Repeat("x", judgeFileLimit+1))
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureJudgeEvidence(t.Context(), binary, repo.Dir, "pinned source", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"code:.env", "code:ancestor/file.go", "code:link.go", "code:large.go"} {
		if _, ok := snapshot.Contents[id]; ok {
			t.Fatalf("captured prohibited evidence %s", id)
		}
		found := false
		for _, e := range snapshot.Evidence {
			if e.ID == id {
				found = true
				if e.Available || e.Gap == "" {
					t.Fatalf("undisclosed omission: %+v", e)
				}
			}
		}
		if !found {
			t.Fatalf("omission absent: %s", id)
		}
	}
	repo.WriteFile("src/main.go", "new source")
	captured, err := snapshot.Read("code:src/main.go", 1, 1)
	if err != nil || captured != "original source" {
		t.Fatalf("retargeted snapshot: %q %v", captured, err)
	}
}

func TestJudgeEvidenceLongLineChunksPreserveUTF8(t *testing.T) {
	service, _ := judgeResultUnitFixture(t)
	original := strings.Repeat("é", 40000)
	service.evidence.Contents["source"] = original
	var collected strings.Builder
	offset := 0
	for {
		input, _ := json.Marshal(map[string]any{"id": "source", "start": 1, "count": 1, "byte_offset": offset, "byte_count": 65535})
		call, _ := json.Marshal(map[string]any{"name": "evidence_read", "arguments": json.RawMessage(input)})
		raw, err := service.handle(t.Context(), submission.Request{Method: "tools/call", Payload: call})
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Body string `json:"body"`
			Next int    `json:"next_byte_offset"`
		}
		if err = json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		collected.WriteString(response.Body)
		if response.Next == 0 {
			break
		}
		if response.Next <= offset {
			t.Fatal("chunk did not advance")
		}
		offset = response.Next
	}
	if collected.String() != original {
		t.Fatal("long evidence was clipped or corrupted")
	}
}
