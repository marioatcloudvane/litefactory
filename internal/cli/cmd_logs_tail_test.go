package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bindTailFlagsForTest builds a flag set with logsOptions.bind registered on
// it, parses args against it, and returns the parsed options alongside the
// flag set — callers need the flag set itself to call validateTailFlags,
// which relies on fs.Visit to tell an explicitly passed flag from a default.
func bindTailFlagsForTest(t *testing.T, args ...string) (logsOptions, *flag.FlagSet) {
	t.Helper()
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	var opts logsOptions
	opts.bind(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return opts, fs
}

func TestValidateTailFlagsDefaults(t *testing.T) {
	opts, fs := bindTailFlagsForTest(t)
	if opts.tailN != defaultTailLines {
		t.Fatalf("default -n = %d, want %d", opts.tailN, defaultTailLines)
	}
	cmd := &Command{Name: "logs"}
	if err := validateTailFlags(cmd, fs, opts, false); err != nil {
		t.Fatalf("validateTailFlags with no flags set: %v", err)
	}
}

func TestValidateTailFlagsMutuallyExclusive(t *testing.T) {
	opts, fs := bindTailFlagsForTest(t, "-n", "20", "--all")
	cmd := &Command{Name: "logs"}
	err := validateTailFlags(cmd, fs, opts, false)
	if err == nil {
		t.Fatal("validateTailFlags with -n and --all both set: got nil error, want a usage error")
	}
	if code := ExitCode(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d (%v)", code, ExitUsage, err)
	}
}

func TestValidateTailFlagsNTooSmall(t *testing.T) {
	for _, n := range []string{"0", "-1", "-5"} {
		t.Run(n, func(t *testing.T) {
			opts, fs := bindTailFlagsForTest(t, "-n", n)
			cmd := &Command{Name: "logs"}
			err := validateTailFlags(cmd, fs, opts, false)
			if err == nil {
				t.Fatalf("validateTailFlags with -n %s: got nil error, want a usage error", n)
			}
			if code := ExitCode(err); code != ExitUsage {
				t.Fatalf("exit code = %d, want %d (%v)", code, ExitUsage, err)
			}
		})
	}
}

func TestValidateTailFlagsRejectedOnIssuePath(t *testing.T) {
	cmd := &Command{Name: "logs"}

	optsN, fsN := bindTailFlagsForTest(t, "-n", "20")
	if err := validateTailFlags(cmd, fsN, optsN, true); err == nil {
		t.Fatal("validateTailFlags(-n, hasIssue=true): got nil error, want a usage error")
	} else if !strings.Contains(err.Error(), "-n") {
		t.Fatalf("error %q does not name the offending flag -n", err.Error())
	} else if code := ExitCode(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d (%v)", code, ExitUsage, err)
	}

	optsAll, fsAll := bindTailFlagsForTest(t, "--all")
	if err := validateTailFlags(cmd, fsAll, optsAll, true); err == nil {
		t.Fatal("validateTailFlags(--all, hasIssue=true): got nil error, want a usage error")
	} else if !strings.Contains(err.Error(), "--all") {
		t.Fatalf("error %q does not name the offending flag --all", err.Error())
	} else if code := ExitCode(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d (%v)", code, ExitUsage, err)
	}
}

// writeTailFixture writes lines, each newline-terminated except that a
// final unterminated fragment is appended verbatim when trailing is set, so
// tests can exercise both a normally-flushed log and one caught mid-write.
func writeTailFixture(t *testing.T, dir string, lines []string, trailingNewline bool) string {
	t.Helper()
	path := filepath.Join(dir, "daemon.log")
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(line)
		if i < len(lines)-1 || trailingNewline {
			b.WriteByte('\n')
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// readFrom reads the file at path starting at offset and returns its lines.
func readFrom(t *testing.T, path string, offset int64) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rest := strings.TrimSuffix(string(data[offset:]), "\n")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "\n")
}

func makeLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	return lines
}

func TestTailOffsetDefaultShowsLast200(t *testing.T) {
	dir := t.TempDir()
	path := writeTailFixture(t, dir, makeLines(500), true)

	offset, err := tailOffset(path, defaultTailLines)
	if err != nil {
		t.Fatalf("tailOffset: %v", err)
	}
	got := readFrom(t, path, offset)
	if len(got) != defaultTailLines {
		t.Fatalf("got %d lines, want %d", len(got), defaultTailLines)
	}
	if got[0] != "line 300" || got[len(got)-1] != "line 499" {
		t.Fatalf("tail window = [%q .. %q], want [%q .. %q]",
			got[0], got[len(got)-1], "line 300", "line 499")
	}
}

func TestTailOffsetNSelectsLastN(t *testing.T) {
	dir := t.TempDir()
	path := writeTailFixture(t, dir, makeLines(100), true)

	offset, err := tailOffset(path, 20)
	if err != nil {
		t.Fatalf("tailOffset: %v", err)
	}
	got := readFrom(t, path, offset)
	if len(got) != 20 {
		t.Fatalf("got %d lines, want 20", len(got))
	}
	if got[0] != "line 80" || got[len(got)-1] != "line 99" {
		t.Fatalf("tail window = [%q .. %q], want [%q .. %q]", got[0], got[len(got)-1], "line 80", "line 99")
	}
}

func TestAppLogTailOffsetAllShowsEverything(t *testing.T) {
	dir := t.TempDir()
	path := writeTailFixture(t, dir, makeLines(500), true)

	offset, err := appLogTailOffset(path, logsOptions{all: true, tailN: 20})
	if err != nil {
		t.Fatalf("appLogTailOffset: %v", err)
	}
	if offset != 0 {
		t.Fatalf("--all offset = %d, want 0", offset)
	}
	got := readFrom(t, path, offset)
	if len(got) != 500 {
		t.Fatalf("got %d lines, want 500", len(got))
	}
}

func TestTailOffsetShortFileStartsAtZero(t *testing.T) {
	dir := t.TempDir()
	path := writeTailFixture(t, dir, makeLines(5), true)

	offset, err := tailOffset(path, defaultTailLines)
	if err != nil {
		t.Fatalf("tailOffset: %v", err)
	}
	if offset != 0 {
		t.Fatalf("short file offset = %d, want 0", offset)
	}
	got := readFrom(t, path, offset)
	if len(got) != 5 {
		t.Fatalf("got %d lines, want 5", len(got))
	}
}

func TestTailOffsetMissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.log")

	offset, err := tailOffset(path, defaultTailLines)
	if err != nil {
		t.Fatalf("tailOffset on missing file: %v", err)
	}
	if offset != 0 {
		t.Fatalf("missing file offset = %d, want 0", offset)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("tailOffset created the missing file as a side effect")
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("fixture directory disappeared: %v", statErr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("tailOffset created files as a side effect: %v", entries)
	}
}

func TestTailOffsetUnterminatedLastLine(t *testing.T) {
	dir := t.TempDir()
	// The last line has no trailing newline, as if the writer stopped
	// mid-record.
	path := writeTailFixture(t, dir, makeLines(10), false)

	offset, err := tailOffset(path, 3)
	if err != nil {
		t.Fatalf("tailOffset: %v", err)
	}
	got := readFromRaw(t, path, offset)
	want := []string{"line 7", "line 8", "line 9"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// readFromRaw is like readFrom but does not assume a trailing newline —
// needed for the unterminated-last-line fixture.
func readFromRaw(t *testing.T, path string, offset int64) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rest := string(data[offset:])
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "\n")
}
