package cli

import (
	"bytes"
	"context"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// runFollowLogs orders filesystem changes after observed output and joins the
// writer before reading its result. The deadline detects a hung follower.
func runFollowLogs(t *testing.T, steps func(context.Context, *recordingWriter) error, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cancel()
	var out recordingWriter
	var errOut bytes.Buffer
	done := make(chan error, 1)
	go func() { defer cancel(); done <- steps(ctx, &out) }()
	env := &Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler)}
	code = NewRouter("litefactory", env, Commands()).Run(ctx, args)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return code, out.joined(), errOut.String()
}

// followOpenContext observes the follower's first wait. In a direct call with
// an absent file, Done is consulted only after the failed open, before polling.
type followOpenContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *followOpenContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// TestFollowAppLog_StreamsAppendedRecord is AC-2.1: a record appended to
// daemon.log after the follow session has already started is observed within
// a poll interval, and the command exits 0 once its context is cancelled.
func TestFollowAppLog_StreamsAppendedRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	logPath := filepath.Join(logsDir, "daemon.log")
	const line1 = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
	if err := os.WriteFile(logPath, []byte(line1), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}
	const line2 = `{"time":"2026-09-05T09:00:01Z","level":"INFO","msg":"tick"}` + "\n"

	code, stdout, stderr := runFollowLogs(t, func(ctx context.Context, out *recordingWriter) error {
		if err := out.waitForWrites(ctx, 1); err != nil {
			return err
		}
		if err := appendToFile(logPath, line2); err != nil {
			return err
		}
		return out.waitForWrites(ctx, 2)
	}, "logs", "--raw", "-f")

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, line1) {
		t.Errorf("stdout = %q, want it to contain the initial line %q", stdout, line1)
	}
	if !strings.Contains(stdout, line2) {
		t.Errorf("stdout = %q, want it to contain the appended line %q", stdout, line2)
	}
}

// TestFollowAppLog_TerminalEventDoesNotStopFollow is AC-2.2: a record that
// looks like a run log's terminal result event does not stop the app log's
// follow loop, unlike an issue's run log sink which does stop on it. A line
// appended after the terminal-looking one must still be observed, which
// proves the loop kept polling past it.
func TestFollowAppLog_TerminalEventDoesNotStopFollow(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	logPath := filepath.Join(logsDir, "daemon.log")
	const line1 = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
	if err := os.WriteFile(logPath, []byte(line1), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}
	// terminalLike carries the exact marker isTerminalEvent looks for on an
	// issue's run log (a top-level "type":"result" field) so this test
	// actually exercises the contrast the AC describes, not just a line that
	// happens to mention the word "result".
	const terminalLike = `{"type":"result","subtype":"success"}` + "\n"
	const afterTerminal = `{"time":"2026-09-05T09:00:02Z","level":"INFO","msg":"still running"}` + "\n"

	code, stdout, stderr := runFollowLogs(t, func(ctx context.Context, out *recordingWriter) error {
		if err := out.waitForWrites(ctx, 1); err != nil {
			return err
		}
		if err := appendToFile(logPath, terminalLike); err != nil {
			return err
		}
		if err := out.waitForWrites(ctx, 2); err != nil {
			return err
		}
		if err := appendToFile(logPath, afterTerminal); err != nil {
			return err
		}
		return out.waitForWrites(ctx, 3)
	}, "logs", "--raw", "-f")

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, terminalLike) {
		t.Errorf("stdout = %q, want it to contain the terminal-looking line %q", stdout, terminalLike)
	}
	if !strings.Contains(stdout, afterTerminal) {
		t.Errorf("stdout = %q, want it to contain the line written after the terminal-looking one, "+
			"proving the follow loop did not stop on it", stdout)
	}
}

// TestFollowAppLog_WaitsForFileThenStreams is AC-2.3: -f against a daemon.log
// that does not exist yet waits rather than failing, then starts streaming
// once the file is created and written to.
func TestFollowAppLog_WaitsForFileThenStreams(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	logPath := filepath.Join(logsDir, "daemon.log")
	const line1 = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
	const line2 = `{"time":"2026-09-05T09:00:01Z","level":"INFO","msg":"tick"}` + "\n"

	if _, err := os.Stat(logPath); err == nil {
		t.Fatal("daemon.log unexpectedly exists before the test wrote it")
	}

	ctx, cancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cancel()
	observed := &followOpenContext{Context: ctx, waiting: make(chan struct{})}
	var out recordingWriter
	done := make(chan error, 1)
	go func() { done <- followAppLog(observed, &rawSink{out: &out}, logPath, 0) }()
	select {
	case <-observed.waiting:
	case <-ctx.Done():
		t.Fatal("follower did not attempt absent open")
	}
	if err := os.WriteFile(logPath, []byte(line1+line2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := out.waitForWrites(ctx, 2); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stdout := out.joined()

	if !strings.Contains(stdout, line1) || !strings.Contains(stdout, line2) {
		t.Errorf("stdout = %q, want it to contain both lines written after the file was created", stdout)
	}
}

// TestFollowAppLog_ResumesAfterTruncation is AC-2.4: when daemon.log is
// truncated beneath a follow session (rotated or cleared out from under the
// reader), the follow loop detects the shrink via stat, reopens at offset 0
// on the next tick, and keeps streaming new content rather than erroring or
// stalling on a stale offset.
func TestFollowAppLog_ResumesAfterTruncation(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	logPath := filepath.Join(logsDir, "daemon.log")
	const beforeTruncate = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"before truncate"}` + "\n"
	const afterTruncate = `{"time":"2026-09-05T09:00:02Z","level":"INFO","msg":"after truncate"}` + "\n"
	if err := os.WriteFile(logPath, []byte(beforeTruncate), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	code, stdout, stderr := runFollowLogs(t, func(ctx context.Context, out *recordingWriter) error {
		if err := out.waitForWrites(ctx, 1); err != nil {
			return err
		}
		if err := os.WriteFile(logPath, []byte(afterTruncate), 0o600); err != nil {
			return err
		}
		return out.waitForWrites(ctx, 2)
	}, "logs", "--raw", "-f")

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, beforeTruncate) {
		t.Errorf("stdout = %q, want it to contain the pre-truncation line", stdout)
	}
	if !strings.Contains(stdout, afterTruncate) {
		t.Errorf("stdout = %q, want it to contain the line written after truncation, "+
			"proving the follow loop reopened at offset 0 instead of stalling", stdout)
	}
}
