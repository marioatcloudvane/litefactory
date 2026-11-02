package messaging

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
)

// SetProviderObserver installs an observational sink for completed native turns.
func (s *Service) SetProviderObserver(observer func(context.Context, string, string, string, string)) {
	s.providerObserverMu.Lock()
	s.providerObserver = observer
	s.providerObserverMu.Unlock()
}
func (s *Service) observeConversationProvider(ctx context.Context, provider, conversation, turn string, result agent.ConversationResult, err error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	class := agent.ProviderOutcome(nil, err)
	if err == nil && len(result.Answer) > 0 && result.Session.Resume != "" {
		class = agent.ProviderSuccess
	}
	var failure *agent.ConversationFailure
	if errors.As(err, &failure) && failure.NoProcessStarted {
		return
	}
	// Pre-launch configuration/authorization failures are not provider operations.
	if err != nil && class == agent.ProviderUnknown && (errors.Is(err, agent.ErrConversationBusy) || errors.Is(err, agent.ErrConversationIsolation) || errors.Is(err, agent.ErrConversationSessionMissing)) {
		return
	}
	s.providerObserverMu.RLock()
	observer := s.providerObserver
	s.providerObserverMu.RUnlock()
	if observer != nil {
		observer(ctx, provider, conversation, turn, string(class))
	}
}
