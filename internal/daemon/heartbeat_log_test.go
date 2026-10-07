package daemon_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// logLines returns the captured log lines containing every one of parts.
func logLines(logs *syncBuffer, parts ...string) []string {
	var out []string
next:
	for _, line := range strings.Split(logs.String(), "\n") {
		for _, p := range parts {
			if !strings.Contains(line, p) {
				continue next
			}
		}
		out = append(out, line)
	}
	return out
}

// AC-001, AC-004, AC-005: every heartbeat writes exactly one DEBUG sentence on
// whichever path it leaves by, and a rate-limit backoff produces exactly one
// pause warning and exactly one resume line however many heartbeats it spans.
func TestHeartbeatLineAndPauseTransitionsAcrossABackoff(t *testing.T) {
	f := newFixture(t)
	const retryAfter = 45 * time.Second
	f.forge.quotaReset = f.clock.Now().Add(retryAfter).Unix()
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addComment(testIssue, 111, owner(), "/spike after the limit clears")
	issuesBefore, _, _, _ := f.forge.counts()
	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != issuesBefore+1 {
			return false
		}
		writeRateLimit(w, retryAfter)
		return true
	}
	beats := func() int { return len(logLines(f.logs, "heartbeat woken by")) }
	base := beats()

	if _, err := f.tickErr(); err == nil {
		t.Fatal("a rate-limited tick returned no error")
	}
	if got := beats() - base; got != 1 {
		t.Fatalf("failed heartbeat wrote %d lines, want 1\n%s", got, f.logs)
	}
	failed := logLines(f.logs, "level=DEBUG", "heartbeat woken by timer: failed")
	if len(failed) != 1 || !strings.Contains(failed[0], "GitHub was asked") {
		t.Fatalf("failed heartbeat line=%v", failed)
	}
	f.settle()
	if got := beats() - base; got != 2 {
		t.Fatalf("held-back heartbeat: %d lines in total, want 2", got)
	}
	held := logLines(f.logs, "heartbeat woken by timer: held back by a rate-limit backoff", "GitHub was not asked")
	if len(held) != 1 {
		t.Fatalf("held-back line=%v\n%s", held, f.logs)
	}

	warns := logLines(f.logs, "level=WARN", "discovery paused: rate-limit backoff")
	if len(warns) != 1 {
		t.Fatalf("pause warnings=%d, want exactly 1 across the failed and held-back heartbeats\n%s", len(warns), f.logs)
	}
	for _, want := range []string{"owner comments are not being read", "expected to resume 20"} {
		if !strings.Contains(warns[0], want) {
			t.Errorf("pause warning lacks %q: %s", want, warns[0])
		}
	}
	if got := logLines(f.logs, "discovery resumed after"); len(got) != 0 {
		t.Fatalf("resumed while still backing off: %v", got)
	}

	f.clock.advance(retryAfter + time.Second)
	f.forge.mu.Lock()
	f.forge.quotaReset = f.clock.Now().Add(time.Hour).Unix()
	f.forge.mu.Unlock()
	f.settle()
	f.clock.advance(time.Minute)
	f.settle()
	f.settle()

	if got := len(logLines(f.logs, "level=WARN", "discovery paused:")); got < 1 || got > 2 {
		t.Fatalf("pause warnings=%d", got)
	}
	resumed := logLines(f.logs, "level=INFO", "discovery resumed after")
	if len(resumed) != 1 || !strings.Contains(resumed[0], "paused_for_seconds=") {
		t.Fatalf("resume lines=%v\n%s", resumed, f.logs)
	}
	if got := logLines(f.logs, "level=INFO", "heartbeat woken by"); len(got) != 0 {
		t.Fatalf("heartbeat sentences must be DEBUG: %v", got)
	}
}

// AC-006: one INFO line per owner comment naming issue, comment and fate, and
// no second line when the comment is seen again.
func TestOwnerCommentFateIsLoggedOnce(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	f.forge.addComment(testIssue, 111, owner(), "/spike first")
	f.settle()
	f.forge.addComment(testIssue, 112, owner(), "/frobnicate")
	f.settle()
	f.settle()
	f.settle()

	dispatched := logLines(f.logs, "level=INFO", "owner comment 111 on ", "dispatched as run ")
	if len(dispatched) != 1 || !strings.Contains(dispatched[0], "#123") || !strings.Contains(dispatched[0], "fate=dispatched") {
		t.Fatalf("dispatch fate lines=%v\n%s", dispatched, f.logs)
	}
	ignored := logLines(f.logs, "level=INFO", "owner comment 112 on ", "ignored")
	if len(ignored) != 1 || !strings.Contains(ignored[0], "fate=ignored") {
		t.Fatalf("ignored fate lines=%v\n%s", ignored, f.logs)
	}
	if got := len(logLines(f.logs, "level=INFO", "owner comment ")); got != 2 {
		t.Fatalf("fate lines=%d, want one per comment", got)
	}
}

// AC-007: after ten minutes one INFO digest, unhealthy when a heartbeat failed.
func TestDigestEveryTenMinutes(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	f.forge.addComment(testIssue, 111, owner(), "/spike first")
	f.settle()
	if got := logLines(f.logs, "daemon digest"); len(got) != 0 {
		t.Fatalf("digest before the window ended: %v", got)
	}
	f.clock.advance(11 * time.Minute)
	f.settle()
	digests := logLines(f.logs, "level=INFO", "daemon digest")
	if len(digests) != 1 {
		t.Fatalf("digests=%v\n%s", digests, f.logs)
	}
	for _, want := range []string{"healthy", "heartbeats_webhook_wakes=0", "heartbeats_held_back=", "heartbeats_failed=0",
		"github_polls=", "new_comments=1", "runs_dispatched=", "runs_in_flight=", "requests=", "requests_ok=",
		"requests_not_modified=", "requests_errors=", "quota_left=", "quota_limit=", "quota_resets_at="} {
		if !strings.Contains(digests[0], want) {
			t.Errorf("digest lacks %q: %s", want, digests[0])
		}
	}
	if strings.Contains(digests[0], "unhealthy") {
		t.Errorf("digest unhealthy on a good window: %s", digests[0])
	}
}

// AC-007: a failed heartbeat makes the window unhealthy.
func TestDigestIsUnhealthyAfterAFailedHeartbeat(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	issuesBefore, _, _, _ := f.forge.counts()
	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != issuesBefore+1 {
			return false
		}
		writeRateLimit(w, 10*time.Second)
		return true
	}
	if _, err := f.tickErr(); err == nil {
		t.Fatal("expected a failed heartbeat")
	}
	f.clock.advance(11 * time.Minute)
	f.settle()
	digests := logLines(f.logs, "level=INFO", "daemon digest")
	if len(digests) != 1 || !strings.Contains(digests[0], "unhealthy") || !strings.Contains(digests[0], "heartbeats_failed=1") {
		t.Fatalf("digest=%v\n%s", digests, f.logs)
	}
}

// AC-009: the published snapshot carries the heartbeat, pause and in-flight
// facts, and a pause is published at once.
func TestHealthSnapshotCarriesHeartbeatAndPause(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	snap, err := f.db.ReadHealthSnapshot(t.Context())
	if err != nil || !snap.Found {
		t.Fatalf("snapshot=%+v err=%v", snap, err)
	}
	if snap.Listener.LastHeartbeatStartedAt.IsZero() || snap.Listener.DiscoveryPaused || snap.Listener.RunsInFlight != 0 {
		t.Fatalf("running snapshot=%+v", snap.Listener)
	}

	f.issue("Add a /health endpoint")
	issuesBefore, _, _, _ := f.forge.counts()
	f.forge.onListIssues = func(w http.ResponseWriter, call int) bool {
		if call != issuesBefore+1 {
			return false
		}
		writeRateLimit(w, 30*time.Second)
		return true
	}
	if _, err := f.tickErr(); err == nil {
		t.Fatal("expected a failed heartbeat")
	}
	snap, err = f.db.ReadHealthSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	l := snap.Listener
	if !l.DiscoveryPaused || l.PauseReason != "rate-limit backoff" || l.PausedSince.IsZero() || l.ExpectedResumeAt.IsZero() {
		t.Fatalf("paused snapshot=%+v", l)
	}
}
