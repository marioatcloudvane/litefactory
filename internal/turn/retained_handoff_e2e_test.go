package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRetainedWorkflowHandoffRestartReuse(t *testing.T) {
	db, session, options := agentResultFixture(t, "research")
	ctx := t.Context()
	logRoot := t.TempDir()
	logPath := filepath.Join(logRoot, "command.log")
	command := "retained command stdout\nretained command stderr\n"
	if err := os.WriteFile(logPath, []byte(command), 0600); err != nil {
		t.Fatal(err)
	}
	// The first fixture run becomes a retained command source; the next one
	// receives the handoff. Recovery must never allocate another execution.
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "result", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return logPath }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(ctx, prior.ID, store.RunOutcome{Status: store.RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	current := agentResultRun(t, db, session, "research", "recovery-receiver", 3)
	options.Attempt = current
	service, err := BindAgentResultService(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Submit(ctx, "captured-finding", []byte(`{"result":{"outcome":"incomplete","reason":"Review remains","comment":"Exact retained workflow finding.","payload":{"research":{"request":"Retained request"}}}}`))
	if err != nil || !result.Validation.Valid {
		t.Fatalf("capture result: %+v %v", result, err)
	}
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "fixture", Mode: "research", ProfileVersion: 2, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := json.Marshal(map[string][]byte{"research.dot": []byte("digraph research {}"), "agents/worker.md": []byte("Exact retained workflow guidance."), "skills/guide.md": []byte("Exact retained resource closure.")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	launch, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "research", SessionID: session.ID, TriggerID: 3, DefinitionID: definition.ID, Start: "work", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	var pinned string
	prepare := func(dir string) (retainedevidence.Manifest, map[string]string) {
		t.Helper()
		m, err := service.PrepareRetainedEvidence(ctx, WorkflowEvidenceOptions{LaunchID: launch.LaunchID, Directory: dir, LogRoot: logRoot, Redactor: redact.New(redact.Options{}), ManifestRevision: pinned})
		if err != nil {
			t.Fatal(err)
		}
		return m, workflowRetainedTree(t, dir)
	}
	firstDir := t.TempDir()
	manifest, content := prepare(firstDir)
	pinned = manifest.Revision
	joined := ""
	for _, body := range content {
		joined += body
	}
	for _, want := range []string{"Exact retained workflow guidance.", "Exact retained resource closure.", "Exact retained workflow finding.", command} {
		if !strings.Contains(joined, want) {
			t.Fatalf("handoff missing %q", want)
		}
	}
	if err = os.RemoveAll(firstDir); err != nil {
		t.Fatal(err)
	}
	binding, path := service.Binding(), db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	service, err = OpenAgentResultReader(ctx, reopened, binding, agentresult.EditTargets("research"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, bytes := prepare(t.TempDir())
		if !reflect.DeepEqual(manifest, got) || !reflect.DeepEqual(content, bytes) {
			t.Fatal("restart/reuse changed durable identities or authorized bytes")
		}
	}
	runs, err := reopened.ListRuns(ctx, session.ID)
	if err != nil || len(runs) != 3 {
		t.Fatalf("recovery created executions: %d %v", len(runs), err)
	}
	// A changed retained log must leave a version-bound gap, never disclose the
	// replacement bytes as part of the old handoff.
	if err = os.WriteFile(logPath, []byte("newer output must stay withheld"), 0600); err != nil {
		t.Fatal(err)
	}
	_, changed := prepare(t.TempDir())
	for _, body := range changed {
		if strings.Contains(body, "newer output must stay withheld") {
			t.Fatal("new log substituted for captured version")
		}
	}
}

func workflowRetainedTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[relative] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
