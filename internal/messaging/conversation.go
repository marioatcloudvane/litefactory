package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/providerprocess"
	"github.com/cloudvane/litefactory/internal/store"
)

// ConversationProvider is the narrow native turn boundary; no factory actions cross it.
type ConversationProvider interface {
	RunConversation(context.Context, agent.ConversationRequest) (agent.ConversationResult, error)
}

// ConversationBinding names an opted-in channel and its explicitly selected provider.
type ConversationBinding struct{ Channel, Name, Provider, Model string }

// ConversationOptions is installed before workers start. Context selection belongs
// to the caller and conveys data only. Access is rechecked before every external effect.
type ConversationOptions struct {
	Factory         bool
	Bindings        []ConversationBinding
	StateRoot       string
	Providers       map[string]ConversationProvider
	Models          map[string]string
	CheckAccess     func(context.Context, string) error
	PreparedContext func(context.Context, string) (string, error)
}

type conversationRuntime struct {
	mu           sync.Mutex
	bindings     map[string]ConversationBinding
	channels     []string
	providers    map[string]ConversationProvider
	models       map[string]string
	root, worker string
	check        func(context.Context, string) error
	prepared     func(context.Context, string) (string, error)
	factory      bool
	next         int
	accessMu     sync.Mutex
	accessAfter  map[string]time.Time
}

// EnableConversations installs an exclusive ordinary-message route. Failed
// binding changes retain the route and durable health reason rather than fall
// back to interpreting owner text as a factory command.
func (s *Service) EnableConversations(ctx context.Context, options ConversationOptions, at time.Time) error {
	if !filepath.IsAbs(options.StateRoot) || options.CheckAccess == nil || s.Protect == nil {
		return errors.New("conversation private root, protection and authorization are required")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	r := &conversationRuntime{bindings: make(map[string]ConversationBinding), providers: make(map[string]ConversationProvider), models: make(map[string]string), accessAfter: make(map[string]time.Time), root: filepath.Clean(options.StateRoot), worker: hex.EncodeToString(nonce[:]), check: options.CheckAccess, prepared: options.PreparedContext, factory: options.Factory}
	for name, provider := range options.Providers {
		r.providers[name] = provider
	}
	for name, model := range options.Models {
		r.models[name] = model
	}
	for _, b := range options.Bindings {
		if b.Model != "" {
			r.models[b.Provider] = b.Model
		}
	}
	defer s.conversations.Store(r)
	var failures []error
	for _, b := range options.Bindings {
		if !s.channels[b.Channel] || b.Name == "" || b.Model == "" || (b.Provider != "claude" && b.Provider != "codex") || r.bindings[b.Channel].Channel != "" {
			failures = append(failures, errors.New("invalid conversation channel binding"))
			continue
		}
		r.bindings[b.Channel] = b
		if err := s.ensureConversation(ctx, r.root, b, at); err != nil {
			failures = append(failures, err)
		}
	}
	// Retired configuration never silently releases queued conversation text to
	// the old command interpreter. Preserve the binding and expose the reason.
	saved, err := s.db.ListConversations(ctx)
	if err != nil {
		failures = append(failures, err)
	}
	for _, c := range saved {
		if c.Transport != s.transport {
			continue
		}
		if _, ok := r.bindings[c.ChannelRef]; !ok {
			r.bindings[c.ChannelRef] = ConversationBinding{Channel: c.ChannelRef}
			failures = append(failures, s.db.SetConversationHealth(ctx, c.ID, "conversation disabled or channel authorization removed; retained work is paused", at))
		}
	}
	for c := range r.bindings {
		r.channels = append(r.channels, c)
	}
	sort.Strings(r.channels)
	return errors.Join(failures...)
}

func (s *Service) conversationBound(channel string) bool {
	r := s.conversations.Load()
	if r == nil {
		return false
	}
	_, ok := r.bindings[channel]
	return ok
}

func (s *Service) ensureConversation(ctx context.Context, root string, b ConversationBinding, at time.Time) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	// The random directory is independent of the channel label and owner input.
	state := filepath.Join(root, hex.EncodeToString(nonce[:]))
	_, err := s.db.EnsureConversation(ctx, store.Conversation{Transport: s.transport, ChannelRef: b.Channel, OwnerID: s.owner, Name: b.Name, Provider: b.Provider, StateDir: state}, at)
	if err != nil {
		c, e := s.db.ReadConversation(ctx, s.transport, b.Channel)
		if e == nil && !errors.Is(err, store.ErrConversationDrain) {
			err = errors.Join(err, s.db.SetConversationHealth(ctx, c.ID, "conversation configuration change refused; provider change requires drained work and the same owner", at))
		}
	}
	return err
}

// ConversationStep admits released input then runs one head turn. Its own mutex
// never holds Service.stepMu, so command delivery and intake stay independent.
func (s *Service) ConversationStep(ctx context.Context, at time.Time) (bool, error) {
	r := s.conversations.Load()
	if r == nil || len(r.channels) == 0 {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for offset := 0; offset < len(r.channels); offset++ {
		index := (r.next + offset) % len(r.channels)
		channel := r.channels[index]
		b := r.bindings[channel]
		c, err := s.db.ReadConversation(ctx, s.transport, channel)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return false, err
		}
		if b.Provider == "" || !s.channels[channel] || c.OwnerID != s.owner {
			continue
		}
		if c.Provider != b.Provider {
			if err = s.ensureConversation(ctx, r.root, b, at); err != nil && !errors.Is(err, store.ErrConversationDrain) {
				return false, err
			}
			c, err = s.db.ReadConversation(ctx, s.transport, channel)
			if err != nil {
				return false, err
			}
			if c.Provider != b.Provider {
				b.Provider = c.Provider
				b.Model = r.models[c.Provider]
				if b.Model == "" {
					return false, errors.New("draining conversation requires previous provider model")
				}
			}
		}
		if c.HealthReason != "" {
			continue
		}
		if err = conversationAccess(ctx, r, channel, at); err != nil {
			if accessRetry(err) > 0 {
				continue
			}
			return true, s.db.SetConversationHealth(ctx, c.ID, "conversation authorization unavailable; retained work is paused", at)
		}
		// Only recover after process ownership proves the old child has exited.
		unfinished, err := s.db.RecoverConversationTurns(ctx, c.ID, at)
		if err != nil {
			return false, err
		}
		if len(unfinished) > 0 {
			if err = providerprocess.ConversationIdle(c.StateDir); err != nil {
				if errors.Is(err, agent.ErrConversationBusy) {
					continue
				}
				return false, err
			}
			for _, t := range unfinished {
				notice := "The previous turn was interrupted. Its input will not be replayed. Your next message will continue the saved native session; its unfinished context may be incomplete."
				if c.SessionHandle == "" {
					notice = "The previous turn was interrupted before its native session identity was saved. Continuity is unavailable; no replacement session was created."
				}
				if r.factory {
					savedOutcome, recoveryErr := s.db.RecoverConversationActionOutcome(ctx, t, at)
					if recoveryErr != nil && !errors.Is(recoveryErr, store.ErrNotFound) {
						return true, recoveryErr
					}
					if errors.Is(recoveryErr, store.ErrNotFound) {
						exchanges, e := s.db.ListConversationExchanges(ctx, t.ID)
						if e != nil {
							return true, e
						}
						for _, exchange := range exchanges {
							if exchange.Kind == "saved_outcome" {
								notice = "Saved action outcome:\n" + exchange.Result
							}
						}
					}
					if recoveryErr == nil {
						notice = "Saved action outcome:\n" + savedOutcome
					}
				}
				if err = s.conversationOutcome(ctx, t, "interrupted", notice, at); err != nil {
					return true, err
				}
				if c.SessionHandle == "" {
					if err = s.db.SetConversationHealth(ctx, c.ID, "interrupted turn has no saved native session; continuity unavailable", at); err != nil {
						return true, err
					}
				}
			}
			r.next = (index + 1) % len(r.channels)
			return true, nil
		}
		messages, err := s.db.PendingNaturalMessages(ctx, channel, 1)
		if err != nil {
			return false, err
		}
		if len(messages) > 0 {
			m := messages[0]
			if m.OwnerID != s.owner {
				return true, s.db.SetConversationHealth(ctx, c.ID, "conversation message owner mismatch; retained work is paused", at)
			}
			input, err := s.Protect(ctx, m.Text)
			if err != nil {
				return false, err
			}
			prepared := ""
			if r.prepared != nil {
				prepared, err = r.prepared(ctx, channel)
				if err != nil {
					return false, err
				}
				prepared, err = s.Protect(ctx, prepared)
				if err != nil {
					return false, err
				}
			}
			recovery := ""
			previous, previousErr := s.db.LatestConversationTurn(ctx, c.ID)
			if previousErr != nil && !errors.Is(previousErr, store.ErrNotFound) {
				return false, previousErr
			}
			if previousErr == nil && previous.State == "interrupted" {
				recovery = "The previous turn was interrupted. Its input was not replayed. Continue available native context without claiming its answer completed."
			}
			prompt, err := agent.ConversationPrompt(input, prepared, recovery)
			if err != nil {
				return false, err
			}
			if _, err = s.db.AdmitConversationTurn(ctx, c.ID, m.SourceID, prompt, at); err != nil {
				return false, err
			}
		}
		t, err := s.db.ClaimConversationTurn(ctx, c.ID, r.worker, at, 10*time.Minute)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		r.next = (index + 1) % len(r.channels)
		return true, s.runConversation(ctx, c, b, t, at)
	}
	return false, nil
}

func (s *Service) runConversation(ctx context.Context, c store.Conversation, b ConversationBinding, t store.ConversationTurn, at time.Time) error {
	if s.conversations.Load().factory {
		return s.runFactoryConversation(ctx, c, b, t, at)
	}
	r := s.conversations.Load()
	provider := r.providers[c.Provider]
	if provider == nil {
		return s.conversationUnavailable(ctx, c, t, "selected conversation provider unavailable", at)
	}
	if err := conversationAccess(ctx, r, c.ChannelRef, at); err != nil {
		if accessRetry(err) > 0 {
			return s.db.DeferConversationTurn(ctx, t)
		}
		return s.conversationUnavailable(ctx, c, t, "conversation authorization unavailable; provider launch refused", at)
	}
	if err := conversationCapacity(r.root); err != nil {
		return s.conversationUnavailable(ctx, c, t, "conversation disk backpressure; provider launch refused", at)
	}
	sessionRef, identityErr := naming.ConversationSessionRef(c.ID)
	if identityErr != nil {
		return s.conversationUnavailable(ctx, c, t, "invalid stored conversation identity; provider launch refused", at)
	}
	var identityRecorded atomic.Bool
	turnCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// Identity uses the factory UUID for cold launch and only the exact native
	// handle for resume. Never select latest or retry uncertain input.
	result, runErr := provider.RunConversation(turnCtx, agent.ConversationRequest{ConversationID: c.ID, TurnID: t.ID, Model: b.Model, StateDirectory: c.StateDir, Prompt: t.Prompt, Session: agent.SessionRef{Ref: sessionRef, Resume: c.SessionHandle}, BeforeLaunch: func(callback context.Context) error {
		if err := conversationAccess(callback, r, c.ChannelRef, at); err != nil {
			return err
		}
		return s.db.RenewConversationTurn(callback, t, at, 10*time.Minute)
	}, OnSession: func(callback context.Context, ref agent.SessionRef) error {
		if ref.Ref != sessionRef || !agent.ConversationIdentity(ref.Resume) {
			return errors.New("conversation provider reported invalid identity")
		}
		if err := s.db.SaveConversationSession(callback, t, ref.Resume); err != nil {
			return err
		}
		identityRecorded.Store(true)
		return nil
	}})
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	var accessErr *ConversationAccessError
	switch {
	case errors.As(runErr, &accessErr):
		if !accessErr.Permanent {
			return s.db.DeferConversationTurn(cleanup, t)
		}
		return s.conversationUnavailable(cleanup, c, t, "conversation authorization removed before provider launch; retained work is paused", at)
	case errors.Is(runErr, agent.ErrConversationBusy):
		return s.conversationUnavailable(cleanup, c, t, "previous provider exit could not be established; no new native turn launched", at)
	case errors.Is(runErr, agent.ErrConversationIsolation):
		return s.conversationUnavailable(cleanup, c, t, "conversation isolation unavailable or unverified; provider launch refused", at)
	case errors.Is(runErr, agent.ErrConversationSessionMissing):
		return s.conversationUnavailable(cleanup, c, t, "saved native session missing; no replacement session created", at)
	case runErr != nil:
		saved, err := s.db.ReadConversation(cleanup, c.Transport, c.ChannelRef)
		if err != nil {
			return err
		}
		if saved.SessionHandle == "" {
			return s.conversationUnavailable(cleanup, c, t, "turn unfinished with no saved native identity; no replacement session created", at)
		}
		return s.conversationOutcome(cleanup, t, "interrupted", "The turn was interrupted or failed before a completed answer was recorded. Its input will not be replayed. Your next message will resume the retained native session.", at)
	default:
		if !identityRecorded.Load() || result.Session.Ref != sessionRef || !agent.ConversationIdentity(result.Session.Resume) || (c.SessionHandle != "" && result.Session.Resume != c.SessionHandle) {
			return s.conversationUnavailable(cleanup, c, t, "provider did not confirm the exact saved native session", at)
		}
		if len(result.Answer) == 0 {
			return s.conversationUnavailable(cleanup, c, t, "provider returned no completed answer; retained state is paused", at)
		}
		return s.conversationOutcome(cleanup, t, "completed", string(result.Answer), at)
	}
}

func (s *Service) conversationOutcome(ctx context.Context, t store.ConversationTurn, state, text string, at time.Time) error {
	protected, err := s.Protect(ctx, text)
	if err != nil {
		return err
	}
	if runtime := s.conversations.Load(); runtime != nil && runtime.factory {
		return s.saveConversationPresentation(ctx, t, state, []presentation.AnswerBlock{{Text: protected}}, "", at)
	}
	return s.db.CompleteConversationTurn(ctx, t, state, protected, at)
}
func (s *Service) conversationUnavailable(ctx context.Context, c store.Conversation, t store.ConversationTurn, reason string, at time.Time) error {
	if err := s.conversationOutcome(ctx, t, "unavailable", "Conversation paused: "+reason+". Context and queued messages are retained.", at); err != nil {
		return err
	}
	return s.db.SetConversationHealth(ctx, c.ID, reason, at)
}

// conversationHealth shows durable failures through the existing health command.
func (s *Service) conversationHealth(ctx context.Context, at time.Time) string {
	list, err := s.db.ListConversations(ctx)
	if err != nil {
		return "\nConversation health unavailable."
	}
	text := ""
	for _, c := range list {
		if c.Transport == s.transport && s.channels[c.ChannelRef] {
			reason := c.HealthReason
			if reason == "" {
				reason = "available"
				unfinished, recoveryErr := s.db.RecoverConversationTurns(ctx, c.ID, at)
				if recoveryErr != nil {
					reason = "recovery status unavailable"
				} else if len(unfinished) > 0 {
					reason = "unfinished turn recovery awaits confirmed provider exit"
				}
				if r := s.conversations.Load(); r != nil && r.bindings[c.ChannelRef].Provider != "" && r.bindings[c.ChannelRef].Provider != c.Provider {
					reason = "provider change waiting for drain"
				}
			}
			text += fmt.Sprintf("\nConversation %s (%s): %s.", c.Name, c.Provider, reason)
		}
	}
	return text
}

// conversationHandled records a deterministic factory reply without launching a
// native session. The store refuses it if a provider input was journaled.
func (s *Service) conversationHandled(ctx context.Context, t store.ConversationTurn, text string, at time.Time) error {
	protected, err := s.Protect(ctx, text)
	if err != nil {
		return err
	}
	if runtime := s.conversations.Load(); runtime != nil && runtime.factory {
		return s.saveConversationAnswer(ctx, t, "completed", []presentation.AnswerBlock{{Text: protected}}, "", at, true)
	}
	return s.db.CompleteConversationHandledTurn(ctx, t, protected, at)
}
