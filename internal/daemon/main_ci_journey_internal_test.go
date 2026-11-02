package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/cimonitor"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestMainCIAlertJourney crosses the production Actions reader, daemon observer,
// durable alert journal and Discord HTTP transport without external requests.
func TestMainCIAlertJourney(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	created := at.Add(-time.Hour)
	firstCreated := created
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	runID, runNumber := int64(101), int64(10)
	conclusion := "failure"
	unavailable := false
	type payload struct {
		Content  string `json:"content"`
		Nonce    string `json:"nonce"`
		Enforce  bool   `json:"enforce_nonce"`
		Mentions struct {
			Parse, Users, Roles []string
			RepliedUser         bool `json:"replied_user"`
		} `json:"allowed_mentions"`
	}
	var posts []payload
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/channels/123/messages" {
			if r.Method != http.MethodPost {
				t.Errorf("unexpected Discord method %s", r.Method)
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
				t.Error("notification lacks stable enforced identity")
			}
			if len(p.Mentions.Parse) != 0 || len(p.Mentions.Users) != 0 || len(p.Mentions.Roles) != 0 || p.Mentions.RepliedUser {
				t.Errorf("CI warning or all-clear permits mentions: %+v", p.Mentions)
			}
			for _, unsafe := range []string{"@everyone", "<@456>", "offline-ci-secret"} {
				if strings.Contains(p.Content, unsafe) {
					t.Errorf("unsafe notification text %q", unsafe)
				}
			}
			posts = append(posts, p)
			_, _ = fmt.Fprintf(w, `{"id":"%d"}`, 1000+len(posts))
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("CI integration attempted mutation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if unavailable {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
			return
		}
		run := map[string]any{"id": runID, "run_number": runNumber, "run_attempt": 1, "workflow_id": 7, "head_branch": "main", "head_sha": strings.Repeat("a", 40), "status": "completed", "conclusion": conclusion, "created_at": created.Format(time.RFC3339), "updated_at": created.Add(time.Minute).Format(time.RFC3339), "html_url": fmt.Sprintf("https://github.com/acme/factory/actions/runs/%d", runID)}
		firstRun := map[string]any{}
		for k, v := range run {
			firstRun[k] = v
		}
		firstRun["id"], firstRun["run_number"], firstRun["conclusion"] = int64(101), int64(10), "failure"
		firstRun["created_at"], firstRun["updated_at"] = firstCreated.Format(time.RFC3339), firstCreated.Add(time.Minute).Format(time.RFC3339)
		firstRun["html_url"] = "https://github.com/acme/factory/actions/runs/101"
		switch {
		case r.URL.Path == "/repos/acme/factory":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "factory", "full_name": "acme/factory", "owner": map[string]any{"login": "acme"}})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/factory/actions/workflows/") && !strings.HasSuffix(r.URL.Path, "/runs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "name": "Check @everyone <@456>", "path": ".github/workflows/check.yml", "state": "active"})
		case r.URL.Path == "/repos/acme/factory/actions/workflows/7/runs":
			if r.URL.Query().Get("branch") != "main" {
				t.Error("run scan omitted explicit main branch")
			}
			bounds := strings.Split(r.URL.Query().Get("created"), "..")
			if len(bounds) != 2 {
				t.Error("run scan omitted bounded creation range")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			start, startErr := time.Parse(time.RFC3339, bounds[0])
			end, endErr := time.Parse(time.RFC3339, bounds[1])
			if startErr != nil || endErr != nil {
				t.Errorf("invalid creation range: %q", r.URL.Query().Get("created"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			runs := []any{}
			if !created.Before(start) && !created.After(end) {
				runs = append(runs, run)
			}
			if runID != 101 && !firstCreated.Before(start) && !firstCreated.After(end) {
				runs = append(runs, firstRun)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(runs), "workflow_runs": runs})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/factory/actions/runs/"):
			if strings.HasSuffix(r.URL.Path, "/101") {
				run = firstRun
			}
			_ = json.NewEncoder(w).Encode(run)
		default:
			t.Errorf("unexpected API path: %s", r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()
	client, err := github.New(github.Options{APIBase: api.URL, Token: secrets.NewToken("offline-ci-secret"), HTTPClient: api.Client(), Now: func() time.Time { return at }})
	must(err)
	adapter := discord.New(discord.Options{Token: secrets.NewToken("offline-ci-secret"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	path := filepath.Join(t.TempDir(), "main-ci.db")
	var db *store.DB
	var d *Daemon
	settings := config.DefaultAlerts()
	settings.QuietHours.Enabled = false
	open := func() {
		t.Helper()
		var err error
		db, err = store.Open(ctx, path)
		must(err)
		must(db.Migrate(ctx))
		engine := &alerts.Engine{DB: db, Settings: settings, Location: time.UTC}
		d = &Daemon{db: db, alerts: engine, log: slog.New(slog.DiscardHandler)}
		d.ciMonitor = &cimonitor.Observer{Reader: client, Gate: &cimonitor.Gate{Store: db, Key: "offline-ci", Now: func() time.Time { return at }}, Engine: engine, Origin: "https://api.github.com", Target: config.CITarget{Repository: "acme/factory", Branch: "main", Workflow: ".github/workflows/check.yml"}, Now: func() time.Time { return at }}
		must(d.ciMonitor.Reset(ctx, at))
	}
	open()
	t.Cleanup(func() {
		if db != nil {
			must(db.Close())
		}
	})
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(posts) }
	drain := func() {
		t.Helper()
		for i := 0; i < 10; i++ {
			sent, err := d.alerts.SendStep(ctx, at, "123", "456", adapter)
			must(err)
			if !sent {
				return
			}
		}
		t.Fatal("delivery never became idle")
	}
	poll := func() {
		t.Helper()
		err := d.observeMainCI(ctx, at)
		mu.Lock()
		wantUnavailable := unavailable
		mu.Unlock()
		if wantUnavailable {
			if err == nil {
				t.Fatal("failed Actions read was reported successful")
			}
		} else {
			must(err)
		}
		drain()
	}
	subject := func(rule string) *store.AlertSubject {
		t.Helper()
		l, err := db.ReadAlerts(ctx)
		must(err)
		for _, s := range l.Subjects {
			if s.Rule == rule {
				return s
			}
		}
		t.Fatalf("no subject for %s", rule)
		return nil
	}
	assertState := func(rule string, active, opening, cleared bool) {
		t.Helper()
		s := subject(rule)
		if s.Active != active || s.Episodes[s.Episode].OpeningConfirmed != opening || s.Episodes[s.Episode].ClearConfirmed != cleared {
			t.Fatalf("%s state=%+v episode=%+v", rule, s, s.Episodes[s.Episode])
		}
	}
	poll()
	if count() != 1 {
		t.Fatalf("failure openings=%d, want one", count())
	}
	assertState("main_red", true, true, false)
	at = at.Add(5 * time.Minute)
	poll()
	if count() != 1 {
		t.Fatal("repeated failed observation repeated opening")
	}
	// A failed read has its own readiness warning and cannot erase the red run.
	mu.Lock()
	unavailable = true
	mu.Unlock()
	at = at.Add(5 * time.Minute)
	poll()
	assertState("main_red", true, true, false)
	assertState("ci_monitoring_unavailable", true, true, false)
	if count() != 2 {
		t.Fatalf("monitoring-loss messages=%d, want two total", count())
	}
	must(db.Close())
	db = nil
	open()
	drain()
	assertState("main_red", true, true, false)
	assertState("ci_monitoring_unavailable", true, true, false)
	if count() != 2 {
		t.Fatal("restart repeated opening or established recovery")
	}
	mu.Lock()
	unavailable = false
	mu.Unlock()
	// Complete red reads recover monitoring alone after two scheduled polls.
	at = at.Add(5 * time.Minute)
	poll()
	assertState("ci_monitoring_unavailable", true, true, false)
	at = at.Add(5 * time.Minute)
	poll()
	assertState("ci_monitoring_unavailable", false, true, true)
	assertState("main_red", true, true, false)
	if count() != 3 {
		t.Fatalf("readiness recovery messages=%d, want three total", count())
	}
	mu.Lock()
	runID = 102
	runNumber = 11
	conclusion = "success"
	created = at.Add(time.Minute)
	mu.Unlock()
	at = at.Add(5 * time.Minute)
	poll()
	assertState("main_red", true, true, false)
	if count() != 3 {
		t.Fatal("one later success observation sent an all-clear")
	}
	poll()
	assertState("main_red", true, true, false)
	if count() != 3 {
		t.Fatal("duplicate callback counted as a second clean observation")
	}
	at = at.Add(5 * time.Minute)
	poll()
	assertState("main_red", false, true, true)
	if count() != 4 {
		t.Fatalf("notification count=%d, want two openings and two all-clears", count())
	}
	mu.Lock()
	for _, p := range posts[2:] {
		if !strings.Contains(p.Content, "All clear") {
			t.Errorf("missing readable all-clear: %q", p.Content)
		}
	}
	mu.Unlock()
	must(db.Close())
	db = nil
	open()
	drain()
	if count() != 4 {
		t.Fatal("restart repeated settled recovery")
	}
}

func TestMainCIControlsKeepReadinessIndependent(t *testing.T) {
	d, _, at := alertDaemonFixture(t)
	e := d.alerts
	e.Settings.QuietHours.Enabled = false
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := e.Command(ctx, "mute-main", "mute main 30m", at)
	must(err)
	l, err := d.db.ReadAlerts(ctx)
	must(err)
	if !l.Controls.RuleMutes["main_red"].Equal(at.Add(30*time.Minute)) || !l.Controls.RuleMutes["ci_monitoring_unavailable"].IsZero() {
		t.Fatal("main alias changed readiness mute")
	}
	_, err = e.Command(ctx, "mute-readiness", "mute ci_monitoring_unavailable 1h", at)
	must(err)
	_, err = e.Command(ctx, "unmute-main", "unmute main_red", at)
	must(err)
	l, err = d.db.ReadAlerts(ctx)
	must(err)
	if !l.Controls.RuleMutes["main_red"].IsZero() || !l.Controls.RuleMutes["ci_monitoring_unavailable"].Equal(at.Add(time.Hour)) {
		t.Fatal("canonical main control changed readiness mute")
	}
}

func TestMainCISharedWarningDeliveryControls(t *testing.T) {
	for _, rule := range []string{"main_red", "ci_monitoring_unavailable"} {
		t.Run(rule, func(t *testing.T) {
			d, _, at := alertDaemonFixture(t)
			e := d.alerts
			e.Settings.QuietHours.Enabled = false
			ctx := t.Context()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var kinds []string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p struct {
					Content  string
					Mentions struct {
						Parse, Users, Roles []string
						RepliedUser         bool `json:"replied_user"`
					} `json:"allowed_mentions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if len(p.Mentions.Parse)+len(p.Mentions.Users)+len(p.Mentions.Roles) != 0 || p.Mentions.RepliedUser {
					t.Error("CI notification enabled a mention")
				}
				for _, s := range []string{"@everyone", "<@456>", "ci-control-secret"} {
					if strings.Contains(p.Content, s) {
						t.Errorf("untrusted notification content %q", s)
					}
				}
				mu.Lock()
				defer mu.Unlock()
				kind := "opening"
				if strings.Contains(p.Content, "All clear") {
					kind = "clear"
				}
				if strings.Contains(p.Content, "digest") {
					kind = "digest"
				}
				kinds = append(kinds, kind)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"id":"%d"}`, 1000+len(kinds))
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{Token: secrets.NewToken("ci-control-secret"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
			sentCount := func() int { mu.Lock(); defer mu.Unlock(); return len(kinds) }
			drain := func() {
				t.Helper()
				for i := 0; i < 10; i++ {
					sent, err := e.SendStep(ctx, at, "123", "456", adapter)
					must(err)
					if !sent {
						return
					}
				}
				t.Fatal("notification queue did not drain")
			}
			observe := func(condition string) {
				t.Helper()
				must(e.Observe(ctx, alerts.Observation{Rule: rule, Kind: "workflow", Key: "acme/factory:7", At: at, ID: fmt.Sprintf("%s:%d", condition, at.UnixNano()), Condition: condition, Severity: "warning", Summary: "CI @everyone <@456> failed", Action: "Inspect the workflow run", RecoveryEvidence: "later-success", EvidenceAt: at}))
			}
			_, err := e.Command(ctx, "mute-ci", "mute "+rule+" 30m", at)
			must(err)
			observe("failing")
			drain()
			if sentCount() != 0 {
				t.Fatal("muted warning was delivered")
			}
			e.Settings.Rules[rule] = false
			at = at.Add(31 * time.Minute)
			drain()
			if sentCount() != 0 {
				t.Fatal("disabled queued warning was delivered after mute expired")
			}
			e.Settings.Rules[rule] = true
			drain()
			if sentCount() != 1 {
				t.Fatal("mute expiry lost still-active warning")
			}
			// Disablement also suppresses owed all-clears; observation keeps running.
			e.Settings.Rules[rule] = false
			at = at.Add(5 * time.Minute)
			observe("clean")
			at = at.Add(5 * time.Minute)
			observe("clean")
			drain()
			if sentCount() != 1 {
				t.Fatal("disabled rule sent an all-clear")
			}
			e.Settings.Rules[rule] = true
			drain()
			if sentCount() != 2 {
				t.Fatal("reenabled rule lost confirmed opening's all-clear")
			}
			mu.Lock()
			if kinds[1] != "clear" {
				t.Error("second notification was not an all-clear")
			}
			mu.Unlock()
		})
	}
}

func TestMainCIMonitorStartsWithoutSlotsAndJoinsCanceledHTTP(t *testing.T) {
	t.Parallel()
	d, _, at := alertDaemonFixture(t)
	d.alertClock = func() time.Time { return at }
	if !d.pool.acquire(1) {
		t.Fatal("fill execution pool")
	}
	defer d.pool.release(1)
	entered := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/factory" {
			t.Errorf("unexpected observer request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		entered <- struct{}{}
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	defer api.Close()
	client, err := github.New(github.Options{APIBase: api.URL, Token: secrets.NewToken("offline-ci-lifecycle"), HTTPClient: api.Client(), Now: d.alertClock})
	if err != nil {
		t.Fatal(err)
	}
	d.ciMonitor = &cimonitor.Observer{
		Reader: client, Engine: d.alerts,
		Gate:   &cimonitor.Gate{Store: d.db, Key: "ci-lifecycle", Now: d.alertClock},
		Origin: "https://api.github.com", Now: d.alertClock,
		Target: config.CITarget{Repository: "acme/factory", Branch: "main", Workflow: ".github/workflows/check.yml"},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var joined sync.WaitGroup
	d.serveMainCI(ctx, &joined)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("startup CI observation did not request Actions access while execution slots were full")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("observer cancellation did not reach its blocked HTTP request")
	}
	done := make(chan struct{})
	// The observer owns a bounded joined worker; the rendezvous verifies that
	// cancellation completes cleanup without waiting for the polling interval.
	go func() { joined.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CI observer did not join after canceling its HTTP request")
	}
	used, total := d.pool.capacity()
	if used != 1 || total != 1 {
		t.Fatalf("CI observation changed execution capacity: %d/%d", used, total)
	}
	transport, err := d.db.CITransport(t.Context(), "ci-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if transport.Lease != "" {
		t.Fatal("observer shutdown retained its CI transport lease")
	}
}
