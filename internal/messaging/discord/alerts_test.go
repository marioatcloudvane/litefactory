package discord_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestAlertPayloadMentionTrust(t *testing.T) {
	for _, tc := range []struct {
		name, severity, kind string
		mention, wantMention bool
	}{
		{"critical owner", "critical", "opening", true, true},
		{"critical escalation", "critical", "escalation", true, true},
		{"critical none", "critical", "opening", false, false},
		{"warning forced flag", "warning", "opening", true, false},
		{"all clear forced flag", "critical", "clear", true, false},
		{"digest forced flag", "warning", "digest", true, false},
		{"missing category forced flag", "", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/channels/3/messages" {
					t.Errorf("route %s %s", r.Method, r.URL.Path)
				}
				var p struct {
					Content, Nonce string
					Enforce        bool `json:"enforce_nonce"`
					Mentions       struct {
						Parse, Users, Roles []string
						RepliedUser         bool `json:"replied_user"`
					} `json:"allowed_mentions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Fatal(err)
				}
				if p.Nonce == "" || !p.Enforce || len(p.Mentions.Parse) != 0 || len(p.Mentions.Roles) != 0 || p.Mentions.RepliedUser {
					t.Errorf("unsafe payload %+v", p)
				}
				want := 0
				if tc.wantMention {
					want = 1
				}
				if len(p.Mentions.Users) != want || (want == 1 && p.Mentions.Users[0] != "2") {
					t.Errorf("allowlist %+v", p.Mentions)
				}
				if strings.Count(p.Content, "<@2>") != want {
					t.Errorf("untrusted owner token escaped neutralization: %q", p.Content)
				}
				for _, s := range []string{"<@!2>", "<@999>", "<@&123>", "@everyone", "@here", "test-secret"} {
					if strings.Contains(p.Content, s) {
						t.Errorf("unsafe text %q", s)
					}
				}
				if len(utf16.Encode([]rune(p.Content))) > 2000 {
					t.Error("payload exceeds Discord budget")
				}
				_, _ = w.Write([]byte(`{"id":"321"}`))
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{Token: secrets.NewToken("test-secret"), APIURL: api.URL, Client: api.Client()})
			result := adapter.SendAlert(t.Context(), alerts.Message{ID: tc.name, Channel: "3", OwnerID: "2", MentionOwner: tc.mention, Severity: tc.severity, Kind: tc.kind, Body: "<@2> <@!2> <@999> <@&123> @everyone @here test-secret " + strings.Repeat("😀", 2000)})
			if !result.Confirmed || result.RemoteID != "321" {
				t.Fatalf("result %+v", result)
			}
		})
	}
}

func TestAlertSendRequiresRemoteIdentityAndSafeUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		body                 string
		confirmed, permanent bool
		unavailable          string
	}{
		{"confirmed", 200, `{"id":"321"}`, true, false, ""},
		{"missing identity", 200, `{}`, false, false, ""},
		{"malformed identity", 200, `{"id":"wrong"}`, false, false, ""},
		{"zero identity", 200, `{"id":"0"}`, false, false, ""},
		{"leading zero identity", 200, `{"id":"0321"}`, false, false, ""},
		{"overflow identity", 200, `{"id":"18446744073709551616"}`, false, false, ""},
		{"maximum canonical identity", 200, `{"id":"18446744073709551615"}`, true, false, ""},
		{"expired auth", 401, `{"message":"test-secret"}`, false, true, "Discord authentication unavailable"},
		{"permission", 403, `{}`, false, true, "Discord alert send permission unavailable"},
		{"destination", 404, `{}`, false, true, "Discord alert destination unavailable"},
		{"ambiguous server", 503, `{}`, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{Token: secrets.NewToken("test-secret"), APIURL: api.URL, Client: api.Client()})
			got := adapter.SendAlert(t.Context(), alerts.Message{ID: "one", Channel: "3", Body: "disk is low"})
			if got.Confirmed != tc.confirmed || got.Permanent != tc.permanent || got.Unavailable != tc.unavailable {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestAlertRateLimitFullDelaySharedWithOrdinary(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "7200")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"retry_after":7500,"global":true}`))
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{Now: func() time.Time { return at }, Token: secrets.NewToken("test-secret"), APIURL: api.URL, Client: api.Client()})
	got := adapter.SendAlert(t.Context(), alerts.Message{ID: "rate", Channel: "3", Body: "disk"})
	if got.RetryAfter != 7500*time.Second || got.Confirmed {
		t.Fatalf("alert delay %+v", got)
	}
	ordinary := adapter.Deliver(t.Context(), messaging.Target{Channel: "4", MessageID: "ordinary"}, "hello")
	if ordinary.RetryAfter != 7500*time.Second || calls != 1 {
		t.Fatalf("global backoff not shared: %+v calls=%d", ordinary, calls)
	}
	at = at.Add(7500 * time.Second)
	adapter.Deliver(t.Context(), messaging.Target{Channel: "4", MessageID: "ordinary"}, "hello")
	if calls != 2 {
		t.Fatalf("expired backoff calls=%d", calls)
	}
}

func TestAlertReconciliationAffirmativeExactMatch(t *testing.T) {
	var captured string
	mode := "match"
	pages := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var p struct{ Content string }
			_ = json.NewDecoder(r.Body).Decode(&p)
			captured = p.Content
			_, _ = w.Write([]byte(`{"id":"321"}`))
			return
		}
		if r.URL.Path == "/users/@me" {
			_, _ = w.Write([]byte(`{"id":"99","bot":true}`))
			return
		}
		pages++
		if mode == "forbidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		content, author, channel := captured, "99", "3"
		switch mode {
		case "foreign bot":
			author = "98"
		case "wrong channel":
			channel = "4"
		case "changed bytes":
			content += " changed"
		case "empty":
			_, _ = w.Write([]byte(`[]`))
			return
		case "incomplete":
			_, _ = w.Write([]byte(`[{`))
			return
		}
		m := map[string]any{"id": "321", "channel_id": channel, "content": content, "author": map[string]any{"id": author, "bot": true}}
		if strings.HasSuffix(r.URL.Path, "/321") {
			_ = json.NewEncoder(w).Encode(m)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{m})
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{Token: secrets.NewToken("test-secret"), APIURL: api.URL, Client: api.Client()})
	message := alerts.Message{ID: "one", Channel: "3", Body: "disk low @everyone"}
	if !adapter.SendAlert(t.Context(), message).Confirmed {
		t.Fatal("capture send failed")
	}
	for _, remote := range []string{"", "321"} {
		id, err := adapter.ReconcileAlert(t.Context(), message, remote)
		if err != nil || id != "321" {
			t.Fatalf("exact match %s %s %v", remote, id, err)
		}
	}
	for _, bad := range []string{"foreign bot", "wrong channel", "changed bytes", "empty", "incomplete", "forbidden"} {
		t.Run(bad, func(t *testing.T) {
			mode = bad
			id, err := adapter.ReconcileAlert(t.Context(), message, "")
			if id != "" || err == nil {
				t.Fatalf("uncertain receipt confirmed: %s %v", id, err)
			}
		})
	}
	if pages != 8 {
		t.Fatalf("unexpected history calls %d", pages)
	}
}

func TestAlertReconciliationBoundedPagination(t *testing.T) {
	pages := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_, _ = w.Write([]byte(`{"id":"99","bot":true}`))
			return
		}
		pages++
		messages := make([]map[string]any, 100)
		for i := range messages {
			messages[i] = map[string]any{"id": fmt.Sprint(1000 - pages*100 - i), "channel_id": "3", "content": "different"}
		}
		_ = json.NewEncoder(w).Encode(messages)
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{Token: secrets.NewToken("test-secret"), APIURL: api.URL, Client: api.Client()})
	id, err := adapter.ReconcileAlert(t.Context(), alerts.Message{ID: "one", Channel: "3", Body: "disk"}, "")
	if id != "" || err == nil || pages != 4 {
		t.Fatalf("pagination id=%s err=%v pages=%d", id, err, pages)
	}
}
