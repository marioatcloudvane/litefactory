package messaging_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

// This recorded provider detects accidental replay. It never launches a binary
// or claims to demonstrate native-provider persistence or isolation.
type recoveryRecordingProvider struct{ requests []agent.ConversationRequest }

func (p *recoveryRecordingProvider) RunConversation(_ context.Context, request agent.ConversationRequest) (agent.ConversationResult, error) {
	p.requests = append(p.requests, request)
	return agent.ConversationResult{}, errors.New("unexpected provider launch during missing-identity recovery")
}

type recoveryRecordingSender struct {
	targets []messaging.Target
	texts   []string
}

func (s *recoveryRecordingSender) Deliver(_ context.Context, target messaging.Target, text string) messaging.Delivery {
	s.targets = append(s.targets, target)
	s.texts = append(s.texts, text)
	if len(s.targets) == 1 {
		return messaging.Delivery{RetryAfter: time.Hour}
	}
	return messaging.Delivery{Confirmed: true, PromptID: "recorded-interruption-notice"}
}

func TestConversationRecordedCrashBeforeIdentityRetainsAndFences(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			databasePath := filepath.Join(root, "factory.db")
			var db *store.DB
			defer func() {
				if db != nil {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				}
			}()
			open := func() {
				t.Helper()
				var err error
				db, err = store.Open(ctx, databasePath)
				if err != nil {
					t.Fatal(err)
				}
				if err = db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			open()
			at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
			oldAt := at.Add(-11 * time.Minute)
			recordedProvider := &recoveryRecordingProvider{}
			sender := &recoveryRecordingSender{}
			start := func() *messaging.Service {
				t.Helper()
				service := messaging.New(db, "discord", "7", []string{"3"}, sender, messaging.FactoryStatus{DB: db})
				service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
				err := service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "retained-work", Provider: provider, Model: "recorded"}}, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{provider: recordedProvider}, CheckAccess: func(context.Context, string) error { return nil }}, at)
				if err != nil {
					t.Fatal(err)
				}
				return service
			}
			start()
			binding, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(binding.StateDir, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(binding.StateDir, "recorded-native-state")
			if err = os.WriteFile(marker, []byte("available unfinished native bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			scan, err := db.ReadNaturalScan(ctx, "3")
			if err != nil {
				t.Fatal(err)
			}
			if err = db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "3", Cursor: "100", HighWater: "102"}, []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: "uncertain original input", CreatedAt: oldAt}, {ChannelRef: "3", SourceID: "102", OwnerID: "7", Text: "queued follow-up", CreatedAt: oldAt}}, true); err != nil {
				t.Fatal(err)
			}
			prompt, err := agent.ConversationPrompt("uncertain original input", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.AdmitConversationTurn(ctx, binding.ID, "101", prompt, oldAt); err != nil {
				t.Fatal(err)
			}
			crashedTurn, err := db.ClaimConversationTurn(ctx, binding.ID, "crashed-worker", oldAt, 10*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			// The input may have reached the provider, but no native handle or result
			// reached SQLite. A restarted service must not submit it a second time.
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			db = nil
			open()
			service := start()
			if worked, err := service.ConversationStep(ctx, at); err != nil || !worked {
				t.Fatalf("recovery did not run: worked=%t err=%v", worked, err)
			}
			recovered, err := db.LatestConversationTurn(ctx, binding.ID)
			if err != nil || recovered.ID != crashedTurn.ID || recovered.State != "interrupted" || recovered.Prompt != prompt {
				t.Fatalf("uncertain turn changed: %+v err=%v", recovered, err)
			}
			paused, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || paused.ID != binding.ID || paused.StateDir != binding.StateDir || paused.Provider != provider || paused.Name != binding.Name || paused.SessionHandle != "" || !strings.Contains(paused.HealthReason, "no saved native session") || paused.DeliveryPaused {
				t.Fatalf("recovery replaced or hid binding: %+v err=%v", paused, err)
			}
			if err = db.CompleteConversationTurn(ctx, crashedTurn, "interrupted", "late stale notice", at); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("stale worker completed: %v", err)
			}
			if worked, err := service.ConversationDeliveryStep(ctx, at); err != nil || !worked {
				t.Fatalf("notice delivery not attempted: worked=%t err=%v", worked, err)
			}
			if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "before its native session identity was saved") || !strings.Contains(sender.texts[0], "no replacement session was created") {
				t.Fatalf("missing identity not visible: %v", sender.texts)
			}
			// Restart during the saved-notice retry. Its bytes and nonce survive without
			// releasing the following owner input or invoking either selected provider.
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			db = nil
			open()
			service = start()
			if worked, err := service.ConversationStep(ctx, at); err != nil || worked {
				t.Fatalf("paused input ran before notice: worked=%t err=%v", worked, err)
			}
			if worked, err := service.ConversationDeliveryStep(ctx, at); err != nil || worked {
				t.Fatalf("retry delay ignored: worked=%t err=%v", worked, err)
			}
			at = at.Add(25 * time.Hour)
			if err = service.Purge(ctx, at); err != nil {
				t.Fatal(err)
			}
			if worked, err := service.ConversationDeliveryStep(ctx, at); err != nil || !worked {
				t.Fatalf("saved notice retry missing: worked=%t err=%v", worked, err)
			}
			if len(sender.texts) != 2 || sender.texts[1] != sender.texts[0] || sender.targets[1].MessageID != sender.targets[0].MessageID || !sender.targets[1].Protected {
				t.Fatalf("retry changed saved notice: texts=%v targets=%v", sender.texts, sender.targets)
			}
			if worked, err := service.ConversationStep(ctx, at); err != nil || worked {
				t.Fatalf("missing identity silently launched after notice: worked=%t err=%v", worked, err)
			}
			pending, err := db.PendingNaturalMessages(ctx, "3", 10)
			if err != nil || len(pending) != 1 || pending[0].SourceID != "102" || pending[0].Text != "queued follow-up" {
				t.Fatalf("queued input lost or replayed: %+v err=%v", pending, err)
			}
			contents, err := os.ReadFile(marker)
			if err != nil || string(contents) != "available unfinished native bytes" {
				t.Fatalf("native state discarded: %q err=%v", contents, err)
			}
			if len(recordedProvider.requests) != 0 {
				t.Fatalf("uncertain input replayed or replacement launched: %+v", recordedProvider.requests)
			}
		})
	}
}
