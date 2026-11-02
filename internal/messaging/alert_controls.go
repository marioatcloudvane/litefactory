package messaging

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
)

// SetAlertControls supplies the deterministic alert-only command boundary.
// The callback is independent of conversation providers and instruction loading.
func (s *Service) SetAlertControls(command func(context.Context, string, string, time.Time) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertControl = command
}

// HandleAlertControl authenticates before changing controls or queuing a reply.
// The stable source and notice key make history replay reuse the same effect
// and response even when a crash separates the receipt from reply queueing.
func (s *Service) HandleAlertControl(ctx context.Context, m messagingstate.NaturalMessage, at time.Time) (bool, error) {
	if !alerts.Reserved(m.Text) {
		return false, nil
	}
	if m.OwnerID != s.owner || !s.channels[m.ChannelRef] || m.Transport != s.transport {
		return true, nil
	}
	if !intakeID(m.SourceID) {
		return true, fmt.Errorf("alert command source identity unavailable")
	}
	s.mu.RLock()
	command := s.alertControl
	s.mu.RUnlock()
	if command == nil {
		return true, fmt.Errorf("alert controls unavailable")
	}
	source := s.transport + ":" + m.ChannelRef + ":" + m.SourceID
	reply, err := command(ctx, source, m.Text, at)
	if err != nil {
		return true, err
	}
	if err = s.Announce(ctx, m.ChannelRef, "alerts-control:"+source, "", reply, at); err != nil {
		return true, err
	}
	s.Notify()
	return true, nil
}
