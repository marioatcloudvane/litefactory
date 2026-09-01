package daemon_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
)

// infoOnly swaps the fixture's daemon for one whose logger is held to
// slog.LevelInfo and writes JSON — the level litefactory runs at by default,
// with log_level unset. It returns the buffer that logger writes into.
//
// This is deliberately not the fixture's own [fixture.logs]: that one is a
// text handler held open at Debug so every other test in this package can
// grep for a line regardless of its level, which is exactly the property that
// would make it useless here. A line that only a Debug-level handler would
// show is a line the owner's own daemon — running with nothing set — would
// never write, and that is the distinction these tests exist to catch.
func (f *fixture) infoOnly() *bytes.Buffer {
	f.t.Helper()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	f.daemon = f.newDaemon(daemon.Options{Logger: logger})
	return &logs
}

// TestIssueBodyRefusalsWorthFindingAreLoggedAtInfo is AC-1.1, AC-1.3, AC-1.4
// and AC-1.6.
//
// These three refusals all follow the owner having pointed litefactory at an
// issue by assigning it to himself, so each is the only trace of that
// decision he can find without switching on debug logging: an empty body, a
// body that parses to nothing to act on, and a body that names a command that
// spawns nothing. The level is asserted, not just the text — a line visible
// only under debug is not a line that answers "why did nothing happen?".
func TestIssueBodyRefusalsWorthFindingAreLoggedAtInfo(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty body",
			body: "",
			want: "not running the issue body; its body is empty",
		},
		{
			name: "whitespace-only body",
			body: "  \n\t ",
			want: "not running the issue body; its body is empty",
		},
		{
			// The whole body is the "lf: ask" tag: it grants permission but gives
			// no instruction, so it parses to KindNone same as an empty body would.
			name: "body parses to nothing to act on",
			body: "lf: ask",
			want: "the issue body has nothing to act on; nothing runs",
		},
		{
			name: "command that spawns nothing",
			body: "/frobnicate",
			want: "the issue body names a command that spawns nothing; " +
				"nothing runs and nothing is posted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			logs := f.infoOnly()
			f.plantCursor()
			f.assignedIssue("Add a /health endpoint", tc.body)

			f.settle()

			logged := logs.String()
			if !strings.Contains(logged, `"level":"INFO"`) {
				t.Errorf("no INFO-level line was logged:\n%s", logged)
			}
			if !strings.Contains(logged, tc.want) {
				t.Errorf("logged lines do not contain %q:\n%s", tc.want, logged)
			}
			if got := strings.Count(logged, tc.want); got != 1 {
				t.Errorf("logged %d lines containing %q, want exactly 1:\n%s", got, tc.want, logged)
			}

			posted, reacted, _ := f.forge.snapshot()
			if len(posted) != 0 {
				t.Errorf("posted %d comments on a refusal, want none: %q", len(posted), posted)
			}
			if len(reacted) != 0 {
				t.Errorf("added %d reactions on a refusal, want none", len(reacted))
			}
		})
	}
}

// TestIssueBodyAlreadyHasCommentsRefusalIsLoggedAtInfo is AC-1.2. An issue
// assigned to the owner whose body is refused because it already has comments
// leaves exactly one INFO line naming that reason — the conversation is the
// instruction now, not the opening post.
func TestIssueBodyAlreadyHasCommentsRefusalIsLoggedAtInfo(t *testing.T) {
	f := newFixture(t)
	logs := f.infoOnly()
	f.plantCursor()
	f.assignedIssue("Add a /health endpoint", "/spike add a health endpoint")
	f.forge.addComment(testIssue, 220, stranger(), "is anyone looking at this?")

	f.settle()

	logged := logs.String()
	want := "not running the issue body; " +
		"it already has comments, which are the conversation to follow instead"
	if !strings.Contains(logged, `"level":"INFO"`) {
		t.Errorf("no INFO-level line was logged:\n%s", logged)
	}
	if !strings.Contains(logged, want) {
		t.Errorf("logged lines do not contain %q:\n%s", want, logged)
	}
	if got := strings.Count(logged, want); got != 1 {
		t.Errorf("logged %d lines containing %q, want exactly 1:\n%s", got, want, logged)
	}

	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 0 {
		t.Errorf("posted %d comments on a refusal, want none: %q", len(posted), posted)
	}
	if len(reacted) != 0 {
		t.Errorf("added %d reactions on a refusal, want none", len(reacted))
	}
}

// TestIssueBodyNotAssignedRefusalStaysAtDebug is AC-1.5, and it is the one
// test in this file that pins the trade the other three exist to make safe:
// the "not assigned to the owner" branch fires for nearly every open issue in
// nearly every registered repository on nearly every tick, and promoting it
// by accident would defeat the whole point of promoting the other three —
// the owner's log would go back to being too noisy to find anything in.
func TestIssueBodyNotAssignedRefusalStaysAtDebug(t *testing.T) {
	want := "not running the issue body; it is not assigned to the owner"

	t.Run("the existing debug line is unchanged", func(t *testing.T) {
		f := newFixture(t)
		f.plantCursor()
		f.issue("Add a /health endpoint") // opened by a stranger, assigned to nobody

		f.settle()

		if !strings.Contains(f.logs.String(), want) {
			t.Errorf("the DEBUG line %q was not logged at all:\n%s", want, f.logs)
		}

		posted, reacted, _ := f.forge.snapshot()
		if len(posted) != 0 {
			t.Errorf("posted %d comments on a refusal, want none: %q", len(posted), posted)
		}
		if len(reacted) != 0 {
			t.Errorf("added %d reactions on a refusal, want none", len(reacted))
		}
	})

	t.Run("no INFO line is emitted", func(t *testing.T) {
		f := newFixture(t)
		logs := f.infoOnly()
		f.plantCursor()
		f.issue("Add a /health endpoint")

		f.settle()

		if logged := logs.String(); strings.Contains(logged, "not running the issue body") {
			t.Errorf("the not-assigned refusal was logged at INFO, want it to stay at DEBUG:\n%s", logged)
		}
	})
}
