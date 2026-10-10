package discord_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestMeterOwnerJourney follows signed slash commands through admission, the
// durable inbox and a fake Discord API, using real frozen usage and prices.
func TestMeterOwnerJourney(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "meter.db"))
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
	model := "chat-fixture"
	rates := []store.MeterPriceRate{}
	for _, dim := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, store.MeterPriceRate{CodingAgent: "codex", Model: model, Dimension: dim, USDPerMillion: "1"})
	}
	if _, err := db.SetMeterPrices(ctx, "codex", model, rates); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), name), Forge: store.ForgeGitHub, Owner: "acme", Name: name, RemoteURL: "git@github.com:acme/" + name + ".git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "work")})
		if err != nil {
			t.Fatal(err)
		}
		for issue := 1; issue <= 2; issue++ {
			session, err := db.GetOrCreateSession(ctx, repo, issue)
			if err != nil {
				t.Fatal(err)
			}
			seg, err := db.GetOrCreateSegment(ctx, session.ID, 1, "provider-session", store.AgentCodex)
			if err != nil {
				t.Fatal(err)
			}
			run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentCodex, TriggerCommentID: int64(issue), LogPath: func(n int) string { return fmt.Sprintf("/logs/%d.jsonl", n) }})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartMeterAttempt(ctx, run.ID, "codex")
			if err != nil {
				t.Fatal(err)
			}
			for invocation := 0; invocation < 12; invocation++ {
				id := fmt.Sprintf("invocation-%02d", invocation)
				parent := "invocation-00"
				if invocation == 0 {
					parent = ""
				}
				if err := db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attempt.ID, ParentID: parent, ID: id, SourceID: "fixture", SourceInvocationID: id, Status: "complete"}); err != nil {
					t.Fatal(err)
				}
				n, zero := int64(issue), int64(0)
				observation := agent.UsageObservation{AttemptID: attempt.ID, SourceID: "fixture", EventKey: id, InvocationID: id, ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &n, Output: &zero, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete}}
				if err := db.RecordMeterEvent(ctx, attempt.ID, "codex", agent.UsageEvent{Observation: &observation}); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.FinishMeterAttempt(ctx, attempt.ID, "success", "complete", "fixture", "fixture", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	at := time.Now().UTC().Truncate(time.Second).Add(time.Minute)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var replies []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			var data struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			replies = append(replies, data.Content)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("fake"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	service := messaging.New(db, "discord", "2", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
	if err := adapter.Register(ctx, service.Declarations()...); err != nil {
		t.Fatal(err)
	}
	handler := adapter.Handler(service)
	nextID := 100
	command := func(name string, options ...map[string]any) string {
		t.Helper()
		nextID++
		payload := map[string]any{"id": strconv.Itoa(nextID), "application_id": "1", "type": 2, "token": "fake-interaction", "channel_id": "3", "member": map[string]any{"user": map[string]any{"id": "2"}}, "data": map[string]any{"name": name, "type": 1, "options": options}}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		before := len(replies)
		w := signed(t, handler, private, at, encoded)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":5`) {
			t.Fatalf("ack: %d %s", w.Code, w.Body)
		}
		if len(replies) != before {
			t.Fatal("report delivered before deferred acknowledgement")
		}
		more, err := service.Step(ctx, at)
		if err != nil || !more {
			t.Fatalf("delivery: %t %v", more, err)
		}
		if len(replies) != before+1 {
			t.Fatalf("expected one reply, got %d", len(replies)-before)
		}
		reply := replies[before]
		if utf8.RuneCountInString(reply) > 2000 {
			t.Fatalf("reply exceeds Discord limit: %d", utf8.RuneCountInString(reply))
		}
		return reply
	}
	text := func(name, value string) map[string]any {
		return map[string]any{"name": name, "type": 3, "value": value}
	}
	integer := func(name string, value int) map[string]any {
		return map[string]any{"name": name, "type": 4, "value": value}
	}
	for _, scope := range []struct {
		project string
		task    int
	}{{"", 0}, {"acme/one", 0}, {"acme/one", 1}} {
		opts := []map[string]any{}
		if scope.project != "" {
			opts = append(opts, text("project", scope.project))
		}
		if scope.task != 0 {
			opts = append(opts, integer("task", scope.task))
		}
		reply := command("meter", opts...)
		view, err := meterview.Read(ctx, db, meterview.Request{Project: scope.project, Task: int64(scope.task)})
		if err != nil {
			t.Fatal(err)
		}
		if view.Summary.Tokens.InputUncached == nil || view.Summary.KnownTokens.InputUncached == nil {
			t.Fatalf("fixture missing tokens: %+v; reply %s", view.Summary, reply)
		}
		want := fmt.Sprintf("input_uncached: %d; known subtotal: %d", *view.Summary.Tokens.InputUncached, *view.Summary.KnownTokens.InputUncached)
		if !strings.Contains(reply, want) || !strings.Contains(strings.ToLower(reply), "complete local estimate") || !strings.Contains(reply, "daemon helper overhead excluded") {
			t.Fatalf("shared CLI totals or coverage missing: %s; want %s", reply, want)
		}
	}
	filtered := command("meter", text("project", "acme/one"), text("mode", "implement"), text("agent", "codex"), text("model", model), text("from", "2000-01-01"), text("to", "2100-01-01"))
	if !strings.Contains(filtered, "input_uncached: 36;") {
		t.Fatal(filtered)
	}
	details := []map[string]any{text("project", "acme/one"), integer("task", 1), {"name": "details", "type": 5, "value": true}}
	first := command("meter", details...)
	if !strings.Contains(first, "page 1 of ") || !strings.Contains(first, "next: /meter project:acme/one task:1 details:true page:2") {
		t.Fatalf("missing next-page command: %s", first)
	}
	second := command("meter", append(details, integer("page", 2))...)
	if !strings.Contains(second, "page 2 of ") || !strings.Contains(second, "invocation") {
		t.Fatalf("missing paged details: %s", second)
	}
	out := command("meter", append(details, integer("page", 999))...)
	if !strings.Contains(out, "last valid page is") {
		t.Fatal(out)
	}
	for _, opts := range [][]map[string]any{{integer("task", 1)}, {text("project", "acme/missing")}, {text("from", "bad-date")}} {
		reply := command("meter", opts...)
		if strings.Contains(reply, "Price revisions:") || strings.Contains(reply, "\n") {
			t.Fatalf("expected short argument error: %s", reply)
		}
	}
	priceReply := command("prices", text("agent", "codex"), text("model", model))
	catalogue, err := meterview.Prices(ctx, db, "codex", model)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(priceReply, strings.Join(meterview.PriceLines(catalogue), "\n")) {
		t.Fatal(priceReply)
	}
	help := command("help")
	if !strings.Contains(help, "/meter") || !strings.Contains(help, "/prices") {
		t.Fatal(help)
	}
}
