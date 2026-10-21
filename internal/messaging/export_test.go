package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
)

// RunCommand invokes a registered handler the way the conversation does and
// returns its assessed outcome status.
func (s *Service) RunCommand(ctx context.Context, m Message, at time.Time) (string, presentation.Status, error) {
	s.mu.RLock()
	h := s.commands[m.Command]
	s.mu.RUnlock()
	if h == nil {
		return "", "", errors.New("command has no handler")
	}
	ctx, assessment := presentation.WithAssessment(ctx)
	text, err := h(ctx, m, at)
	return text, assessment.Status, err
}
