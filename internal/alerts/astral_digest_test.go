package alerts_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type astralTransportFunc func(*http.Request) (*http.Response, error)

func (f astralTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQuietDigestAstralHTTPPayloadsAreBounded(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "astral.db"))
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
	type payload struct {
		Content  string `json:"content"`
		Nonce    string `json:"nonce"`
		Mentions struct {
			Parse, Users, Roles []string
			RepliedUser         bool `json:"replied_user"`
		} `json:"allowed_mentions"`
	}
	var posts []payload
	client := &http.Client{Transport: astralTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			response := `{"id":"99","bot":true}`
			if r.URL.Path != "/users/@me" {
				history := []map[string]any{}
				for i, p := range posts {
					history = append(history, map[string]any{"id": fmt.Sprint(1001 + i), "channel_id": "123", "content": p.Content, "author": map[string]any{"id": "99", "bot": true}})
				}
				raw, err := json.Marshal(history)
				if err != nil {
					t.Fatal(err)
				}
				response = string(raw)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
		}
		if r.Method != http.MethodPost || r.URL.Path != "/channels/123/messages" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		posts = append(posts, p)
		if len(posts) == 2 { // Remote acceptance without a usable response identity.
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"%d"}`, 1000+len(posts))))}, nil
	})}
	adapter := discord.New(discord.Options{ApplicationID: "1", Token: secrets.NewToken("offline-astral"), APIURL: "https://offline.invalid", Client: client, Now: func() time.Time { return at }})
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	const count = 6
	for i := 0; i < count; i++ {
		task := fmt.Sprintf("github:test/factory#%d", i+1)
		err := engine.Observe(ctx, alerts.Observation{Rule: "task_stuck", Kind: "task", Key: task, TaskRef: task, ID: task, At: at, Condition: "failing", Severity: "warning", Summary: strings.Repeat("😀", 160) + " @everyone <@456> <@&789>", Action: "Inspect doctor evidence."})
		if err != nil {
			t.Fatal(err)
		}
	}
	if sent, err := engine.SendStep(ctx, at, "123", "456", adapter); err != nil || sent {
		t.Fatalf("quiet warnings were not held: sent=%v err=%v", sent, err)
	}
	// Simulate held bodies persisted by the previous renderer.
	if err := db.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		for _, n := range l.Notifications {
			n.Body += "\n" + n.Marker
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(8 * time.Hour)
	for i := 0; i < 20; i++ {
		sent, err := engine.SendStep(ctx, at, "123", "456", adapter)
		if err != nil {
			t.Fatal(err)
		}
		if !sent {
			break
		}
		if i == 19 {
			t.Fatal("digest did not become idle")
		}
	}
	before, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, uncertain := 0, 0
	for _, n := range before.Notifications {
		if n.Kind == "digest" {
			if n.State == "confirmed" {
				confirmed++
			}
			if n.State == "uncertain" {
				uncertain++
			}
		}
	}
	if confirmed == 0 || uncertain != 1 {
		t.Fatalf("partial digest states: confirmed=%d uncertain=%d", confirmed, uncertain)
	}
	path := db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	engine.DB = db
	postCount := len(posts)
	at = at.Add(6 * time.Minute)
	for i := 0; i < 3; i++ {
		if _, err = engine.SendStep(ctx, at, "123", "456", adapter); err != nil {
			t.Fatal(err)
		}
	}
	if len(posts) != postCount {
		t.Fatal("digest part resent after restart")
	}
	if len(posts) < 2 {
		t.Fatalf("astral members did not form bounded parts: %d", len(posts))
	}
	var combined strings.Builder
	nonces := map[string]bool{}
	for _, p := range posts {
		if strings.Count(p.Content, "[lf:alert:") != 1 {
			t.Fatal("digest must have one transport marker")
		}
		if len(utf16.Encode([]rune(p.Content))) > 2000 {
			t.Fatal("serialized Discord payload exceeds UTF-16 content limit")
		}
		if len(p.Mentions.Parse) != 0 || len(p.Mentions.Users) != 0 || len(p.Mentions.Roles) != 0 || p.Mentions.RepliedUser {
			t.Fatal("warning digest enabled mentions")
		}
		if strings.Contains(p.Content, "@everyone") || strings.Contains(p.Content, "<@456>") || strings.Contains(p.Content, "<@&789>") {
			t.Fatal("untrusted digest mention was not neutralized")
		}
		if p.Nonce == "" || nonces[p.Nonce] {
			t.Fatal("digest part repeated a notification identity")
		}
		nonces[p.Nonce] = true
		combined.WriteString(p.Content)
	}
	for i := 0; i < count; i++ {
		task := fmt.Sprintf("github:test/factory#%d", i+1)
		if strings.Count(combined.String(), task) != 1 {
			t.Fatalf("member %s missing or repeated across digest parts", task)
		}
	}
	l, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range l.Subjects {
		if !s.Episodes[s.Episode].OpeningConfirmed {
			t.Fatal("accepted digest part did not confirm its member episode")
		}
	}
}
