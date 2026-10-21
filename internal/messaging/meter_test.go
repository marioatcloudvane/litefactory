package messaging_test

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestMeterAndPricesReaders runs the meter, prices and help readers the way the
// conversation does, using real frozen usage and prices.
func TestMeterAndPricesReaders(t *testing.T) {
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
	if _, err := db.ReservePlanStory(ctx, "github:acme/one#1", "child"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindPlanStoryIssue(ctx, "github:acme/one#1", "child", 2, 2); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second).Add(time.Minute)
	service := messaging.New(db, "discord", "2", []string{"3"}, nil, messaging.FactoryStatus{DB: db})
	type option struct{ name, value string }
	command := func(name string, options ...option) string {
		t.Helper()
		args := map[string]string{}
		for _, o := range options {
			args[o.name] = o.value
		}
		reply, _, err := service.RunCommand(ctx, messaging.Message{Transport: "discord", ID: "100", Channel: "3", Sender: "2", Command: name, Arguments: args}, at)
		if err != nil {
			t.Fatal(err)
		}
		if utf8.RuneCountInString(reply) > 2000 {
			t.Fatalf("reply exceeds Discord limit: %d", utf8.RuneCountInString(reply))
		}
		return reply
	}
	text := func(name, value string) option { return option{name, value} }
	integer := func(name string, value int) option { return option{name, strconv.Itoa(value)} }
	for _, scope := range []struct {
		project string
		task    int
	}{{"", 0}, {"acme/one", 0}, {"acme/one", 1}} {
		opts := []option{}
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
	family := command("meter", text("project", "acme/one"), integer("task", 1))
	for _, want := range []string{"input_uncached: 36;", "#1: Complete local estimate USD:", "#2: Complete local estimate USD:"} {
		if !strings.Contains(family, want) {
			t.Fatalf("family breakdown missing %q: %s", want, family)
		}
	}
	filtered := command("meter", text("project", "acme/one"), text("mode", "implement"), text("agent", "codex"), text("model", model), text("from", "2000-01-01"), text("to", "2100-01-01"))
	if !strings.Contains(filtered, "input_uncached: 36;") {
		t.Fatal(filtered)
	}
	details := []option{text("project", "acme/one"), integer("task", 1), {"details", "true"}}
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
	for _, opts := range [][]option{{integer("task", 1)}, {text("project", "acme/missing")}, {text("from", "bad-date")}} {
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
	if !strings.Contains(help, "meter") || !strings.Contains(help, "prices") || strings.Contains(help, "/meter") {
		t.Fatal(help)
	}
}
