package alerts_test

import (
	"encoding/json"
	"fmt"
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
}
