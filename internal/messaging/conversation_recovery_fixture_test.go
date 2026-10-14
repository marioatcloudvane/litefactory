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
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// A provider process exits after reporting identity but before its terminal
// event. The database retains an unfinished launch, as after a factory crash.
// Recovery must announce that uncertainty without launching the old input again.
func TestConversationUnfinishedLaunchRecovery(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			ctx := t.Context()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "factory.db")
			executable := conversationExecutable(t, provider)
			at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
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
				db, err = store.Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				if err = db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			open()
			send := &journeySender{}
			var adapter messaging.ConversationProvider
			start := func() *messaging.Service {
				t.Helper()
				s := messaging.New(db, "discord", "7", []string{"3"}, send, messaging.FactoryStatus{DB: db})
				s.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
				adapter = conversationAdapter(t, provider, executable, root)
				err := s.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "recovery", Provider: provider, Model: "fixture"}}, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{provider: adapter}, CheckAccess: func(context.Context, string) error { return nil }}, at)
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			s := start()
			if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			admit := func(id, text string) {
				t.Helper()
				reader := &journeyReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, CreatedAt: at}}}
				intake := messaging.Intake{DB: db, Reader: reader, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: s.Protect}
				if err := intake.Poll(ctx, at); err != nil {
					t.Fatal(err)
				}
			}
			drain := func() {
				t.Helper()
				for range 10 {
					work, err := s.ConversationStep(ctx, at)
					if err != nil {
						t.Fatal(err)
					}
					sent, err := s.ConversationDeliveryStep(ctx, at)
					if err != nil {
						t.Fatal(err)
					}
					if !work && !sent {
						return
					}
				}
				t.Fatal("recovery failed to settle")
			}
			admit("101", "Remember that my chosen flower is marigold.")
			drain()
			original, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil {
				t.Fatal(err)
			}
			admit("102", "Interrupt before answering this input.")
			prompt, err := agent.ConversationPrompt("Interrupt before answering this input.", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.AdmitConversationTurn(ctx, original.ID, "102", prompt, at); err != nil {
				t.Fatal(err)
			}
			turn, err := db.ClaimConversationTurn(ctx, original.ID, "previous-factory", at, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := naming.ConversationSessionRef(original.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.RunConversation(ctx, agent.ConversationRequest{ConversationID: original.ID, TurnID: turn.ID, Model: "fixture", StateDirectory: original.StateDir, Prompt: prompt, Session: agent.SessionRef{Ref: ref, Resume: original.SessionHandle}, BeforeLaunch: func(callback context.Context) error { return db.RenewConversationTurn(callback, turn, at, time.Minute) }, OnSession: func(callback context.Context, session agent.SessionRef) error {
				return db.SaveConversationSession(callback, turn, session.Resume)
			}})
			if err == nil || errors.Is(err, agent.ErrConversationSessionMissing) || errors.Is(err, agent.ErrConversationIsolation) {
				t.Fatalf("fixture did not produce unfinished known session: %v", err)
			}
			unfinished, err := db.LatestConversationTurn(ctx, original.ID)
			if err != nil || unfinished.State != "running" {
				t.Fatalf("launch boundary lost: %+v %v", unfinished, err)
			}
			// The owned fixture exited. Restart with an expired lease; recovery cannot
			// infer whether the submitted input was incorporated into native context.
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = nil
			at = at.Add(2 * time.Minute)
			open()
			s = start()
			drain()
			recovered, err := db.LatestConversationTurn(ctx, original.ID)
			if err != nil || recovered.ID != turn.ID || recovered.State != "interrupted" || recovered.DeliveredAt.IsZero() {
				t.Fatalf("unfinished recovery: %+v %v", recovered, err)
			}
			if len(send.texts) != 2 || !strings.Contains(send.texts[1], "will not be replayed") {
				t.Fatalf("ordered unfinished notice missing: %v", send.texts)
			}
			attempts, err := os.ReadFile(filepath.Join(original.StateDir, "fixture-attempts"))
			if err != nil {
				t.Fatal(err)
			}
			if lines := strings.Split(strings.TrimSpace(string(attempts)), "\n"); len(lines) != 2 {
				t.Fatalf("recovery replayed uncertain input: %q", attempts)
			}
			admit("103", "What flower did I choose?")
			drain()
			retained, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || retained.ID != original.ID || retained.SessionHandle != original.SessionHandle || retained.StateDir != original.StateDir || retained.HealthReason != "" {
				t.Fatalf("interruption lost binding: %+v %v", retained, err)
			}
			if len(send.texts) != 3 || send.texts[2] != "Your flower is marigold." {
				t.Fatalf("resumed context missing: %v", send.texts)
			}
			// Removing only the disposable fixture's native identity models a lost native
			// session. The adapter must try the saved handle, then visibly pause the binding.
			if err := os.Remove(filepath.Join(original.StateDir, "fixture-session")); err != nil {
				t.Fatal(err)
			}
			admit("104", "What flower did I choose?")
			drain()
			paused, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || paused.ID != original.ID || paused.SessionHandle != original.SessionHandle || paused.StateDir != original.StateDir || !strings.Contains(paused.HealthReason, "session missing") {
				t.Fatalf("missing native session replaced or concealed: %+v %v", paused, err)
			}
			if len(send.texts) != 4 || !strings.Contains(send.texts[3], "no replacement session created") {
				t.Fatalf("missing-session notice absent: %v", send.texts)
			}
			attempts, err = os.ReadFile(filepath.Join(original.StateDir, "fixture-attempts"))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(attempts)), "\n")
			if len(lines) != 4 || lines[0] != "cold" {
				t.Fatalf("unexpected provider attempts: %q", attempts)
			}
			for _, line := range lines[1:] {
				if line != "resume "+original.SessionHandle {
					t.Fatalf("resume lost exact identity: %q", attempts)
				}
			}
			admit("105", "Please keep this queued until continuity is repaired.")
			drain()
			pending, err := db.PendingNaturalMessages(ctx, "3", 10)
			if err != nil || len(pending) != 1 || pending[0].SourceID != "105" {
				t.Fatalf("paused work was lost: %+v %v", pending, err)
			}
			finalAttempts, err := os.ReadFile(filepath.Join(original.StateDir, "fixture-attempts"))
			if err != nil || string(finalAttempts) != string(attempts) {
				t.Fatalf("paused binding launched replacement: %q %v", finalAttempts, err)
			}
		})
	}
}
