package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// newSlogGoldenHandlers builds a JSON handler and a Text handler configured
// exactly like internal/logging.New configures its handlers: only Level is
// set, neither AddSource nor ReplaceAttr. renderSlogLine's whole premise
// (see the doc comment atop cmd_logs_slog.go) depends on that; if this ever
// drifts from logging.go, these golden tests are the ones that should catch
// it.
func newSlogGoldenHandlers() (jsonBuf, textBuf *bytes.Buffer, jsonH, textH slog.Handler) {
	jsonBuf = &bytes.Buffer{}
	textBuf = &bytes.Buffer{}
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	jsonH = slog.NewJSONHandler(jsonBuf, opts)
	textH = slog.NewTextHandler(textBuf, opts)
	return jsonBuf, textBuf, jsonH, textH
}

// handleBoth feeds the same record to both handlers, so the JSON side is
// exactly what daemon.log would hold and the text side is exactly what the
// console would have shown for the same call.
func handleBoth(t *testing.T, jsonH, textH slog.Handler, r slog.Record) {
	t.Helper()

	if err := jsonH.Handle(context.Background(), r); err != nil {
		t.Fatalf("json handler.Handle: %v", err)
	}
	if err := textH.Handle(context.Background(), r); err != nil {
		t.Fatalf("text handler.Handle: %v", err)
	}
}

// lastLine returns the last non-empty newline-terminated line written to buf,
// with its trailing newline stripped.
func lastLine(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("buffer has no lines")
	}
	return lines[len(lines)-1]
}

// TestRenderSlogLine_GoldenMatchesTextHandler is the AC-3.1 golden test:
// records built from typed attrs (string, int, bool, error) across every
// level are logged through a JSON handler (the daemon.log shape) and a text
// handler (what daemon run's console would have printed) side by side, and
// renderSlogLine's output on the JSON line must be byte-identical to the
// text handler's line for the same record.
//
// time.Duration is deliberately not included in this byte-identical
// comparison: slog's own JSONHandler encodes a Duration attr as a bare JSON
// number (nanoseconds), throwing away the information that let TextHandler
// print "1.5s", so no renderer working from JSON alone can reconstruct that
// form. This is documented in cmd_logs_slog.go's package doc as an accepted,
// inherent limitation, not a defect; TestRenderSlogLine_DurationAttr below
// exercises it directly instead of asserting an impossible byte match here.
func TestRenderSlogLine_GoldenMatchesTextHandler(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		msg   string
		attrs []slog.Attr
	}{
		{
			name:  "info with string and int attrs",
			level: slog.LevelInfo,
			msg:   "started",
			attrs: []slog.Attr{slog.String("name", "worker-1"), slog.Int("count", 3)},
		},
		{
			name:  "debug with bool attr",
			level: slog.LevelDebug,
			msg:   "flag check",
			attrs: []slog.Attr{slog.Bool("enabled", true), slog.Bool("dryRun", false)},
		},
		{
			name:  "warn with error attr",
			level: slog.LevelWarn,
			msg:   "retrying",
			attrs: []slog.Attr{slog.Any("err", errors.New("connection reset")), slog.Int("attempt", 2)},
		},
		{
			name:  "error with mixed attrs",
			level: slog.LevelError,
			msg:   "request failed",
			attrs: []slog.Attr{
				slog.String("method", "GET"),
				slog.Int("status", 500),
				slog.Bool("retryable", true),
				slog.Any("err", errors.New("boom")),
			},
		},
		{
			name:  "no attrs",
			level: slog.LevelInfo,
			msg:   "heartbeat",
			attrs: nil,
		},
		{
			name:  "string needing quoting",
			level: slog.LevelInfo,
			msg:   "message with spaces",
			attrs: []slog.Attr{slog.String("path", "/tmp/has spaces/file.txt"), slog.String("empty", "")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			jsonBuf, textBuf, jsonH, textH := newSlogGoldenHandlers()
			r := slog.NewRecord(time.Now(), tt.level, tt.msg, 0)
			r.AddAttrs(tt.attrs...)

			handleBoth(t, jsonH, textH, r)
			gotRendered, ok := renderSlogLine([]byte(lastLine(t, jsonBuf)))

			if !ok {
				t.Fatalf("renderSlogLine reported ok=false for a valid JSON object line")
			}
			wantText := lastLine(t, textBuf)
			if gotRendered != wantText {
				t.Errorf("renderSlogLine output mismatch:\n got:  %q\n want: %q", gotRendered, wantText)
			}
		})
	}
}

// TestRenderSlogLine_DurationAttr documents and locks in the accepted
// limitation called out in cmd_logs_slog.go: a time.Duration attr cannot
// round-trip through JSON back to its TextHandler string form ("1.5s")
// because JSONHandler itself only ever wrote the bare nanosecond count. The
// renderer must still render *something* sane (the plain integer) rather
// than drop the line or error, and the two handlers' outputs for the
// duration field are expected to differ.
func TestRenderSlogLine_DurationAttr(t *testing.T) {
	jsonBuf, textBuf, jsonH, textH := newSlogGoldenHandlers()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "timed out", 0)
	r.AddAttrs(slog.Duration("timeout", 1500*time.Millisecond))

	handleBoth(t, jsonH, textH, r)
	gotRendered, ok := renderSlogLine([]byte(lastLine(t, jsonBuf)))

	if !ok {
		t.Fatalf("renderSlogLine reported ok=false for a valid JSON object line")
	}
	wantText := lastLine(t, textBuf)
	if !strings.Contains(wantText, "timeout=1.5s") {
		t.Fatalf("test assumption broken: text handler line %q does not contain timeout=1.5s", wantText)
	}
	if strings.Contains(gotRendered, "1.5s") {
		t.Fatalf("renderSlogLine unexpectedly reconstructed the Duration string form: %q", gotRendered)
	}
	if !strings.Contains(gotRendered, "timeout=1500000000") {
		t.Errorf("renderSlogLine did not render the raw nanosecond count for the duration attr: %q", gotRendered)
	}
}

// TestRenderSlogLine_WithGroup is AC-3.2: a WithGroup call nests its attrs
// under an object keyed by the group name in the JSON line, and
// renderSlogLine must flatten that back to a dotted key exactly the way
// TextHandler would have rendered the same WithGroup call directly (the
// group's attrs never round-tripped through JSON for TextHandler).
func TestRenderSlogLine_WithGroup(t *testing.T) {
	jsonBuf, textBuf, jsonH, textH := newSlogGoldenHandlers()
	jsonH = jsonH.WithGroup("git")
	textH = textH.WithGroup("git")
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "checkout", 0)
	r.AddAttrs(slog.String("branch", "main"))

	handleBoth(t, jsonH, textH, r)
	gotRendered, ok := renderSlogLine([]byte(lastLine(t, jsonBuf)))

	if !ok {
		t.Fatalf("renderSlogLine reported ok=false for a valid JSON object line")
	}
	if !strings.Contains(gotRendered, "git.branch=main") {
		t.Errorf("renderSlogLine did not flatten the group to git.branch=main: %q", gotRendered)
	}
	wantText := lastLine(t, textBuf)
	if gotRendered != wantText {
		t.Errorf("renderSlogLine output mismatch:\n got:  %q\n want: %q", gotRendered, wantText)
	}
}

// TestRenderSlogLine_PreservesAttributeOrder is AC-3.3: the rendered line's
// attribute order must be the JSON object's key order, not any sorted or
// map-iteration order. A hand-written line is used here so the field order
// is asserted directly against the literal source text, independent of
// whatever order a real slog call happens to produce.
func TestRenderSlogLine_PreservesAttributeOrder(t *testing.T) {
	line := []byte(`{"time":"2024-01-15T10:30:00Z","level":"INFO","msg":"m","zebra":1,"apple":2,"middle":3,"bravo":4,"yankee":5}`)

	got, ok := renderSlogLine(line)

	if !ok {
		t.Fatalf("renderSlogLine reported ok=false for a valid JSON object line")
	}
	wantOrder := []string{"time=", "level=INFO", "msg=m", "zebra=1", "apple=2", "middle=3", "bravo=4", "yankee=5"}
	var gotOrder []int
	for _, want := range wantOrder {
		idx := strings.Index(got, want)
		if idx == -1 {
			t.Fatalf("rendered line %q missing expected token %q", got, want)
		}
		gotOrder = append(gotOrder, idx)
	}
	for i := 1; i < len(gotOrder); i++ {
		if gotOrder[i] < gotOrder[i-1] {
			t.Errorf("attribute order not preserved: token %q appears before %q in %q", wantOrder[i], wantOrder[i-1], got)
		}
	}
}

// TestRenderSlogLine_TimestampMillisReformat is AC-3.4: JSONHandler writes
// time.Time as RFC3339 with nanosecond precision; TextHandler prints the
// same instant truncated to millisecond precision. renderSlogLine must
// reformat the first field's "time" the same way TextHandler would have,
// verified here against a real TextHandler call for the identical instant
// rather than a hand-computed string.
func TestRenderSlogLine_TimestampMillisReformat(t *testing.T) {
	ts, err := time.Parse(time.RFC3339Nano, "2024-01-15T10:30:00.123456789Z")
	if err != nil {
		t.Fatalf("time.Parse: %v", err)
	}
	jsonBuf, textBuf, jsonH, textH := newSlogGoldenHandlers()
	r := slog.NewRecord(ts, slog.LevelInfo, "tick", 0)

	handleBoth(t, jsonH, textH, r)
	gotRendered, ok := renderSlogLine([]byte(lastLine(t, jsonBuf)))

	if !ok {
		t.Fatalf("renderSlogLine reported ok=false for a valid JSON object line")
	}
	wantText := lastLine(t, textBuf)
	if gotRendered != wantText {
		t.Errorf("renderSlogLine output mismatch:\n got:  %q\n want: %q", gotRendered, wantText)
	}
	if !strings.HasPrefix(gotRendered, "time=2024-01-15T10:30:00.123Z") {
		t.Errorf("renderSlogLine did not reformat to millisecond precision: %q", gotRendered)
	}
}

// TestRenderSlogLine_PassesThroughNonObjectLines is AC-3.5: a line that
// isn't valid JSON at all, and a line that is valid JSON but not an object,
// must both come back with ok=false so the caller passes them through
// verbatim instead of dropping them or erroring.
func TestRenderSlogLine_PassesThroughNonObjectLines(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{name: "not json at all", line: "not json at all"},
		{name: "bare number", line: "42"},
		{name: "bare array", line: "[1,2,3]"},
		{name: "bare string", line: `"hello"`},
		{name: "empty line", line: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/reports not-ok", func(t *testing.T) {
			t.Parallel()

			_, ok := renderSlogLine([]byte(tt.line))

			if ok {
				t.Errorf("renderSlogLine reported ok=true for non-object line %q", tt.line)
			}
		})
	}
}

// TestSlogTextSink_Write_PassesThroughUnrenderableLines exercises the full
// sink, not just renderSlogLine, to confirm write() emits an unrenderable
// line exactly as it arrived (AC-3.5) instead of dropping it or returning an
// error, and that a renderable line is written as its rendered form.
func TestSlogTextSink_Write_PassesThroughUnrenderableLines(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantEqual string // exact expected bytes written, empty means "compute from renderSlogLine"
	}{
		{name: "not json at all", line: "not json at all", wantEqual: "not json at all"},
		{name: "bare number", line: "42", wantEqual: "42"},
		{name: "bare array", line: "[1,2,3]", wantEqual: "[1,2,3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			sink := newSlogTextSink(&out, nil)

			done, err := sink.write([]byte(tt.line))

			if err != nil {
				t.Fatalf("write returned error: %v", err)
			}
			if done {
				t.Errorf("write reported done=true; the app log sink never terminates early")
			}
			if out.String() != tt.wantEqual {
				t.Errorf("write output = %q, want %q (verbatim passthrough)", out.String(), tt.wantEqual)
			}
		})
	}
}

// TestSlogTextSink_Write_RendersValidObjectLine confirms the sink writes the
// rendered form (not the raw JSON) for a line that does decode as an object,
// followed by a newline.
func TestSlogTextSink_Write_RendersValidObjectLine(t *testing.T) {
	var out bytes.Buffer
	sink := newSlogTextSink(&out, nil)
	line := []byte(`{"time":"2024-01-15T10:30:00Z","level":"INFO","msg":"hello","name":"world"}`)

	done, err := sink.write(line)

	if err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	if done {
		t.Errorf("write reported done=true; the app log sink never terminates early")
	}
	want := "time=2024-01-15T10:30:00.000Z level=INFO msg=hello name=world\n"
	if out.String() != want {
		t.Errorf("write output = %q, want %q", out.String(), want)
	}
}
