package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// Reading claude's event stream (SD-6, AC-3.16, AC-3.17).
//
// Two rules, both learned the expensive way:
//
//  1. bufio.Scanner must not be used. Over its token limit it does not truncate
//     the line, it returns ErrTooLong and stops the loop — so an unchecked
//     s.Err() turns "one long tool result" into "the run had no result event".
//     Lines of 72,411 bytes were measured from an ordinary file read.
//     bufio.Reader.ReadBytes has no such limit.
//  2. Every line is written to NNNN.jsonl as it arrives, with no buffering
//     layer of our own, so a crash — of claude, of the daemon, of the machine —
//     leaves the events that had already happened on disk.

// readerBufferSize is the starting size of the stdout buffer. It is not a
// limit: ReadBytes grows past it as needed. It is sized so that the common
// line, a few kilobytes, never needs a second read.
const readerBufferSize = 64 << 10

// Event type and subtype values from the stream-json protocol, confirmed
// against a captured run rather than documentation.
const (
	eventTypeSystem = "system"
	eventTypeResult = "result"
	subtypeInit     = "init"
)

// eventEnvelope is enough of any event to route it. Every line is decoded into
// this first, and only the two interesting kinds are decoded again in full.
type eventEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// initEvent is the first event of a run. It reports what claude actually
// resolved, which is not necessarily what was asked for: .tools reflects
// --tools (SD-2), and .session_id echoes --session-id.
type initEvent struct {
	Model          string   `json:"model"`
	Tools          []string `json:"tools"`
	SessionID      string   `json:"session_id"`
	CWD            string   `json:"cwd"`
	PermissionMode string   `json:"permissionMode"`
	APIKeySource   string   `json:"apiKeySource"`
}

// resultEvent is the terminal event.
//
// Note what is absent: .subtype. It reads "success" even when .is_error is
// true, so branching on it silently reports failed runs as successful (SD-3).
// The only trustworthy field is .is_error.
type resultEvent struct {
	SessionID    string          `json:"session_id"`
	IsError      bool            `json:"is_error"`
	Result       string          `json:"result"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	HasCost      bool            `json:"-"`
	HasNumTurns  bool            `json:"-"`
	Usage        json.RawMessage `json:"usage"`
	ModelUsage   json.RawMessage `json:"modelUsage"`
	DurationMS   int64           `json:"duration_ms"`
	NumTurns     int             `json:"num_turns"`
}

// UnmarshalJSON preserves affirmative zero separately from absent or null facts.
func (e *resultEvent) UnmarshalJSON(data []byte) error {
	type plain resultEvent
	var decoded struct {
		*plain
		Cost  *float64 `json:"total_cost_usd"`
		Turns *int     `json:"num_turns"`
	}
	*e = resultEvent{}
	decoded.plain = (*plain)(e)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Cost != nil {
		e.HasCost = true
		e.TotalCostUSD = *decoded.Cost
	}
	if decoded.Turns != nil {
		e.HasNumTurns = true
		e.NumTurns = *decoded.Turns
	}
	return nil
}

// eventStream consumes claude's stdout: it persists every line verbatim and
// extracts the handful of fields a run report needs.
type eventStream struct {
	sink   io.Writer
	logger *slog.Logger

	events    int
	malformed int
	longest   int

	model     string
	tools     []string
	sessionID string
	result    *resultEvent
	quota     quotaEvidence

	writeErr error
	usage    *usageCollector
}

// consume reads r to EOF.
//
// It returns only read failures. A failure to write the run log does not stop
// the loop: the pipe still has to be drained to EOF or the child blocks on a
// full pipe buffer and cmd.Wait never returns. The write failure is reported
// separately by [eventStream.err].
func (s *eventStream) consume(r io.Reader) error {
	reader := bufio.NewReaderSize(r, readerBufferSize)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s.record(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read claude stdout: %w", err)
		}
	}
}

// record persists one raw line and then interprets it.
func (s *eventStream) record(line []byte) {
	if len(line) > s.longest {
		s.longest = len(line)
	}
	s.write(line)
	s.usage.observe(line)
	s.observe(line)
}

// write appends the line to the run log exactly as claude emitted it. A final
// line without a newline — which happens when the process is killed mid-write —
// gets one, so the file stays parseable line by line.
func (s *eventStream) write(line []byte) {
	if s.sink == nil || s.writeErr != nil {
		return
	}
	if _, err := s.sink.Write(line); err != nil {
		s.writeErr = fmt.Errorf("write run log: %w", err)
		return
	}
	if line[len(line)-1] != '\n' {
		if _, err := s.sink.Write([]byte{'\n'}); err != nil {
			s.writeErr = fmt.Errorf("write run log: %w", err)
		}
	}
}

// observe extracts the fields the run report needs from one event.
//
// A line that is not JSON is counted and skipped rather than failing the run:
// it has already been persisted verbatim, and a stray diagnostic on stdout must
// not cost us the events around it.
func (s *eventStream) observe(line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return
	}

	var envelope eventEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		s.malformed++
		s.logger.Debug("unparseable line on claude stdout",
			slog.Int("bytes", len(trimmed)), slog.String("error", err.Error()))
		return
	}
	s.events++

	switch {
	case envelope.Type == "rate_limit_event":
		var event struct {
			Info quotaInfo `json:"rate_limit_info"`
		}
		if err := json.Unmarshal(trimmed, &event); err != nil {
			var status struct {
				Info struct {
					Status string `json:"status"`
				} `json:"rate_limit_info"`
			}
			if json.Unmarshal(trimmed, &status) == nil {
				// Top-level admission remains authoritative even when optional
				// window metadata is unsupported. A malformed rejection has no
				// usable reset, so observe records it conservatively.
				s.quota.observe(quotaInfo{Status: status.Info.Status})
			}
			return
		}
		s.quota.observe(event.Info)
	case envelope.Type == eventTypeSystem && envelope.Subtype == subtypeInit:
		var event initEvent
		if err := json.Unmarshal(trimmed, &event); err != nil {
			s.logger.Warn("init event could not be decoded", slog.String("error", err.Error()))
			return
		}
		s.model = event.Model
		s.tools = event.Tools
		s.sessionID = event.SessionID
		s.logger.Info("claude session started",
			slog.String("model", event.Model),
			slog.String("session_id", event.SessionID),
			slog.String("tools", strings.Join(event.Tools, ",")),
			slog.String("permission_mode", event.PermissionMode),
			slog.String("api_key_source", event.APIKeySource))

	case envelope.Type == eventTypeResult:
		var event resultEvent
		if err := json.Unmarshal(trimmed, &event); err != nil {
			s.logger.Warn("result event could not be decoded", slog.String("error", err.Error()))
			return
		}
		// Keep the last one. There is normally exactly one; if claude ever
		// emits more, the terminal event is the one that describes the run.
		s.result = &event
		if event.SessionID != "" {
			s.sessionID = event.SessionID
		}
	}
}

// err reports a failure to persist the stream, if there was one.
func (s *eventStream) err() error { return s.writeErr }

// tailWriter keeps the last bytes written to it and discards the rest. It is
// how stderr is captured: a run that fails needs its last lines quoted in the
// failure comment (S-9), and a run that fails by emitting megabytes of
// diagnostics must not take the daemon's memory with it.
//
// It is written by the stderr reader goroutine and read only after that
// goroutine has been joined, so the mutex is not strictly required — it is here
// so that reading it early is a wrong answer rather than a data race.
type tailWriter struct {
	limit int

	mu        sync.Mutex
	buf       []byte
	truncated bool
}

// Write implements io.Writer and never fails: dropping the middle of a large
// stderr is the point, not an error.
func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	if n >= w.limit {
		w.buf = append(w.buf[:0], p[n-w.limit:]...)
		w.truncated = true
		return n, nil
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.limit {
		w.buf = w.buf[len(w.buf)-w.limit:]
		w.truncated = true
	}
	return n, nil
}

// String returns the retained tail, prefixed with a marker when earlier output
// was dropped so a reader is never shown a partial stderr as if it were whole.
func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.truncated {
		return "[earlier stderr dropped]\n" + string(w.buf)
	}
	return string(w.buf)
}
