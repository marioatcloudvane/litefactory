package daemon_test

import (
	"strconv"
	"strings"
	"testing"
)

// TestEveryLineBelowPollNamesItsTask is AC-PAR-7.1. A line emitted while a task
// is being worked has to say which task, and which repository, without the
// reader inferring it from the lines around it.
//
// That inference is what stops working when the daemon runs tasks concurrently:
// the lines of two tasks interleave, and an undecorated one is then attributable
// only by guesswork. The test exists now, before the concurrency, because the
// decoration is what makes the later change readable rather than something to
// be added once it is already needed.
//
// Three lines stand in for the whole set — one from [Daemon.bootstrap], one from
// [Daemon.acknowledge] and one from [Daemon.run] — because they are reached from
// the three different depths below poll that carry a task: the first sight, the
// comment queue, and the turn itself.
func TestEveryLineBelowPollNamesItsTask(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight, which bootstraps the watermark

	f.forge.addComment(testIssue, 40, owner(), "/spike add a health endpoint")
	f.settle()
	if got := f.turns.triggers(); len(got) != 1 {
		t.Fatalf("ran %d turns, want 1\nlogs:\n%s", len(got), f.logs)
	}

	wantRepo := "repo=" + testOwnerName + "/" + testRepoName
	wantTask := "task=github:" + testOwnerName + "/" + testRepoName + "#" + strconv.Itoa(testIssue)

	for _, msg := range []string{
		"first sight of a task",
		"acknowledged the comment",
		"turn complete",
	} {
		line := logLine(t, f.logs.String(), msg)
		if !strings.Contains(line, wantTask) {
			t.Errorf("the %q line does not carry %s:\n%s", msg, wantTask, line)
		}
		if !strings.Contains(line, wantRepo) {
			t.Errorf("the %q line does not carry %s:\n%s", msg, wantRepo, line)
		}
		// A child logger and a call site that both set the same key print it
		// twice. Dropping the attribute at the call site when the child already
		// carries it is part of this decoration, so assert it stayed dropped.
		for _, key := range []string{"task=", "repo=", "session_id="} {
			if n := strings.Count(line, key); n > 1 {
				t.Errorf("the %q line carries %s %d times, want once:\n%s", msg, key, n, line)
			}
		}
	}
}

// logLine returns the one logged line whose message contains want, and fails
// the test with the whole log if there is none.
func logLine(t *testing.T, logs, want string) string {
	t.Helper()

	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no line containing %q was logged\nlogs:\n%s", want, logs)
	return ""
}
