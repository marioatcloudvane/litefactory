package messaging

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

// typingSender records typing calls and deliveries in one ordered log.
type typingSender struct {
	mu     sync.Mutex
	events []string
}

func (p *typingSender) Typing(_ context.Context, channel string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "typing:"+channel)
}

func (p *typingSender) Deliver(_ context.Context, t Target, _ string) Delivery {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "deliver:"+t.Channel)
	return Delivery{Confirmed: true, PromptID: "900"}
}

func (p *typingSender) log() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

func shortTyping(t *testing.T, d time.Duration) {
	t.Helper()
	previous := typingInterval
	typingInterval = d
	t.Cleanup(func() { typingInterval = previous })
}

type typingConversationProvider func(context.Context, agent.ConversationRequest) (agent.ConversationResult, error)

func (p typingConversationProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	return p(ctx, r)
}

func TestConversationTypingStopsWithWork(t *testing.T) {
	shortTyping(t, 5*time.Millisecond)
	for _, mode := range []string{"success", "failure", "cancel", "paused", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			run, at := factoryUnitRun(t, &factoryUnitProvider{})
			send := &typingSender{}
			run.s.sender = send
			if err := run.s.db.DeferConversationTurn(t.Context(), run.t); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			run.s.conversations.Load().providers["claude"] = typingConversationProvider(func(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
				time.Sleep(30 * time.Millisecond)
				if mode == "cancel" {
					cancel()
					return agent.ConversationResult{}, ctx.Err()
				}
				if mode == "failure" {
					return agent.ConversationResult{}, errors.New("recorded failure")
				}
				session := r.Session
				session.Resume = session.Ref
				if err := r.OnSession(ctx, session); err != nil {
					return agent.ConversationResult{}, err
				}
				return agent.ConversationResult{Session: session, Answer: []byte(`{"version":1,"kind":"answer","text":"done"}`)}, nil
			})
			if mode == "paused" {
				if err := run.s.db.SetConversationHealth(ctx, run.c.ID, "historical uncertainty", at); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unauthorized" {
				run.s.conversations.Load().check = func(context.Context, string) error { return &ConversationAccessError{Permanent: true} }
			}
			if _, err := run.s.ConversationStep(ctx, at); err != nil {
				t.Fatal(err)
			}
			count := len(send.log())
			if mode == "paused" || mode == "unauthorized" {
				if count != 0 {
					t.Fatal("typing without authorized work")
				}
			} else if count < 2 {
				t.Fatalf("no typing refresh: %v", send.log())
			}
			time.Sleep(20 * time.Millisecond)
			if len(send.log()) != count {
				t.Fatal("typing continued after work")
			}
		})
	}
}
