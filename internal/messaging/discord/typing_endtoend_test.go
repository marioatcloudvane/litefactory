package discord_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type typingInterpreter struct{}

func (typingInterpreter) Interpret(context.Context, messaging.InterpretationRequest) ([]byte, error) {
	return []byte(`{"version":1,"kind":"clarify","question":"Which project?"}`), nil
}

type typingReader struct{ messages []store.NaturalMessage }

func (*typingReader) CheckIntakeCapability(context.Context, string) error { return nil }
func (r *typingReader) ReadIntakePage(_ context.Context, _, _, before string) (messaging.IntakePage, error) {
	if before != "" || len(r.messages) == 0 {
		return messaging.IntakePage{}, nil
	}
	return messaging.IntakePage{Messages: r.messages, HighID: r.messages[0].SourceID, LowID: r.messages[len(r.messages)-1].SourceID, Count: len(r.messages)}, nil
}

// TestPlainMessageTypingPrecedesReply runs an admitted plain owner message
// through the production service and adapter against a fake Discord API.
func TestPlainMessageTypingPrecedesReply(t *testing.T) {
	for _, tc := range []struct {
		name   string
		typing func(http.ResponseWriter)
	}{
		{name: "accepted", typing: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }},
		{name: "server error", typing: func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }},
		{name: "forbidden", typing: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":50013,"message":"Missing Permissions"}`))
		}},
		{name: "transport error", typing: func(w http.ResponseWriter) {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "typing.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var calls []string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				mu.Unlock()
				if r.Header.Get("Authorization") != "Bot test-token" {
					t.Errorf("missing bot authorization on %s", r.URL.Path)
				}
				if r.URL.Path == "/channels/3/typing" {
					tc.typing(w)
					return
				}
				_, _ = w.Write([]byte(`{"id":"900"}`))
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client()})
			at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
			s := messaging.New(db, "discord", "7", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
			s.EnableNatural(typingInterpreter{})
			if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			intake := &messaging.Intake{DB: db, Reader: &typingReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: "How is it going?", CreatedAt: at}}}, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
			if err = intake.Poll(ctx, at); err != nil {
				t.Fatal(err)
			}
			for range 10 {
				natural, err := s.NaturalStep(ctx, at)
				if err != nil {
					t.Fatal(err)
				}
				shared, err := s.Step(ctx, at)
				if err != nil {
					t.Fatal(err)
				}
				if !natural && !shared {
					break
				}
			}
			mu.Lock()
			got := append([]string(nil), calls...)
			mu.Unlock()
			typing, reply := -1, -1
			for i, c := range got {
				switch c {
				case "POST /channels/3/typing":
					if typing < 0 {
						typing = i
					}
				case "POST /channels/3/messages":
					reply = i
				}
			}
			if typing < 0 || reply < 0 || typing > reply {
				t.Fatalf("typing must precede the reply: %v", got)
			}
			row, err := db.ReadChatReply(ctx, "discord", "nl:101")
			if err != nil {
				t.Fatal(err)
			}
			if row.State != "confirmed" {
				t.Fatalf("reply not confirmed after typing %s: %+v", tc.name, row)
			}
		})
	}
}
