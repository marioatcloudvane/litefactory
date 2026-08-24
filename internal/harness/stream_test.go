package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const (
	initLine = `{"type":"system","subtype":"init","cwd":"/w","session_id":"1a17a10a-0000-5000-8000-000000000001",` +
		`"tools":["Read","Edit"],"model":"claude-sonnet-4-6","permissionMode":"default","apiKeySource":"none"}`
	resultLine = `{"type":"result","subtype":"success","is_error":false,"duration_ms":1673,"num_turns":1,` +
		`"result":"DONE","total_cost_usd":0.04622315}`
)

func consumeLines(t *testing.T, lines ...string) (*eventStream, *bytes.Buffer) {
	t.Helper()
	var sink bytes.Buffer
	stream := &eventStream{sink: &sink, logger: discardLogger()}
	if err := stream.consume(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatalf("consume: %v", err)
	}
	return stream, &sink
}

// AC-3.17: model and tool surface come from the init event, result text and
// cost from the terminal result event. The field names are taken from a real
// captured run, not from documentation.
func TestEventStreamExtractsInitAndResult(t *testing.T) {
	stream, _ := consumeLines(t, initLine, resultLine)

	if stream.model != "claude-sonnet-4-6" {
		t.Errorf("model = %q", stream.model)
	}
	if !slices.Equal(stream.tools, []string{"Read", "Edit"}) {
		t.Errorf("tools = %q", stream.tools)
	}
	if stream.sessionID != "1a17a10a-0000-5000-8000-000000000001" {
		t.Errorf("session id = %q", stream.sessionID)
	}
	if stream.result == nil {
		t.Fatal("no terminal result event was captured")
	}
	if stream.result.Result != "DONE" {
		t.Errorf("result text = %q", stream.result.Result)
	}
	if stream.result.TotalCostUSD != 0.04622315 {
		t.Errorf("cost = %v", stream.result.TotalCostUSD)
	}
	if stream.result.NumTurns != 1 {
		t.Errorf("turns = %d", stream.result.NumTurns)
	}
	if stream.events != 2 {
		t.Errorf("events = %d, want 2", stream.events)
	}
}

// SD-6, AC-3.16. bufio.Scanner does not merely truncate a line over its token
// limit: it returns ErrTooLong and aborts the loop, so the terminal result
// event that follows is lost. Lines of 72,411 bytes were measured in the spike.
func TestEventStreamReadsLinesLargerThan64KB(t *testing.T) {
	huge, err := json.Marshal(map[string]any{
		"type":    "user",
		"content": strings.Repeat("x", 200_000),
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, sink := consumeLines(t, initLine, string(huge), resultLine)

	if stream.result == nil {
		t.Fatal("the terminal result event was lost behind an over-long line — this is the Scanner bug")
	}
	if stream.events != 3 {
		t.Errorf("events = %d, want 3", stream.events)
	}
	if stream.longest != len(huge)+1 {
		t.Errorf("longest line = %d bytes, want %d", stream.longest, len(huge)+1)
	}
	if !bytes.Contains(sink.Bytes(), huge) {
		t.Error("the over-long line was not written to the run log intact")
	}

	// And the log is still one event per line.
	if got := len(bytes.Split(bytes.TrimRight(sink.Bytes(), "\n"), []byte("\n"))); got != 3 {
		t.Errorf("run log has %d lines, want 3", got)
	}
}

// The concrete regression: the same input through a default bufio.Scanner
// loses the result event. Kept as a test so that "just use a Scanner with a
// bigger buffer" is refuted in the repository rather than in a review comment.
func TestScannerWouldHaveLostTheResultEvent(t *testing.T) {
	huge := `{"type":"user","content":"` + strings.Repeat("x", 200_000) + `"}`
	input := strings.Join([]string{initLine, huge, resultLine}, "\n") + "\n"

	scanner := bufio.NewScanner(strings.NewReader(input))
	var seen int
	for scanner.Scan() {
		seen++
	}
	if scanner.Err() == nil {
		t.Skip("bufio.Scanner no longer errors on this input; the reason for ReadBytes has changed")
	}
	if seen > 1 {
		t.Fatalf("Scanner read %d lines before aborting; expected it to stop at the over-long one", seen)
	}
}

// A stray diagnostic on stdout must not cost us the events around it. It has
// already been persisted verbatim; it is counted and skipped.
func TestEventStreamCountsMalformedLinesAndKeepsGoing(t *testing.T) {
	stream, sink := consumeLines(t, initLine, "npm WARN something happened", resultLine)

	if stream.malformed != 1 {
		t.Errorf("malformed = %d, want 1", stream.malformed)
	}
	if stream.events != 2 {
		t.Errorf("events = %d, want 2", stream.events)
	}
	if stream.result == nil {
		t.Fatal("a non-JSON line lost the terminal result event")
	}
	if !strings.Contains(sink.String(), "npm WARN something happened") {
		t.Error("the unparseable line was not persisted verbatim")
	}
}

// SD-3. .subtype reads "success" even on is_error: true runs — measured on
// every "Not logged in" capture from the spike. Nothing may branch on it.
func TestEventStreamKeepsIsErrorIndependentOfSubtype(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login",` +
		`"total_cost_usd":0,"num_turns":0}`
	stream, _ := consumeLines(t, initLine, line)

	if stream.result == nil {
		t.Fatal("no result event captured")
	}
	if !stream.result.IsError {
		t.Fatal("is_error was not read; a failed run would be reported as a success")
	}
}

func TestEventStreamAddsAMissingFinalNewline(t *testing.T) {
	var sink bytes.Buffer
	stream := &eventStream{sink: &sink, logger: discardLogger()}
	// No trailing newline: what a process killed mid-write leaves behind.
	if err := stream.consume(strings.NewReader(initLine)); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !strings.HasSuffix(sink.String(), "}\n") {
		t.Errorf("run log does not end in a newline: %q", sink.String()[max(0, sink.Len()-20):])
	}
	if stream.events != 1 {
		t.Errorf("events = %d, want 1", stream.events)
	}
}

// A failure to write the run log must not stop the read loop: the pipe still
// has to be drained to EOF or the child blocks on a full buffer and Wait never
// returns. The write failure is reported separately.
func TestEventStreamDrainsDespiteAWriteFailure(t *testing.T) {
	stream := &eventStream{sink: failingWriter{}, logger: discardLogger()}
	if err := stream.consume(strings.NewReader(initLine + "\n" + resultLine + "\n")); err != nil {
		t.Fatalf("consume returned a read error for a write failure: %v", err)
	}
	if stream.result == nil {
		t.Fatal("the loop stopped at the write failure and lost the result event")
	}
	if stream.err() == nil {
		t.Fatal("the write failure was swallowed")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTailWriterKeepsTheTail(t *testing.T) {
	w := &tailWriter{limit: 16}

	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if got := w.String(); got != "0123456789" {
		t.Fatalf("short write mangled: %q", got)
	}

	if _, err := w.Write([]byte("abcdefghij")); err != nil {
		t.Fatal(err)
	}
	got := w.String()
	if !strings.HasSuffix(got, "abcdefghij") {
		t.Errorf("tail lost the most recent bytes: %q", got)
	}
	if !strings.HasPrefix(got, "[earlier stderr dropped]") {
		t.Errorf("a truncated tail is not marked as truncated: %q", got)
	}
}

func TestTailWriterHandlesAWriteLargerThanTheLimit(t *testing.T) {
	w := &tailWriter{limit: 8}
	n, err := w.Write([]byte(strings.Repeat("z", 1000) + "END12345"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1008 {
		t.Errorf("Write reported %d bytes, want 1008; a short count looks like an error to io.Copy", n)
	}
	if !strings.HasSuffix(w.String(), "END12345") {
		t.Errorf("tail = %q", w.String())
	}
}
