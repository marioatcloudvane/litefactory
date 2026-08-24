package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// The fake claude: a shell script on a test-controlled PATH, per the plan's
// testing rules. It exercises the real binary resolution, the real pipes, the
// real stdin delivery and the real event stream, with none of the cost or the
// non-determinism of the actual binary.
//
// It writes the prompt it was given into $TMPDIR, which is one of the eight
// variables the harness allowlist forwards (SD-4) — so the assertion that the
// prompt arrived on stdin is made from the child's own point of view.
const fakeClaudeScript = `
cat > "$TMPDIR/prompt.txt"
printf 'package api // written by the run\n' > health.go
printf '%s\n' '{"type":"system","subtype":"init","cwd":"w","session_id":"1a17a10a-0000-5000-8000-000000000001","tools":["Read","Edit"],"model":"claude-fake-1","permissionMode":"default"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"duration_ms":1673,"num_turns":2,"result":"Added a /health endpoint.","total_cost_usd":0.0125}'
`

// TestExecuteThroughTheRealHarness runs the whole turn against a real
// *harness.Runner.
//
// Every other test in this package substitutes the runner, which is the right
// trade for the paths that are about locking and bookkeeping. This one is the
// counterweight: it is the only test that proves the wiring between the two
// packages — that the prompt reaches the child's stdin, that the child runs in
// the worktree rather than the clone, that the ordinal picked in the database
// is the ordinal the JSONL is written under, and that the model from the init
// event lands in NNNN.meta.json (AC-3.17).
//
// It runs against a dirtied clone, so it is also a second, independent
// assertion of AC-3.10 through the real subprocess path.
func TestExecuteThroughTheRealHarness(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	f.repo.Dirty()

	binDir := filepath.Join(filepath.Dir(f.repo.Dir), "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create the fake bin directory: %v", err)
	}
	script := filepath.Join(binDir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+fakeClaudeScript), 0o700); err != nil { //nolint:gosec // a fixture executable.
		t.Fatalf("write the fake claude: %v", err)
	}
	tmpDir := t.TempDir()

	runner, err := harness.New(harness.Options{
		Paths: f.paths,
		Claude: config.Claude{
			Binary:         "claude",
			Tools:          []string{"Read", "Edit"},
			AllowedTools:   []string{"Read", "Edit(./**)"},
			SettingsPath:   filepath.Join(f.paths.Home(), "claude-settings.json"),
			TimeoutSeconds: 60,
		},
		Environ: []string{
			"PATH=" + binDir + ":/usr/bin:/bin",
			"HOME=" + filepath.Dir(f.repo.Dir),
			"USER=lf-test-user",
			"SHELL=/bin/sh",
			"TMPDIR=" + tmpDir,
			// Credential-shaped variables the boundary has to stop. Nothing
			// here asserts on them — internal/harness owns AC-3.13 — but a turn
			// that reached around the harness to exec claude itself would be
			// handing them over, and this makes that visible.
			"GITHUB_TOKEN=ghp_leakcanary000000000000",
			"LF_HOME=" + f.paths.Home(),
		},
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("build the harness runner: %v", err)
	}
	f.opts.Claude = runner

	var result Result
	gittest.AssertUnchanged(t, f.repo.Dir, func() {
		result = f.execute(f.request())
	})

	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if !result.Committed || !result.Pushed {
		t.Errorf("committed=%t pushed=%t, want both", result.Committed, result.Pushed)
	}
	if !f.originHas(testBranch) {
		t.Errorf("origin does not carry %s", testBranch)
	}

	// AC-3.12: the prompt went on stdin, and it is the prompt the metadata
	// document records verbatim (AC-3.11).
	sent, err := os.ReadFile(filepath.Join(tmpDir, "prompt.txt"))
	if err != nil {
		t.Fatalf("read the prompt the child received: %v", err)
	}
	meta := f.readMeta(1)
	if string(sent) != meta.Prompt {
		t.Errorf("the prompt on stdin differs from the one recorded:\nstdin: %q\nmeta:  %q", sent, meta.Prompt)
	}
	if !strings.Contains(meta.Prompt, testIssueTitle) {
		t.Errorf("the prompt does not carry the issue title:\n%s", meta.Prompt)
	}

	// AC-3.17: model and tool surface come from the init event.
	if meta.Model != "claude-fake-1" {
		t.Errorf("meta model = %q, want the init event's claude-fake-1", meta.Model)
	}
	if len(meta.Tools) != 2 {
		t.Errorf("meta tools = %v, want the init event's two", meta.Tools)
	}
	if meta.CostUSD == nil || *meta.CostUSD != 0.0125 {
		t.Errorf("meta cost = %v, want the result event's 0.0125", meta.CostUSD)
	}

	// S-1: the ordinal the database allocated is the ordinal on disk.
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunSucceeded {
		t.Fatalf("runs = %+v, want one succeeded row", runs)
	}
	if !strings.HasSuffix(runs[0].LogPath, filepath.Join("123", "0001.jsonl")) {
		t.Errorf("log path = %q, want it to end in 123/0001.jsonl", runs[0].LogPath)
	}
	if _, err := os.Stat(runs[0].LogPath); err != nil {
		t.Errorf("the event stream was not written: %v", err)
	}
}
