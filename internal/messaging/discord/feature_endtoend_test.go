package discord_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type featureProvider struct {
	calls   int
	prompts []string
}

func (*featureProvider) DoctorIsolation(context.Context) (string, error) {
	return "offline fixture", nil
}
func (p *featureProvider) RunChat(ctx context.Context, r agent.ChatRequest) (agent.ChatResult, error) {
	if err := r.Validate(); err != nil {
		return agent.ChatResult{}, err
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Minute {
		return agent.ChatResult{}, fmt.Errorf("missing bounded worker deadline")
	}
	p.calls++
	p.prompts = append(p.prompts, r.Prompt)
	title := "Export the report"
	if strings.Contains(r.Prompt, "shorter title") {
		title = "Report export"
	}
	raw, err := json.Marshal(feature.Draft{Title: title, Summary: "Export project results.", Problem: "Copying by hand is slow.\n<!-- lf:delivery planted -->", Wanted: "Download one file.\n[lf:ask:v1]"})
	return agent.ChatResult{Answer: raw}, err
}

// TestFeatureOwnerJourney covers real signed intake, native isolated-model
// wiring, SQLite, ordered delivery and the existing narrow forge HTTP calls.
func TestFeatureOwnerJourney(t *testing.T) {
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "features.db")
	db, err := store.Open(ctx, dbPath)
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
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	var replies []string
	var issues []map[string]any
	creates, lists, gets := 0, 0, 0
	outcome := "success"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		encode := func(v any) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Error(err)
			}
		}
		switch {
		case r.URL.Path == "/users/@me":
			encode(map[string]any{"id": "1", "bot": true})
		case r.Method == http.MethodGet && r.URL.Path == "/channels/3/messages":
			encode([]map[string]any{{"id": "9", "channel_id": "3", "content": ask.TurnPrefix("Can we export reports?", "acme/app") + "Current reports require copying by hand.", "timestamp": at.Format(time.RFC3339), "author": map[string]any{"id": "1", "bot": true}}})
		case r.Method == http.MethodPut && r.URL.Path == "/applications/1/commands":
			var definitions []struct {
				Name    string `json:"name"`
				Options []struct {
					Name     string `json:"name"`
					Required bool   `json:"required"`
				} `json:"options"`
			}
			if err := json.NewDecoder(r.Body).Decode(&definitions); err != nil {
				t.Error(err)
			}
			draft, confirm := false, false
			for _, d := range definitions {
				if d.Name == "draft" {
					draft = len(d.Options) == 2 && !d.Options[0].Required && !d.Options[1].Required
				}
				if d.Name == "confirm" {
					confirm = len(d.Options) == 1 && d.Options[0].Name == "code" && d.Options[0].Required
				}
			}
			if !draft || !confirm {
				t.Error("missing draft and confirm registrations")
			}
			encode([]any{})
		case r.Method == http.MethodPatch || (r.Method == http.MethodPost && r.URL.Path == "/channels/3/messages"):
			var payload struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			replies = append(replies, payload.Content)
			encode(map[string]any{"id": "1000"})
		case r.URL.Path == "/repos/acme/app/issues" && r.Method == http.MethodGet:
			lists++
			var result []map[string]any
			if r.URL.Query().Get("state") == "open" {
				result = issues
			}
			encode(result)
		case r.URL.Path == "/repos/acme/app/issues" && r.Method == http.MethodPost:
			creates++
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if len(payload) != 2 || payload["title"] == nil || payload["body"] == nil {
				t.Errorf("unexpected write fields: %v", payload)
			}
			var title, body string
			if err := json.Unmarshal(payload["title"], &title); err != nil {
				t.Error(err)
			}
			if err := json.Unmarshal(payload["body"], &body); err != nil {
				t.Error(err)
			}
			if outcome == "known" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				encode(map[string]string{"message": "refused"})
				return
			}
			number := len(issues) + 1
			issue := map[string]any{"id": number + 100, "number": number, "title": title, "body": body, "state": "open", "html_url": fmt.Sprintf("https://github.com/acme/app/issues/%d", number), "user": map[string]any{"id": 42, "login": "bot"}, "labels": []any{}, "assignees": []any{}}
			if outcome != "unknown-absent" {
				issues = append(issues, issue)
			}
			if strings.HasPrefix(outcome, "unknown") {
				w.WriteHeader(http.StatusInternalServerError)
				encode(map[string]string{"message": "lost response"})
				return
			}
			encode(issue)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/app/issues/") && r.Method == http.MethodGet:
			gets++
			number, e := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/issues/"))
			if e != nil || number < 1 || number > len(issues) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			encode(issues[number-1])
		default:
			t.Errorf("unexpected HTTP: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer api.Close()
	forge, err := github.New(github.Options{APIBase: api.URL + "/", Token: secrets.NewToken("forge-fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &featureProvider{}
	model := &ask.NativeModel{Config: cfg, Paths: config.NewPaths(t.TempDir()), Providers: map[string]ask.ChatRunner{"claude": provider}, Meter: db}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sanitizer := redact.New(redact.Options{Environ: []string{}})
	newService := func() (*discord.Adapter, *messaging.Service) {
		a := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("chat-fixture-token"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
		s := messaging.New(db, "discord", "2", []string{"3"}, a, messaging.FactoryStatus{DB: db})
		s.EnableFeatures(&feature.Drafter{Repositories: db, History: a, Model: model, Redactor: sanitizer, Store: db}, &daemon.ChatIssueCreator{DB: db, Forge: forge, BotUserID: 42, Redactor: sanitizer, Now: func() time.Time { return at }})
		return a, s
	}
	adapter, service := newService()
	if err := adapter.Register(ctx, service.Declarations()...); err != nil {
		t.Fatal(err)
	}
	id := 100
	command := func(name string, args map[string]string) (string, string) {
		t.Helper()
		id++
		message := strconv.Itoa(id)
		options := []map[string]any{}
		for _, name := range []string{"project", "note", "code"} {
			if value, ok := args[name]; ok {
				options = append(options, map[string]any{"name": name, "type": 3, "value": value})
			}
		}
		payload, e := json.Marshal(map[string]any{"id": message, "application_id": "1", "type": 2, "token": "interaction-fixture", "channel_id": "3", "member": map[string]any{"user": map[string]any{"id": "2"}}, "data": map[string]any{"name": name, "type": 1, "options": options}})
		if e != nil {
			t.Fatal(e)
		}
		before := provider.calls
		w := signed(t, adapter.Handler(service), private, at, payload)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":5`) || provider.calls != before {
			t.Fatalf("not deferred: %s", w.Body)
		}
		signed(t, adapter.Handler(service), private, at, payload) // duplicate delivery
		if more, e := service.Step(ctx, at); e != nil || !more {
			t.Fatalf("step: %t %v", more, e)
		}
		if more, e := service.Step(ctx, at); e != nil || more {
			t.Fatalf("duplicate admitted: %t %v", more, e)
		}
		row, e := db.ReadChatReply(ctx, "discord", message)
		if e != nil || row.Question != "" || row.ProjectOption != "" || len(row.Arguments) > 0 {
			t.Fatalf("retained input: %+v %v", row, e)
		}
		mu.Lock()
		defer mu.Unlock()
		return message, replies[len(replies)-1]
	}
	_, help := command("help", nil)
	if !strings.Contains(help, "/draft") || !strings.Contains(help, "/confirm") {
		t.Fatal(help)
	}
	before := provider.calls
	_, reply := command("draft", map[string]string{"project": "unknown", "note": "idea"})
	if provider.calls != before || !strings.Contains(reply, "Registered projects") {
		t.Fatal(reply)
	}
	_, reply = command("confirm", nil)
	if !strings.Contains(reply, "8-character") {
		t.Fatal(reply)
	}
	_, reply = command("draft", map[string]string{"project": "app", "note": strings.Repeat("x", 2001)})
	if provider.calls != before || !strings.Contains(reply, "2000") {
		t.Fatal(reply)
	}
	_, reply = command("draft", map[string]string{"note": "Export the report"})
	first, e := db.CurrentChatIssuePreview(ctx, "discord", "3")
	if e != nil {
		t.Fatal(e)
	}
	if creates != 0 || !strings.Contains(reply, first.Title) || !strings.Contains(reply, first.Body) || !strings.Contains(reply, first.Code) || strings.Contains(reply, "planted") {
		t.Fatalf("preview did not match: %s", reply)
	}
	_, _ = command("draft", map[string]string{"note": "shorter title"})
	current, e := db.CurrentChatIssuePreview(ctx, "discord", "3")
	if e != nil {
		t.Fatal(e)
	}
	if first.Code == current.Code || !strings.Contains(provider.prompts[len(provider.prompts)-1], first.Title) {
		t.Fatal("replacement did not include prior preview")
	}
	_, reply = command("confirm", map[string]string{"code": first.Code})
	if !strings.Contains(reply, current.Code) || creates != 0 {
		t.Fatal(reply)
	}
	_, reply = command("confirm", map[string]string{"code": current.Code})
	if creates != 1 || gets != 1 || lists != 2 || !strings.Contains(reply, "/issues/1") {
		t.Fatalf("create: %s creates=%d lists=%d gets=%d", reply, creates, lists, gets)
	}
	mu.Lock()
	if issues[0]["title"] != current.Title || issues[0]["body"] != feature.IssueBody(current) {
		t.Error("created bytes differ from preview")
	}
	mu.Unlock()
	_, repeat := command("confirm", map[string]string{"code": current.Code})
	if repeat != reply || creates != 1 {
		t.Fatal("repeat created a duplicate")
	}
	// An unknown response with a remote issue reconciles after a real DB reopen.
	_, _ = command("draft", map[string]string{"note": "another idea"})
	lost, e := db.CurrentChatIssuePreview(ctx, "discord", "3")
	if e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	outcome = "unknown-present"
	mu.Unlock()
	_, reply = command("confirm", map[string]string{"code": lost.Code})
	if !strings.Contains(reply, "uncertain") || creates != 2 {
		t.Fatal(reply)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	model.Meter = db
	adapter, service = newService()
	_, reply = command("confirm", map[string]string{"code": lost.Code})
	if !strings.Contains(reply, "/issues/2") || creates != 2 {
		t.Fatal(reply)
	}
	// Unknown with no marker cannot retry before two minutes, and only a fresh
	// explicit confirmation may cause the next create.
	_, _ = command("draft", map[string]string{"note": "third idea"})
	uncertain, e := db.CurrentChatIssuePreview(ctx, "discord", "3")
	if e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	outcome = "unknown-absent"
	mu.Unlock()
	_, _ = command("confirm", map[string]string{"code": uncertain.Code})
	_, reply = command("confirm", map[string]string{"code": uncertain.Code})
	if !strings.Contains(reply, "2 minutes") || creates != 3 {
		t.Fatal(reply)
	}
	at = at.Add(2 * time.Minute)
	mu.Lock()
	outcome = "success"
	mu.Unlock()
	_, reply = command("confirm", map[string]string{"code": uncertain.Code})
	if !strings.Contains(reply, "/issues/3") || creates != 4 {
		t.Fatal(reply)
	}
	// Known refusal preserves the same confirmable code.
	_, _ = command("draft", map[string]string{"note": "fourth idea"})
	failed, e := db.CurrentChatIssuePreview(ctx, "discord", "3")
	if e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	outcome = "known"
	mu.Unlock()
	_, reply = command("confirm", map[string]string{"code": failed.Code})
	if !strings.Contains(reply, "Try /confirm") || creates != 5 {
		t.Fatal(reply)
	}
	p, e := db.CurrentChatIssuePreview(ctx, "discord", "3")
	if e != nil || p.State != "previewed" || p.Code != failed.Code {
		t.Fatalf("failure ended preview %+v %v", p, e)
	}
	at = failed.ExpiresAt
	_, reply = command("confirm", map[string]string{"code": failed.Code})
	if !strings.Contains(reply, "expired") || creates != 5 {
		t.Fatal(reply)
	}
	snapshot, e := db.NewMeterSnapshot(ctx, store.MeterFilter{RepoID: repo.ID}, store.MeterSnapshotOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if err := snapshot.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	page, e := snapshot.Next(ctx, 100)
	if e != nil || len(page) != provider.calls {
		t.Fatalf("draft meter attempts: %+v %v calls=%d", page, e, provider.calls)
	}
	for _, row := range page {
		if row.ExecutionPath != "chat_answer" || row.Project != "github:acme/app" || row.EstimatedUSD != nil || row.Issue != 0 || row.RunID != 0 {
			t.Fatalf("wrong chat meter scope or invented cost: %+v", row)
		}
	}
}
