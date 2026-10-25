package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestRuntimeShorteningRetainsRecoveryDestination(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	row := f.allocateRun(session, 1, time.Now())
	// Put the recovery item beyond both the renderer's result cap and a small
	// comment budget. The offered existing raw route must retain its whole text.
	recovery := "Inspect the retained result with the current command. Repeating an uncertain effect can create duplicate work."
	text := strings.Repeat("Retained observations.\n", 2000) + recovery
	event, err := json.Marshal(map[string]any{"type": "result", "result": text, "is_error": false})
	if err != nil {
		t.Fatal(err)
	}
	raw := string(event) + "\n"
	path := f.writeLog(session, row.Ordinal, raw)
	f.finishRun(row, "succeeded", 0, time.Second)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 4000})
	if err != nil {
		t.Fatal(err)
	}
	run := report.Run{Ref: f.ref(session), Ordinal: row.Ordinal, LogPath: path, Result: &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: text}}
	body, err := renderer.RenderRun(t.Context(), run)
	if err != nil {
		t.Fatal(err)
	}
	want := report.RunInspectionCommand(run.Ref, row.Ordinal) + " --raw"
	if strings.Contains(body.String(), recovery) || !strings.Contains(body.String(), want) {
		t.Fatalf("short report lost destination: %s", body.String())
	}
	code, out, stderr := f.run("logs", session.TaskRef, "--run", "1", "--raw")
	if code != 0 || stderr != "" || out != raw {
		t.Fatalf("destination did not retain complete bytes: code=%d stderr=%s", code, stderr)
	}
	var saved struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &saved); err != nil || !strings.Contains(saved.Result, recovery) {
		t.Fatalf("omitted action/effect unavailable: %v", err)
	}
}
