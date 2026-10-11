package discord_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestNaturalPromptDeliveryIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		incomplete, confirmed bool
		id                    string
	}{
		{name: "known", body: `{"id":"123456789"}`, confirmed: true, id: "123456789"},
		{name: "missing identity", body: `{}`, confirmed: true},
		{name: "invalid identity", body: `{"id":"bot-prompt"}`, confirmed: true},
		{name: "uncertain body", body: `{"id":"123456789"}`, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/channels/3/messages" {
					t.Errorf("wrong delivery route: %s %s", r.Method, r.URL.Path)
				}
				var payload struct {
					Content string `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.Content != "Exact proposal. Confirm abc123." {
					t.Errorf("protected bytes changed: %q", payload.Content)
				}
				if tc.incomplete {
					w.Header().Set("Content-Length", "100")
				}
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Error(err)
				}
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client()})
			got := adapter.Deliver(t.Context(), messaging.Target{Transport: "discord", Channel: "3", MessageID: "nl:101", Protected: true}, "Exact proposal. Confirm abc123.")
			if calls != 1 || got.Confirmed != tc.confirmed || got.PromptID != tc.id {
				t.Fatalf("identity delivery: calls=%d got=%+v", calls, got)
			}
		})
	}
}

func TestNaturalProtectedOversizeRefusesBeforeDelivery(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if _, err := w.Write([]byte(`{"id":"123456789"}`)); err != nil {
			t.Error(err)
		}
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client()})
	got := adapter.Deliver(t.Context(), messaging.Target{Transport: "discord", Channel: "3", MessageID: "nl:101", Protected: true}, strings.Repeat("x", 2001))
	if calls != 0 || !got.Permanent || got.Confirmed || got.PromptID != "" {
		t.Fatalf("oversize proposal acquired delivery identity: calls=%d got=%+v", calls, got)
	}
}
