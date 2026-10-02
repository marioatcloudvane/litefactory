package turn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
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

	runner, tmpDir := realRunner(t, f, fakeClaudeScript)
	f.opts.Runners = runnerSet(runner)

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

// realRunner builds a *harness.Runner around a fake claude on a
// test-controlled PATH and returns it with the $TMPDIR the fake writes into.
//
// TMPDIR is one of the eight variables the harness allowlist forwards (SD-4),
// which is what lets a fake report on its own invocation from the child's point
// of view instead of the parent's.
func realRunner(t *testing.T, f *fixture, script string) (*harness.Runner, string) {
	t.Helper()

	binDir := filepath.Join(filepath.Dir(f.repo.Dir), "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create the fake bin directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"+script), 0o700); err != nil { //nolint:gosec // a fixture executable.
		t.Fatalf("write the fake claude: %v", err)
	}
	tmpDir := t.TempDir()

	runner, err := harness.New(harness.Options{
		Paths: f.paths,
		Model: config.DefaultClaudeModel,
		Claude: config.Claude{
			Binary:         "claude",
			Tools:          []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"},
			AllowedTools:   config.DefaultClaude().AllowedTools,
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
	return runner, tmpDir
}

// fakeClaudeArgvDumpScript is the same fake, reporting its argv instead of its
// stdin.
const fakeClaudeArgvDumpScript = `
printf '%s\n' "$@" > "$TMPDIR/argv.txt"
cat > /dev/null
printf 'package api // written by the run\n' > health.go
printf '%s\n' '{"type":"system","subtype":"init","cwd":"w","session_id":"1a17a10a-0000-5000-8000-000000000001","tools":["Read","Edit"],"model":"claude-fake-1","permissionMode":"default"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"duration_ms":1673,"num_turns":2,"result":"Added a /health endpoint.","total_cost_usd":0.0125}'
`

// storedClaudeSessionID is a canonical version 5 UUID that naming's derivation
// does not produce for the fixture's task: the shape of a segments row written
// by an older derivation, or by another build.
const storedClaudeSessionID = "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e"

// TestExecuteSendsTheStoredClaudeSessionID is AC-SR-1.2.
//
// The stored id and the derived one agree today, and GetOrCreateSegment's doc
// already settles what happens if they ever stop agreeing: the stored value
// wins, because it is the one claude has already seen. Resuming a conversation
// is what turns that from bookkeeping into correctness — a run that named the
// derived id would be asking for a transcript that does not exist.
//
// So the row is seeded with an id the derivation cannot produce and the child's
// own argv is the witness. It runs through a real *harness.Runner because the
// claim is precisely that nothing between the row and the process derives
// anything of its own.
func TestExecuteSendsTheStoredClaudeSessionID(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	if _, err := f.db.GetOrCreateSegment(
		t.Context(), f.session.ID, store.FirstSegmentOrdinal, storedClaudeSessionID, store.AgentClaude); err != nil {
		t.Fatalf("seed the segment: %v", err)
	}

	runner, tmpDir := realRunner(t, f, fakeClaudeArgvDumpScript)
	f.opts.Runners = runnerSet(runner)

	if result := f.execute(f.request()); !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	derived, err := naming.SessionRef(f.ref(), store.FirstSegmentOrdinal)
	if err != nil {
		t.Fatalf("derive the session ref: %v", err)
	}
	if derived == storedClaudeSessionID {
		t.Fatal("the seeded id equals the derived one, so this test asserts nothing")
	}

	dump, err := os.ReadFile(filepath.Join(tmpDir, "argv.txt"))
	if err != nil {
		t.Fatalf("read the argv the child received: %v", err)
	}
	argv := strings.Split(strings.TrimRight(string(dump), "\n"), "\n")

	i := slices.Index(argv, "--session-id")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("argv is missing --session-id: %q", argv)
	}
	if argv[i+1] != storedClaudeSessionID {
		t.Errorf("--session-id = %q, want the stored %q", argv[i+1], storedClaudeSessionID)
	}
	if slices.Contains(argv, derived) {
		t.Errorf("argv carries the derived id %q: something re-derived it instead of reading the row", derived)
	}

	// The metadata document is the other place the id is read from later, so it
	// has to name the same conversation.
	if got := f.readMeta(1).ClaudeSessionID; got != storedClaudeSessionID {
		t.Errorf("meta claude session id = %q, want the stored %q", got, storedClaudeSessionID)
	}
}

// TestSecondRunResumesInsteadOfReusingTheSessionID is AC-SR-2.1 and AC-SR-2.2,
// and it is the latent defect made executable.
//
// litefactory has never had a task with a second run. When it does, the second
// run reuses the segment — that is the whole point of the segment — and before
// this change it handed claude the same --session-id a second time. That does
// not resume anything: claude checks the id against the transcript on disk and
// exits 1 with "Session ID <uuid> is already in use", before emitting a single
// event. So the assertion is made where it can only be satisfied by the flag
// actually changing: on the child's own argv, through a real *harness.Runner*.
func TestSecondRunResumesInsteadOfReusingTheSessionID(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	// The rows a first run left behind: the conversation, and a run on it that
	// has stopped. That is the entire evidence the decision has.
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
		seededComment, "implement", store.RunReported)

	runner, tmpDir := realRunner(t, f, fakeClaudeArgvDumpScript)
	f.opts.Runners = runnerSet(runner)

	if result := f.execute(f.request()); !result.Succeeded() {
		t.Fatalf("the second run did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	dump, err := os.ReadFile(filepath.Join(tmpDir, "argv.txt"))
	if err != nil {
		t.Fatalf("read the argv the child received: %v", err)
	}
	argv := strings.Split(strings.TrimRight(string(dump), "\n"), "\n")

	i := slices.Index(argv, "--resume")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("the second run of the task did not pass --resume: %q", argv)
	}
	if argv[i+1] != storedClaudeSessionID {
		t.Errorf("--resume = %q, want the stored %q", argv[i+1], storedClaudeSessionID)
	}
	if slices.Contains(argv, "--session-id") {
		t.Errorf("the second run still passes --session-id, which claude rejects both as a reuse "+
			"and as a companion to --resume: %q", argv)
	}
	// AC-2.9: resuming is a swap, not a widening. The boundary flags are the
	// ones this must never have quietly dropped along with --session-id.
	for _, flag := range []string{"--tools", "--settings", "--allowedTools", "--strict-mcp-config"} {
		if !slices.Contains(argv, flag) {
			t.Errorf("the resumed run does not pass %s: %q", flag, argv)
		}
	}

	// The run is the task's second, and its document says it resumed.
	meta := f.readMeta(2)
	if !meta.Resumed {
		t.Error("the metadata document does not record that the run resumed")
	}
	if meta.ClaudeSessionID != storedClaudeSessionID {
		t.Errorf("meta claude session id = %q, want the stored %q", meta.ClaudeSessionID, storedClaudeSessionID)
	}
}
