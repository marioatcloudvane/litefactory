package messaging_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

type orderedConversationProvider struct{ calls []string }

func (p *orderedConversationProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	p.calls = append(p.calls, r.TurnID)
	session := r.Session
	if session.Resume == "" {
		session.Resume = session.Ref
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	return agent.ConversationResult{Session: session, Answer: []byte("Saved answer for " + r.TurnID)}, nil
}

type orderedConversationSender struct {
	targets []messaging.Target
	texts   []string
}

func (s *orderedConversationSender) Deliver(_ context.Context, target messaging.Target, text string) messaging.Delivery {
	s.targets = append(s.targets, target)
	s.texts = append(s.texts, text)
	if len(s.targets) == 1 {
		return messaging.Delivery{RetryAfter: time.Hour}
	}
	return messaging.Delivery{Confirmed: true, PromptID: fmt.Sprint(len(s.targets))}
}

func TestConversationDeliveryRetryPreservesOrderAndSavedBytes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(root, "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	provider := &orderedConversationProvider{}
	sender := &orderedConversationSender{}
	service := messaging.New(db, "discord", "7", []string{"3", "4"}, sender, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	if err = service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "first", Provider: "claude", Model: "fixture"}, {Channel: "4", Name: "second", Provider: "claude", Model: "fixture"}}, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }}, at); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{"3", "4"} {
		if err = db.ActivateNaturalScan(ctx, channel, "100"); err != nil {
			t.Fatal(err)
		}
		scan, err := db.ReadNaturalScan(ctx, channel)
		if err != nil {
			t.Fatal(err)
		}
		messages := []store.NaturalMessage{{ChannelRef: channel, SourceID: "101", OwnerID: "7", Text: "first input", CreatedAt: at}}
		high := "101"
		if channel == "3" {
			messages = append(messages, store.NaturalMessage{ChannelRef: channel, SourceID: "102", OwnerID: "7", Text: "later input", CreatedAt: at})
			high = "102"
		}
		if err = db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: channel, Cursor: "100", HighWater: high}, messages, true); err != nil {
			t.Fatal(err)
		}
	}
	step := func() {
		t.Helper()
		if _, err := service.ConversationStep(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	deliver := func() {
		t.Helper()
		if _, err := service.ConversationDeliveryStep(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	step()
	deliver() // Channel 3 saves an answer; delivery is throttled.
	step()
	deliver() // Channel 4 can run and deliver despite channel 3's predecessor.
	step()    // Channel 3 admits later input, but cannot launch it yet.
	if len(provider.calls) != 2 || len(sender.targets) != 2 || sender.targets[0].Channel != "3" || sender.targets[1].Channel != "4" {
		t.Fatalf("throttled channel did not yield: calls=%v targets=%v", provider.calls, sender.targets)
	}
	at = at.Add(25 * time.Hour)
	if err = service.Purge(ctx, at); err != nil {
		t.Fatal(err)
	}
	deliver()
	if len(provider.calls) != 2 || len(sender.targets) != 3 || sender.targets[2].MessageID != sender.targets[0].MessageID || sender.texts[2] != sender.texts[0] || !sender.targets[2].Protected {
		t.Fatalf("retry regenerated or changed saved reply: calls=%v targets=%v texts=%v", provider.calls, sender.targets, sender.texts)
	}
	step()
	deliver()
	if len(provider.calls) != 3 || len(sender.targets) != 4 || sender.targets[3].Channel != "3" || sender.targets[3].MessageID == sender.targets[0].MessageID {
		t.Fatalf("later turn did not follow acknowledged predecessor: calls=%v targets=%v", provider.calls, sender.targets)
	}
}

func TestConversationAccessThrottleBeforeLaunchDefersInput(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(root, "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	provider := &orderedConversationProvider{}
	service := messaging.New(db, "discord", "7", []string{"3", "4"}, &journeySender{}, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	checks := 0
	if err = service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "first", Provider: "claude", Model: "fixture"}, {Channel: "4", Name: "second", Provider: "claude", Model: "fixture"}}, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(_ context.Context, channel string) error {
		if channel == "3" {
			checks++
			if checks == 3 {
				return &messaging.ConversationAccessError{RetryAfter: time.Hour}
			}
		}
		return nil
	}}, at); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{"3", "4"} {
		if err = db.ActivateNaturalScan(ctx, channel, "100"); err != nil {
			t.Fatal(err)
		}
		scan, err := db.ReadNaturalScan(ctx, channel)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: channel, Cursor: "100", HighWater: "101"}, []store.NaturalMessage{{ChannelRef: channel, SourceID: "101", OwnerID: "7", Text: "keep my input", CreatedAt: at}}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = service.ConversationStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	binding, err := db.ReadConversation(ctx, "discord", "3")
	if err != nil {
		t.Fatal(err)
	}
	deferred, err := db.LatestConversationTurn(ctx, binding.ID)
	if err != nil || binding.HealthReason != "" || deferred.State != "pending" || len(provider.calls) != 0 {
		t.Fatalf("temporary access failure launched or paused permanently: binding=%+v turn=%+v calls=%v error=%v", binding, deferred, provider.calls, err)
	}
	if _, err = service.ConversationStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	if len(provider.calls) != 1 || provider.calls[0] == deferred.ID {
		t.Fatalf("throttled channel did not yield: %v", provider.calls)
	}
	at = at.Add(time.Hour)
	if _, err = service.ConversationStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	if len(provider.calls) != 2 || provider.calls[1] != deferred.ID {
		t.Fatalf("deferred input was lost or replayed with a new identity: %v", provider.calls)
	}
}
