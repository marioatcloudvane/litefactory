package harness

import (
	"encoding/json"
	"io"
	"log/slog"

	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
)

type resultEvent = native.ResultEvent

// eventStream attaches factory usage attribution and quota decisions to the native decoder.
type eventStream struct {
	sink                       io.Writer
	logger                     *slog.Logger
	events, malformed, longest int
	model                      string
	tools                      []string
	sessionID                  string
	result                     *resultEvent
	quota                      quotaEvidence
	writeErr                   error
	usage                      *usageCollector
	native                     *native.Stream
}

func (s *eventStream) decoder() *native.Stream {
	if s.native == nil {
		s.native = &native.Stream{Sink: s.sink, Logger: s.logger, Observe: func(line []byte) {
			s.usage.observe(line)
			var event struct {
				Type string    `json:"type"`
				Info quotaInfo `json:"rate_limit_info"`
			}
			if json.Unmarshal(line, &event) == nil && event.Type == "rate_limit_event" {
				s.quota.observe(event.Info)
			} else {
				var status struct {
					Type string `json:"type"`
					Info struct {
						Status string `json:"status"`
					} `json:"rate_limit_info"`
				}
				if json.Unmarshal(line, &status) == nil && status.Type == "rate_limit_event" {
					s.quota.observe(quotaInfo{Status: status.Info.Status})
				}
			}
		}}
	}
	return s.native
}
func (s *eventStream) sync() {
	n := s.decoder()
	s.events = n.Events
	s.malformed = n.Malformed
	s.longest = n.Longest
	s.model = n.Model
	s.tools = n.Tools
	s.sessionID = n.SessionID
	s.result = n.Result
	s.writeErr = n.WriteErr
}
func (s *eventStream) consume(r io.Reader) error { err := s.decoder().Consume(r); s.sync(); return err }
func (s *eventStream) err() error                { return s.writeErr }
