package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/redact"
)

func TestRetainedWorkflowEvidenceCurrentGrantAndRedaction(t *testing.T) {
	db, _, options := agentResultFixture(t, "research")
	writer, err := BindAgentResultService(t.Context(), options)
	mustProgress(t, err)
	secret := "captured-secret-token-1049"
	input := []byte(`{"result":{"outcome":"incomplete","reason":"Further work remains","comment":"Visible captured-secret-token-1049","payload":{"research":{"request":"PRIVATE sibling request"}}}}`)
	submitted, err := writer.Submit(t.Context(), "captured", input)
	mustProgress(t, err)
	if submitted.Snapshot == nil {
		t.Fatalf("result not retained: %+v", submitted)
	}
	reader, err := OpenAgentResultReader(t.Context(), db, writer.Binding(), []agentresult.Target{{Field: "comment"}})
	mustProgress(t, err)
	dir := t.TempDir()
	redactor := redact.New(redact.Options{Environ: []string{"GITHUB_TOKEN=" + secret}})
	manifest, err := reader.PrepareRetainedEvidence(t.Context(), WorkflowEvidenceOptions{Directory: dir, Redactor: redactor})
	mustProgress(t, err)
	var index struct {
		Views []WorkflowEvidenceView `json:"views"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "retained-evidence", "index-000000.json"))
	mustProgress(t, err)
	mustProgress(t, json.Unmarshal(raw, &index))
	found := false
	for i, entry := range manifest.Entries {
		if entry.Kind != "result_revision" {
			continue
		}
		view := index.Views[i]
		if view.Path == "" {
			t.Fatalf("authorized comment unavailable: %+v", view)
		}
		raw, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(view.Path)))
		mustProgress(t, err)
		if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "PRIVATE sibling request") {
			t.Fatalf("unauthorized or unredacted view: %s", raw)
		}
		if !strings.Contains(string(raw), "Visible") {
			t.Fatalf("authorized comment missing: %s", raw)
		}
		if view.Digest == entry.Digest {
			t.Fatal("projected/redacted view reused raw source digest")
		}
		found = true
	}
	if !found {
		t.Fatal("result source absent")
	}
}

func TestRetainedWorkflowEvidenceRejectsSymlinkSandbox(t *testing.T) {
	_, _, options := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), options)
	mustProgress(t, err)
	dir, outside := t.TempDir(), t.TempDir()
	mustProgress(t, os.Symlink(outside, filepath.Join(dir, "retained-evidence")))
	_, err = service.PrepareRetainedEvidence(t.Context(), WorkflowEvidenceOptions{Directory: dir, Redactor: redact.New(redact.Options{Environ: []string{}})})
	if err == nil {
		t.Fatal("symlink evidence directory accepted")
	}
	entries, err := os.ReadDir(outside)
	mustProgress(t, err)
	if len(entries) != 0 {
		t.Fatal("evidence escaped invocation sandbox")
	}
}
