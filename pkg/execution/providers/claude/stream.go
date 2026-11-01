package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
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

// EventEnvelope is enough of any event to route it. Every line is decoded into
// this first, and only the two interesting kinds are decoded again in full.
type EventEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// InitEvent is the first event of a run. It reports what claude actually
// resolved, which is not necessarily what was asked for: .tools reflects
// --tools (SD-2), and .session_id echoes --session-id.
type InitEvent struct {
	Model          string   `json:"model"`
	Tools          []string `json:"tools"`
	SessionID      string   `json:"session_id"`
	CWD            string   `json:"cwd"`
	PermissionMode string   `json:"permissionMode"`
	APIKeySource   string   `json:"apiKeySource"`
}

// ResultEvent is the terminal event.
//
// Note what is absent: .subtype. It reads "success" even when .is_error is
// true, so branching on it silently reports failed runs as successful (SD-3).
// The only trustworthy field is .is_error.
type ResultEvent struct {
	SessionID    string          `json:"session_id"`
	IsError      bool            `json:"is_error"`
	Errors       []string        `json:"errors"`
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
func (e *ResultEvent) UnmarshalJSON(data []byte) error {
	type plain ResultEvent
	var decoded struct {
		*plain
		Cost  *float64 `json:"total_cost_usd"`
		Turns *int     `json:"num_turns"`
	}
	*e = ResultEvent{}
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

// Stream consumes claude's stdout: it persists every line verbatim and
// extracts the handful of fields a run report needs.
type Stream struct {
	Sink   io.Writer
	Logger *slog.Logger

	Events    int
	Malformed int
	Longest   int

	Model     string
	Tools     []string
	SessionID string
	Result    *ResultEvent

	WriteErr error
	Observe  func([]byte)
}

// Consume reads r to EOF.
//
// It returns only read failures. A failure to write the run log does not stop
// the loop: the pipe still has to be drained to EOF or the child blocks on a
// full pipe buffer and cmd.Wait never returns. The write failure is reported
// separately by [Stream.Err].
func (s *Stream) Consume(r io.Reader) error {
	reader := bufio.NewReaderSize(r, readerBufferSize)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s.Record(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read claude stdout: %w", err)
		}
	}
}

// Record persists one raw line and then interprets it.
func (s *Stream) Record(line []byte) {
	if len(line) > s.Longest {
		s.Longest = len(line)
	}
	s.WriteLine(line)
	if s.Observe != nil {
		s.Observe(line)
	}
	s.ObserveLine(line)
}

// WriteLine appends the line to the run log exactly as claude emitted it. A final
// line without a newline — which happens when the process is killed mid-write —
// gets one, so the file stays parseable line by line.
func (s *Stream) WriteLine(line []byte) {
	if len(line) == 0 || s.Sink == nil || s.WriteErr != nil {
		return
	}
	if _, err := s.Sink.Write(line); err != nil {
		s.WriteErr = fmt.Errorf("write run log: %w", err)
		return
	}
	if line[len(line)-1] != '\n' {
		if _, err := s.Sink.Write([]byte{'\n'}); err != nil {
			s.WriteErr = fmt.Errorf("write run log: %w", err)
		}
	}
}

// ObserveLine extracts the fields the run report needs from one event.
//
// A line that is not JSON is counted and skipped rather than failing the run:
// it has already been persisted verbatim, and a stray diagnostic on stdout must
// not cost us the events around it.
func (s *Stream) ObserveLine(line []byte) {
	if s.Logger == nil {
		s.Logger = slog.New(slog.DiscardHandler)
	}
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return
	}

	var envelope EventEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		s.Malformed++
		s.Logger.Debug("unparseable line on claude stdout",
			slog.Int("bytes", len(trimmed)), slog.String("error", err.Error()))
		return
	}
	s.Events++

	switch {
	case envelope.Type == eventTypeSystem && envelope.Subtype == subtypeInit:
		var event InitEvent
		if err := json.Unmarshal(trimmed, &event); err != nil {
			s.Logger.Warn("init event could not be decoded", slog.String("error", err.Error()))
			return
		}
		s.Model = event.Model
		s.Tools = event.Tools
		s.SessionID = event.SessionID
		s.Logger.Info("claude session started",
			slog.String("model", event.Model),
			slog.String("session_id", event.SessionID),
			slog.String("tools", strings.Join(event.Tools, ",")),
			slog.String("permission_mode", event.PermissionMode),
			slog.String("api_key_source", event.APIKeySource))

	case envelope.Type == eventTypeResult:
		var event ResultEvent
		if err := json.Unmarshal(trimmed, &event); err != nil {
			s.Logger.Warn("result event could not be decoded", slog.String("error", err.Error()))
			return
		}
		// Keep the last one. There is normally exactly one; if claude ever
		// emits more, the terminal event is the one that describes the run.
		s.Result = &event
		if event.SessionID != "" {
			s.SessionID = event.SessionID
		}
	}
}

// Err reports a failure to persist the stream, if there was one.
func (s *Stream) Err() error { return s.WriteErr }
