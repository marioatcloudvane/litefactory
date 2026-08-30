package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestArgvCarriesNoStringOriginatingFromGitHub is AC-2.4, the load-bearing
// security test (C2): a run through the real pipeline — turn.Execute into a
// real *harness.Runner — with a resolved profile and issue labels standing in
// for what a maintainer put on the issue. The label text must reach argv
// nowhere at all, and everything that does reach argv must be exactly the
// operator-authored strings the (fake) resolver put on the request.
//
// The labels here are exactly what [Daemon.resolveTools] would have read to
// select this profile — this test starts one hop downstream of that
// resolution, on the [Request] it produces, which is the boundary internal/turn
// actually owns. What it proves is that nothing between that boundary and the
// child's argv can let a label's own text leak through, however suggestively
// it is named.
func TestArgvCarriesNoStringOriginatingFromGitHub(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	runner, tmpDir := realRunner(t, f, fakeClaudeArgvDumpScript)
	f.opts.Claude = runner

	const sentinel = "ZZZ-LABEL-TEXT-MUST-NEVER-REACH-ARGV-ZZZ"
	req := f.request()
	req.Issue.Labels = []string{"lf:tools=research", sentinel, "LF:Tools=research"}
	req.Tools = []string{"Read", "Edit"}
	req.AllowedTools = []string{"Read"}
	req.ToolProfile = "research"

	result := f.execute(req)
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	dump, err := os.ReadFile(filepath.Join(tmpDir, "argv.txt"))
	if err != nil {
		t.Fatalf("read the argv the child received: %v", err)
	}
	argvText := string(dump)

	// The label text: not the sentinel, not the label that happened to select
	// the profile, not a mis-cased variant of it.
	for _, leaked := range []string{sentinel, "lf:tools=research", "LF:Tools=research"} {
		if strings.Contains(argvText, leaked) {
			t.Errorf("argv contains label-derived text %q:\n%s", leaked, argvText)
		}
	}
	// The profile *name* is a lookup key and is never joined into argv either,
	// per [Request.ToolProfile]'s own contract.
	if strings.Contains(argvText, "research") {
		t.Errorf("argv contains the profile name, which must never be joined into it:\n%s", argvText)
	}

	// What it does carry: exactly the operator-authored lists the request
	// carried, and nothing more or less (AC-2.5).
	argv := strings.Split(strings.TrimRight(argvText, "\n"), "\n")
	i := indexOf(argv, "--tools")
	if i < 0 || argv[i+1] != "Read,Edit" {
		t.Errorf("--tools = %v, want Read,Edit", argv)
	}
	j := indexOf(argv, "--allowedTools")
	if j < 0 || argv[j+1] != "Read" {
		t.Errorf("--allowedTools = %v, want Read", argv)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestLabelsNeverReachThePrompt is AC-2.13. [Issue.Labels] documents itself
// as a lookup key and nothing else (S-13); this drives a real prompt assembly
// with a label whose text would be unmistakable if it leaked, and reads the
// prompt back from the one place it is durably recorded — the run's metadata
// document — the same way [TestExecuteThroughTheRealHarness] does for the
// issue title.
func TestLabelsNeverReachThePrompt(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	const sentinel = "UNIQUE-LABEL-TEXT-9f3-MUST-NOT-APPEAR-IN-ANY-PROMPT"
	req := f.request()
	req.Issue.Labels = []string{"lf:tools=research", sentinel}
	req.Tools = []string{"Read"}
	req.ToolProfile = "research"

	result := f.execute(req)
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	sent := runner.requests[0].Prompt
	if strings.Contains(sent, sentinel) || strings.Contains(sent, "lf:tools=research") {
		t.Errorf("the prompt sent to claude carries label text:\n%s", sent)
	}

	meta := f.readMeta(1)
	if strings.Contains(meta.Prompt, sentinel) || strings.Contains(meta.Prompt, "lf:tools=research") {
		t.Errorf("the recorded prompt carries label text:\n%s", meta.Prompt)
	}
}

// TestColdRerunCarriesTheSameProfile is AC-2.12: harvest.go is unmodified, and
// [preparation.request] is the same struct on both attempts of a resume
// refusal's rerun — only Prompt and Path are overwritten
// (harvest.go:126-129) — so a resolved profile rides the cold rerun for free.
// This is the executable proof of that claim.
func TestColdRerunCarriesTheSameProfile(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		if req.Path == harness.PathResume {
			return refused(req, f.paths.Home())
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, store.ModeSpike, store.RunReported)

	req := f.request()
	req.Tools = []string{"Read", "Bash"}
	req.AllowedTools = []string{"Bash(ls:*)"}
	req.ToolProfile = "research"

	result := f.execute(req)
	if !result.Succeeded() {
		t.Fatalf("the cold rerun did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if runner.callCount() != 2 {
		t.Fatalf("claude ran %d times, want exactly 2: the refused resume and one cold rerun", runner.callCount())
	}

	first, second := runner.requests[0], runner.requests[1]
	if first.Path != harness.PathResume || second.Path != harness.PathColdAfterResumeRefused {
		t.Fatalf("paths = %s then %s, want resume then cold_after_resume_refused", first.Path, second.Path)
	}
	if first.ToolProfile != "research" || second.ToolProfile != "research" {
		t.Errorf("ToolProfile = %q then %q, want %q on both attempts", first.ToolProfile, second.ToolProfile, "research")
	}
	if !equalStringSlices(first.Tools, req.Tools) || !equalStringSlices(second.Tools, req.Tools) {
		t.Errorf("Tools = %v then %v, want the same %v on both attempts", first.Tools, second.Tools, req.Tools)
	}
	if !equalStringSlices(first.AllowedTools, req.AllowedTools) || !equalStringSlices(second.AllowedTools, req.AllowedTools) {
		t.Errorf("AllowedTools = %v then %v, want the same %v on both attempts",
			first.AllowedTools, second.AllowedTools, req.AllowedTools)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
