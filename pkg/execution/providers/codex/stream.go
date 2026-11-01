package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Stream is native JSONL evidence, with no factory outcome classification.
type Stream struct {
	ThreadID, Text, FailureDetail  string
	QuotaMessage                   string
	Events, Malformed, Longest     int
	TurnStarted, Completed, Failed bool
	Err                            error
}

// Consume decodes native JSONL and preserves every output line in sink.
func Consume(src io.Reader, sink io.Writer, observe func([]byte)) Stream {
	var s Stream
	if sink == nil {
		sink = io.Discard
	}
	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s.Events++
			if len(line) > s.Longest {
				s.Longest = len(line)
			}
			if _, werr := sink.Write(line); werr != nil && s.Err == nil {
				s.Err = werr
			}
			if observe != nil {
				observe(line)
			}
			s.ConsumeLine(line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.Err = err
			}
			break
		}
	}
	return s
}

// ConsumeLine recognizes explicit failure independently of optional provider
// detail. Other Events retain their existing typed-decoding behavior.
func (s *Stream) ConsumeLine(line []byte) {
	var terminal struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &terminal) == nil && terminal.Type == "turn.failed" {
		s.Failed = true
		var detail struct {
			Message    string `json:"message"`
			Code       string `json:"code"`
			StatusCode int    `json:"status_code"`
		}
		if json.Unmarshal(terminal.Error, &detail) == nil && (strings.TrimSpace(detail.Message) != "" || detail.Code != "" || detail.StatusCode != 0) {
			s.FailureDetail = detail.Message
			if detail.Code != "" {
				s.FailureDetail += " " + detail.Code
			}
			if detail.StatusCode != 0 {
				s.FailureDetail += fmt.Sprintf(" HTTP %d", detail.StatusCode)
			}
		} else {
			var message string
			if json.Unmarshal(terminal.Message, &message) == nil && strings.TrimSpace(message) != "" {
				s.FailureDetail = message
			}
		}
		// A quota refusal emits this alongside a generic error notice carrying
		// the same Text; taking it here keeps recoverable notices inert.
		s.QuotaMessage = s.FailureDetail
		return
	}
	var e struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
		Message  string `json:"message"`
		Item     struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &e) != nil {
		s.Malformed++
	} else {
		switch e.Type {
		case "thread.started":
			s.ThreadID = e.ThreadID
		case "turn.started":
			s.TurnStarted = true
		case "item.completed":
			if e.Item.Type == "agent_message" {
				s.Text = e.Item.Text
			}
		case "turn.completed":
			s.Completed = true
			s.QuotaMessage = ""
		case "error":
			// Generic notices can recover; the raw log retains diagnostics.
		}
	}
}
