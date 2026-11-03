package alerts_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestFactoryAlertJourney follows two source incidents through the production
// policy, local SQLite ledger and Discord HTTP boundary across a restart.
func TestFactoryAlertJourney(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "factory-alerts.db")
	var db *store.DB
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	open := func() { t.Helper(); var err error; db, err = store.Open(ctx, path); must(err); must(db.Migrate(ctx)) }
	open()
	t.Cleanup(func() {
		if db != nil {
			must(db.Close())
		}
	})
	type payload struct {
		Content  string `json:"content"`
		Nonce    string `json:"nonce"`
		Enforce  bool   `json:"enforce_nonce"`
		Mentions struct {
			Parse       []string `json:"parse"`
			Users       []string `json:"users"`
			Roles       []string `json:"roles"`
			RepliedUser bool     `json:"replied_user"`
		} `json:"allowed_mentions"`
	}
	var posts []payload
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/channels/123/messages" || r.Header.Get("Authorization") != "Bot offline-alert-test" {
			t.Errorf("unexpected alert request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if p.Nonce == "" || !p.Enforce {
			t.Error("alert has no enforced stable identity")
		}
		if len(p.Mentions.Parse) != 0 || len(p.Mentions.Roles) != 0 || p.Mentions.RepliedUser {
			t.Error("alert permits an untrusted mention class")
		}
		if strings.Count(p.Content, "[lf:alert:") != 1 {
			t.Error("notification must carry exactly one transport marker")
		}
		posts = append(posts, p)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"%d"}`, 1000+len(posts))
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{ApplicationID: "1", Token: secrets.NewToken("offline-alert-test"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	settings := config.DefaultAlerts()
	engine := &alerts.Engine{DB: db, Settings: settings, Location: time.UTC}
	drain := func() {
		t.Helper()
		for i := 0; i < 10; i++ {
			sent, err := engine.SendStep(ctx, at, "123", "456", adapter)
			must(err)
			if !sent {
				return
			}
		}
		t.Fatal("alert delivery did not become idle")
	}
	disk := alerts.Observation{Rule: "disk_low", Kind: "factory disk", Key: "daemon-volume"}
	task := store.AlertTaskSource{Task: "github:test/factory#1", Version: "implement", ProgressAt: at.Add(-121 * time.Minute)}
	stuck := alerts.Observation{Rule: "no_progress", Kind: "task", Key: task.Task}
	must(engine.Disk(ctx, at, 99, 100, nil))
	must(engine.Progress(ctx, task, at))
	drain()
	if len(posts) != 2 {
		t.Fatalf("opening count=%d, want two", len(posts))
	}
	for _, p := range posts {
		if len(p.Mentions.Users) != 1 || p.Mentions.Users[0] != "456" || strings.Count(p.Content, "<@456>") != 1 {
			t.Error("critical opening must construct exactly the configured owner mention")
		}
	}
	at = at.Add(time.Minute)
	must(engine.Disk(ctx, at, 99, 100, nil))
	must(engine.Progress(ctx, task, at))
	drain()
	if len(posts) != 2 {
		t.Fatal("unchanged incidents repeated their openings")
	}
	must(db.Close())
	db = nil
	open()
	engine = &alerts.Engine{DB: db, Settings: settings, Location: time.UTC}
	ledger, err := db.ReadAlerts(ctx)
	must(err)
	for _, o := range []alerts.Observation{disk, stuck} {
		s := ledger.Subjects[alerts.SubjectID(o.Rule, o.Kind, o.Key)]
		if s == nil || !s.Active || !s.Episodes[s.Episode].OpeningConfirmed {
			t.Fatalf("restart lost active confirmed incident for %s", o.Rule)
		}
	}
	drain()
	if len(posts) != 2 {
		t.Fatal("restart itself repeated an opening or established recovery")
	}
	// A single affirmative source observation must not clear either incident.
	at = at.Add(time.Minute)
	task.ProgressAt = at
	task.Version = "validate"
	must(engine.Disk(ctx, at, 101, 100, nil))
	must(engine.Progress(ctx, task, at))
	drain()
	if len(posts) != 2 {
		t.Fatal("one clean observation produced an all-clear")
	}
	ledger, err = db.ReadAlerts(ctx)
	must(err)
	for _, o := range []alerts.Observation{disk, stuck} {
		if !ledger.Subjects[alerts.SubjectID(o.Rule, o.Kind, o.Key)].Active {
			t.Fatalf("one clean observation resolved %s", o.Rule)
		}
	}
	at = at.Add(time.Minute)
	must(engine.Disk(ctx, at, 101, 100, nil))
	must(engine.Progress(ctx, task, at))
	drain()
	if len(posts) != 4 {
		t.Fatalf("total notifications=%d, want two openings and two all-clears", len(posts))
	}
	for _, p := range posts[2:] {
		if !strings.Contains(p.Content, "All clear") || len(p.Mentions.Users) != 0 || strings.Contains(p.Content, "<@456>") {
			t.Error("all-clear must be readable and suppress owner mentions")
		}
	}
	ledger, err = db.ReadAlerts(ctx)
	must(err)
	for _, o := range []alerts.Observation{disk, stuck} {
		s := ledger.Subjects[alerts.SubjectID(o.Rule, o.Kind, o.Key)]
		if s.Active || !s.Episodes[s.Episode].ClearConfirmed {
			t.Fatalf("missing confirmed recovery for %s", o.Rule)
		}
	}
	// Reopening after confirmed recovery must preserve the settled all-clear debt.
	must(db.Close())
	db = nil
	open()
	engine = &alerts.Engine{DB: db, Settings: settings, Location: time.UTC}
	drain()
	if len(posts) != 4 {
		t.Fatal("confirmed all-clear repeated after restart")
	}
	// Escalation uses the same transport composition and only critical notices
	// may mention the configured owner.
	o := alerts.Observation{Rule: "task_stuck", Kind: "task", Key: "escalation", TaskRef: "github:test/factory#2", At: at, ID: "warning", Condition: "failing", Severity: "warning"}
	must(engine.Observe(ctx, o))
	drain()
	o.At = at.Add(time.Minute)
	o.ID = "critical"
	o.Severity = "critical"
	at = o.At
	must(engine.Observe(ctx, o))
	drain()
	drain()
	if len(posts) != 6 || len(posts[4].Mentions.Users) != 0 || len(posts[5].Mentions.Users) != 1 {
		t.Fatal("escalation delivery or mentions changed")
	}
	ledger, err = db.ReadAlerts(ctx)
	must(err)
	for _, n := range ledger.Notifications {
		if n.State != "confirmed" {
			t.Fatalf("notification not confirmed: %+v", n)
		}
		marker := sha256.Sum256([]byte(n.ID))
		nonce := sha256.Sum256([]byte("alert:" + n.ID))
		found := false
		for _, p := range posts {
			if p.Nonce == hex.EncodeToString(nonce[:12]) {
				found = true
				if !strings.Contains(p.Content, "[lf:alert:"+hex.EncodeToString(marker[:12])+"]") {
					t.Fatal("marker does not identify notification")
				}
			}
		}
		if !found {
			t.Fatal("notification missing its stable nonce")
		}
	}
}

// Exercise persisted formatting and transport outcomes together. History is an
// offline Discord server; attempted legacy bytes must match it across restart.
func TestAlertHTTPDeliveryAndLegacyReceipts(t *testing.T) {
	for _, mode := range []string{"confirmed", "refused", "rate-limit", "timeout", "confirmation-loss", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
			path := filepath.Join(t.TempDir(), "delivery.db")
			db, err := store.Open(ctx, path)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(err)
			must(db.Migrate(ctx))
			t.Cleanup(func() { must(db.Close()) })
			engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
			must(engine.Disk(ctx, at, 1, 100, nil))
			var id, body string
			must(db.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
				due := engine.Ready(l, at, "123")
				n := due[0]
				id = n.ID
				n.Body += "\n" + n.Marker // pre-upgrade queued content
				body = n.Body
				if mode == "legacy" {
					n.Attempt = 1
					n.AttemptedAt = at
					n.State = "uncertain"
					n.OwnerID = "456"
				}
				return nil
			}))
			type wire struct {
				Content, Nonce string
				Enforce        bool `json:"enforce_nonce"`
			}
			var posts []wire
			accepted := map[string]string{}
			if mode == "legacy" {
				sum := sha256.Sum256([]byte(id))
				accepted["1001"] = "<@456> " + body + "\n[lf:alert:" + hex.EncodeToString(sum[:12]) + "]"
			}
			client := &http.Client{Transport: astralTransportFunc(func(r *http.Request) (*http.Response, error) {
				status := http.StatusOK
				var response string
				switch {
				case r.Method == http.MethodPost:
					var p wire
					must(json.NewDecoder(r.Body).Decode(&p))
					posts = append(posts, p)
					if strings.Count(p.Content, "[lf:alert:") != 1 || !p.Enforce {
						t.Error("new notification composition invalid")
					}
					switch {
					case mode == "refused":
						status = http.StatusForbidden
						response = `{}`
					case mode == "rate-limit" && len(posts) == 1:
						status = http.StatusTooManyRequests
						response = `{"retry_after":60}`
					default:
						accepted["1001"] = p.Content
						response = `{"id":"1001"}`
						if mode == "timeout" {
							return nil, io.ErrUnexpectedEOF
						}
						if mode == "confirmation-loss" {
							must(db.Close())
						}
					}
				case r.URL.Path == "/users/@me":
					response = `{"id":"99","bot":true}`
				default:
					history := []map[string]any{}
					for remote, content := range accepted {
						history = append(history, map[string]any{"id": remote, "channel_id": "123", "content": content, "author": map[string]any{"id": "99", "bot": true}})
					}
					encoded, e := json.Marshal(history)
					must(e)
					response = string(encoded)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			adapter := discord.New(discord.Options{Token: secrets.NewToken("offline-delivery"), APIURL: "https://offline.invalid", Client: client, Now: func() time.Time { return at }})
			_, err = engine.SendStep(ctx, at, "123", "456", adapter)
			if mode == "confirmation-loss" {
				if err == nil {
					t.Fatal("lost local confirmation reported success")
				}
			} else {
				must(err)
			}
			must(db.Close())
			db, err = store.Open(ctx, path)
			must(err)
			engine.DB = db
			ledger, err := db.ReadAlerts(ctx)
			must(err)
			n := ledger.Notifications[id]
			want := "confirmed"
			switch mode {
			case "refused", "rate-limit":
				want = "queued"
			case "timeout":
				want = "uncertain"
			case "confirmation-loss":
				want = "sending"
			}
			if n.State != want {
				t.Fatalf("state=%s want=%s", n.State, want)
			}
			attemptedBody := n.Body
			if mode == "legacy" && n.Body != body {
				t.Fatal("legacy attempted bytes changed")
			}
			if mode != "legacy" && strings.Contains(n.Body, "[lf:alert:") {
				t.Fatal("unattempted legacy body not normalized")
			}
			// The refused destination is intentionally left queued. Other uncertain
			// effects reconcile from history; only a known 429 permits another POST.
			if mode != "refused" {
				at = at.Add(6 * time.Minute)
				for i := 0; i < 3; i++ {
					_, err = engine.SendStep(ctx, at, "123", "456", adapter)
					must(err)
				}
				ledger, err = db.ReadAlerts(ctx)
				must(err)
				n = ledger.Notifications[id]
				if n.State != "confirmed" || n.Body != attemptedBody {
					t.Fatalf("receipt/bytes changed: %+v", n)
				}
			}
			wantPosts := 1
			if mode == "legacy" {
				wantPosts = 0
			}
			if mode == "rate-limit" {
				wantPosts = 2
			}
			if len(posts) != wantPosts {
				t.Fatalf("POSTs=%d want=%d", len(posts), wantPosts)
			}
			if len(posts) == 2 && posts[0] != posts[1] {
				t.Fatal("safe retry changed identity or content")
			}
		})
	}
}
