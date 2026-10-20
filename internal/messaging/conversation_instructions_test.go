package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

type instructionFailureFixture struct {
	service      *Service
	db           *store.DB
	provider     *factoryUnitProvider
	sender       *detailsUnitSender
	conversation store.Conversation
	at           time.Time
}

func newInstructionFailureFixture(t *testing.T, pkg *instructionpackage.Package, factory bool, invalid string) instructionFailureFixture {
	t.Helper()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "instructions.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"saved"}`}}
	sender := &detailsUnitSender{result: Delivery{Confirmed: true, PromptID: "saved-reply"}}
	service := New(db, "discord", "1", []string{"2"}, sender, FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	at := time.Now().UTC()
	stateRoot := filepath.Join(t.TempDir(), "sessions")
	if invalid != "" {
		c := store.Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "owner", Provider: "claude", StateDir: filepath.Join(stateRoot, "preexisting")}
		if invalid == "missing" {
			_, err = db.EnsureConversation(ctx, c, at)
		} else {
			_, err = db.EnsureConversationWithCapture(ctx, c, []byte(`{}`), at)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	enableErr := service.EnableConversations(ctx, ConversationOptions{Factory: factory, Instructions: pkg, Bindings: []ConversationBinding{{Channel: "2", Name: "owner", Provider: "claude", Model: "fixture"}}, StateRoot: stateRoot, Providers: map[string]ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }}, at)
	if invalid == "" && enableErr != nil {
		t.Fatal(enableErr)
	}
	if invalid != "" && (enableErr == nil || !strings.Contains(enableErr.Error(), "instructions unavailable or invalid")) {
		t.Fatalf("missing safe capture diagnostic: %v", enableErr)
	}
	conversation, err := db.ReadConversation(ctx, "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	scan, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "101"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "101", OwnerID: "1", Text: "Keep this pending input.", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	return instructionFailureFixture{service, db, provider, sender, conversation, at}
}

func TestConversationInvalidRetainedInstructionsPreservePendingInput(t *testing.T) {
	for _, invalid := range []string{"missing", "corrupt"} {
		t.Run(invalid, func(t *testing.T) {
			f := newInstructionFailureFixture(t, nil, true, invalid)
			if _, err := f.service.ConversationStep(t.Context(), f.at); err != nil {
				t.Fatal(err)
			}
			if f.provider.calls != 0 {
				t.Fatalf("invalid retained instructions launched provider %d times", f.provider.calls)
			}
			pending, err := f.db.PendingNaturalMessages(t.Context(), "2", 10)
			if err != nil || len(pending) != 1 || pending[0].Text != "Keep this pending input." {
				t.Fatalf("pending work changed: %+v %v", pending, err)
			}
			c, err := f.db.ReadConversation(t.Context(), "discord", "2")
			if err != nil || !strings.Contains(c.HealthReason, "model calls paused") || c.ID != f.conversation.ID || c.SessionHandle != "" {
				t.Fatalf("capture failure did not safely pause original binding: %+v %v", c, err)
			}
			if _, err := f.service.ConversationStep(t.Context(), f.at); err != nil {
				t.Fatal(err)
			}
			if f.provider.calls != 0 {
				t.Fatal("paused binding launched provider on later step")
			}
		})
	}
}

func TestConversationSavedReplyDoesNotRequireInstructionRestore(t *testing.T) {
	f := newInstructionFailureFixture(t, nil, true, "missing")
	ctx := t.Context()
	// Arrange an already saved answer, as if capture failure was detected after completion.
	if err := f.db.SetConversationHealth(ctx, f.conversation.ID, "", f.at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AdmitConversationTurn(ctx, f.conversation.ID, "101", "saved prompt", f.at); err != nil {
		t.Fatal(err)
	}
	turn, err := f.db.ClaimConversationTurn(ctx, f.conversation.ID, "saved-worker", f.at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.SaveConversationSession(ctx, turn, "saved-native-session"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompleteConversationTurn(ctx, turn, "completed", "Saved answer survives unavailable instructions.", f.at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.restoreConversationInstructions(ctx, f.conversation.ID, f.at); err == nil {
		t.Fatal("missing retained capture was accepted")
	}
	worked, err := f.service.ConversationDeliveryStep(ctx, f.at)
	if err != nil || !worked {
		t.Fatalf("saved delivery failed: worked=%v err=%v", worked, err)
	}
	if len(f.sender.texts) != 1 || !strings.Contains(f.sender.texts[0], "Saved answer survives unavailable instructions.") {
		t.Fatalf("saved answer changed: %v", f.sender.texts)
	}
	if f.provider.calls != 0 {
		t.Fatalf("saved delivery launched provider %d times", f.provider.calls)
	}
}

func TestConversationRestoreCancellationDoesNotPauseBinding(t *testing.T) {
	f := newInstructionFailureFixture(t, nil, false, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.service.restoreConversationInstructions(ctx, f.conversation.ID, f.at); !errors.Is(err, context.Canceled) {
		t.Fatalf("restore cancellation lost: %v", err)
	}
	c, err := f.db.ReadConversation(t.Context(), "discord", "2")
	if err != nil || c.HealthReason != "" {
		t.Fatalf("cancellation paused binding: %+v %v", c, err)
	}
}

func TestConversationPackageValidInstructionsStillRespectPromptBudget(t *testing.T) {
	for _, role := range []string{"conversation-native", "conversation-factory"} {
		t.Run(role, func(t *testing.T) {
			roles := instructionpackage.ConversationRoles()
			baseline := fstest.MapFS{"metadata.yaml": {Data: []byte("schema: 1\nversion: bounds-fixture\n")}, "catalogue.yaml": {Data: []byte("- " + strings.Join(roles, "\n- ") + "\n")}}
			for _, name := range roles {
				text := "Small " + name + "."
				if name == role {
					text = strings.Repeat("x", 256<<10)
				}
				baseline["agents/"+name+".md"] = &fstest.MapFile{Data: []byte(text)}
			}
			pkg, err := instructionpackage.Load(t.Context(), baseline, "")
			if err != nil {
				t.Fatalf("package-valid role rejected prematurely: %v", err)
			}
			f := newInstructionFailureFixture(t, pkg, role == "conversation-factory", "")
			if role == "conversation-native" {
				_, stepErr := f.service.ConversationStep(t.Context(), f.at)
				if stepErr == nil || !strings.Contains(stepErr.Error(), "prompt byte budget exceeded") {
					t.Fatalf("oversized native prompt was not explicitly rejected: %v", stepErr)
				}
				pending, err := f.db.PendingNaturalMessages(t.Context(), "2", 10)
				if err != nil || len(pending) != 1 {
					t.Fatalf("native overflow lost pending input: %+v %v", pending, err)
				}
			} else {
				ctx := t.Context()
				if _, err := f.db.AdmitConversationTurn(ctx, f.conversation.ID, "101", "saved prompt", f.at); err != nil {
					t.Fatal(err)
				}
				turn, err := f.db.ClaimConversationTurn(ctx, f.conversation.ID, "bounds-worker", f.at, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				capture, err := f.service.restoreConversationInstructions(ctx, f.conversation.ID, f.at)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(factoryConversationPrompt{Instructions: capture.Factory(), OwnerInput: "Keep this pending input."})
				if err != nil {
					t.Fatal(err)
				}
				run := factoryConversationRun{s: f.service, c: f.conversation, b: ConversationBinding{Channel: "2", Provider: "claude", Model: "fixture"}, t: turn, at: f.at}
				if _, err := run.invoke(ctx, string(encoded)); err == nil || !strings.Contains(err.Error(), "prompt byte budget exceeded") {
					t.Fatalf("oversized factory prompt was not explicitly rejected: %v", err)
				}
				exchanges, err := f.db.ListConversationExchanges(ctx, turn.ID)
				if err != nil || len(exchanges) != 0 {
					t.Fatalf("overflow persisted a provider invocation: %+v %v", exchanges, err)
				}
			}
			if f.provider.calls != 0 {
				t.Fatalf("oversized prompt launched provider %d times", f.provider.calls)
			}
			raw, _, err := f.db.ReadConversationCapture(t.Context(), f.conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			capture, err := instructionpackage.RestoreConversation(raw)
			if err != nil {
				t.Fatal(err)
			}
			retained := capture.Native()
			if role == "conversation-factory" {
				retained = capture.Factory()
			}
			if len(retained) != 256<<10 {
				t.Fatal("prompt overflow silently substituted or truncated captured instructions")
			}
		})
	}
}
