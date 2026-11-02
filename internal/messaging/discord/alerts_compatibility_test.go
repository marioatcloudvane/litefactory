package discord_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// Ordinary caller-controlled content must never acquire mention permissions,
// including the configured owner's token, on either reply route.
type baselineIntake struct{}

func (baselineIntake) Admit(context.Context, messaging.Message, time.Time) (bool, bool, error) {
	return true, true, nil
}
func (baselineIntake) Notify() {}

func TestOrdinaryPayloadMentionSuppressionBaseline(t *testing.T) {
	for _, route := range []string{"channel", "interaction", "reply"} {
		t.Run(route, func(t *testing.T) {
			calls := 0
			at := time.Now()
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if route == "interaction" && r.Method != http.MethodPatch {
					t.Errorf("interaction used %s", r.Method)
				}
				if route != "interaction" && r.Method != http.MethodPost {
					t.Errorf("channel used %s", r.Method)
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				var mentions struct {
					Parse       []string `json:"parse"`
					Users       []string `json:"users"`
					Roles       []string `json:"roles"`
					RepliedUser bool     `json:"replied_user"`
				}
				if len(payload["allowed_mentions"]) == 0 {
					t.Error("missing explicit allowed_mentions")
				}
				if err := json.Unmarshal(payload["allowed_mentions"], &mentions); err != nil {
					t.Error(err)
				}
				if len(mentions.Parse) != 0 || len(mentions.Users) != 0 || len(mentions.Roles) != 0 || mentions.RepliedUser {
					t.Errorf("ordinary payload permits mentions: %s", payload["allowed_mentions"])
				}
				_, _ = w.Write([]byte(`{"id":"123456789"}`))
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Now: func() time.Time { return at }, Token: secrets.NewToken("baseline-token"), APIURL: api.URL, Client: api.Client()})
			target := messaging.Target{Transport: "discord", Channel: "3", MessageID: "ordinary-message"}
			if route == "interaction" {
				target.Handle = "10"
				signed(t, adapter.Handler(baselineIntake{}), private, at, body("10", "3", "2", "details"))
			}
			const content = "<@2> <@!2> <@999> <@&123> @everyone @here"
			var got messaging.Delivery
			if route == "reply" {
				got = adapter.DeliverReply(t.Context(), target, presentation.Payload{Content: content})
			} else {
				got = adapter.Deliver(t.Context(), target, content)
			}
			if calls != 1 || !got.Confirmed {
				t.Fatalf("delivery calls=%d result=%+v", calls, got)
			}
		})
	}
}
