package discord_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestTaskNoticeJourney drives durable factory records through the collector,
// real outbox and adapter. The fake API accepts a post but loses its response;
// a reopened database retries the same nonce without adding a second post.
type titledIssues struct{}

func (titledIssues) GetIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	return github.Issue{Number: n, Title: "Pick a release"}, nil
}

func TestTaskNoticeJourney(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "notice.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Migrate(ctx))
	at := time.Now().UTC().Truncate(time.Second)
	var mu sync.Mutex
	visible := map[string]string{}
	attempts := map[string]int{}
	lost := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/channels/3/messages" || r.Header.Get("Authorization") != "Bot notice-test-token" {
			t.Error("invalid channel-post request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Content  string `json:"content"`
			Nonce    string `json:"nonce"`
			Enforce  bool   `json:"enforce_nonce"`
			Flags    int    `json:"flags"`
			Mentions struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.Nonce == "" || !body.Enforce || len(body.Mentions.Parse) != 0 {
			t.Error("nonce or mention policy missing")
		}
		if body.Flags&4 == 0 {
			t.Error("notice post does not suppress embeds")
		}
		mu.Lock()
		defer mu.Unlock()
		attempts[body.Nonce]++
		visible[body.Nonce] = body.Content
		if !lost {
			lost = true
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{ApplicationID: "1", Token: secrets.NewToken("notice-test-token"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	issues := titledIssues{}
	collector := &messaging.NoticeCollector{DB: db, Transport: "discord", Issues: issues}
	must(collector.Collect(ctx, at)) // Off means even positions are absent.
	if _, _, err := db.ListNoticeRecords(ctx, "discord", "run_starts", at); err == nil {
		t.Fatal("disabled notices wrote a position")
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "repo", RemoteURL: "git@github.com:acme/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	must(err)
	session, err := db.GetOrCreateSession(ctx, repo, 596)
	must(err)
	seg, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
	must(err)
	mode := "plan"
	allocate := func(trigger int64) store.Run {
		t.Helper()
		r, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: mode, Agent: "codex", TriggerCommentID: trigger, StartedAt: at, LogPath: func(n int) string { return fmt.Sprintf("/logs/%d.jsonl", n) }})
		must(err)
		return r
	}
	history := allocate(1)
	zero := int64(0)
	_, err = db.FinishRun(ctx, history.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero, FinishedAt: at})
	must(err)
	collector.Channel = "3"
	must(collector.Collect(ctx, at))
	if _, err := db.ReadChatNotice(ctx, "discord", fmt.Sprintf("run:%d:start", history.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("history was posted: %v", err)
	}
	// One mode change, then two further runs in the same mode: one start.
	mode = "implement"
	runs := []store.Run{allocate(2), allocate(3), allocate(4)}
	must(collector.Collect(ctx, at))
	service := messaging.New(db, "discord", "2", []string{"3"}, adapter, nil)
	more, err := service.NoticeStep(ctx, at)
	must(err)
	if !more {
		t.Fatal("no start notice")
	}
	// Restart with a possibly accepted post and durable pending sources.
	must(db.Close())
	db, err = store.Open(ctx, path)
	must(err)
	collector = &messaging.NoticeCollector{DB: db, Transport: "discord", Channel: "3", Issues: issues}
	service = messaging.New(db, "discord", "2", []string{"3"}, adapter, nil)
	must(collector.Collect(ctx, at.Add(time.Second)))
	// Add measured usage to only the successful run.
	model := "notice-fixture"
	_, err = db.SetMeterPrices(ctx, "codex", model, []store.MeterPriceRate{{CodingAgent: "codex", Model: model, Dimension: "input_uncached", USDPerMillion: "1"}, {CodingAgent: "codex", Model: model, Dimension: "output", USDPerMillion: "1"}, {CodingAgent: "codex", Model: model, Dimension: "cache_read", USDPerMillion: "1"}, {CodingAgent: "codex", Model: model, Dimension: "cache_write", USDPerMillion: "1"}})
	must(err)
	meter, err := db.StartMeterAttempt(ctx, runs[0].ID, "codex")
	must(err)
	must(db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: meter.ID, ID: "root", SourceID: "fixture", SourceInvocationID: "root", Status: "complete"}))
	input := int64(100)
	observation := agent.UsageObservation{AttemptID: meter.ID, SourceID: "fixture", EventKey: "usage", InvocationID: "root", ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &input, Output: &zero, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete}}
	must(db.RecordMeterEvent(ctx, meter.ID, "codex", agent.UsageEvent{Observation: &observation}))
	must(db.FinishMeterAttempt(ctx, meter.ID, "success", "complete", "fixture", "fixture", nil))
	_, err = db.FinishRun(ctx, runs[0].ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero, FinishedAt: at.Add(time.Second)})
	must(err)
	_, err = db.FinishRun(ctx, runs[1].ID, store.RunOutcome{Status: store.RunFailed, Error: "controlled failure", FinishedAt: at.Add(time.Second)})
	must(err)
	_, err = db.RequestOwnerStop(ctx, session.TaskRef)
	must(err)
	_, err = db.FinishRun(ctx, runs[2].ID, store.RunOutcome{Status: store.RunFailed, FinishedAt: at.Add(time.Second)})
	must(err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "notices", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	must(err)
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "build", StartedAt: at})
	must(err)
	// Internal graph node moves within the mode post nothing.
	_, err = db.RecordGraphTransition(ctx, store.GraphTransition{InstanceID: instance.ID, FromNode: "build", ToNode: "save", OnOutcome: "done", At: at})
	must(err)
	_, err = db.RecordGraphTransition(ctx, store.GraphTransition{InstanceID: instance.ID, FromNode: "build", ToNode: "build", OnOutcome: "retry", At: at})
	must(err)
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "build", StartedAt: at})
	must(err)
	must(db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "question", at.Add(time.Second)))
	must(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "owner-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "build", Kind: "question", Candidate: []byte(`{"questions":[{"text":"Which release should I target?"}],"comment":"Do not show this report prose"}`)}))
	must(collector.Collect(ctx, at.Add(time.Second))) // Wait is not published yet.
	if _, err := db.ReadChatNotice(ctx, "discord", "wait:owner-question@"+store.FormatTime(at.Add(time.Second))+"@"+session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unpublished question posted: %v", err)
	}
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "owner-question", SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#596", TriggerNamespace: "issue-comment", Purpose: "implement-question", BotUserID: 99, Body: "Question", Marker: "marker"})
	must(err)
	_, err = db.MarkReportDeliverySent(ctx, "owner-question")
	must(err)
	_, err = db.ConfirmReportDelivery(ctx, "owner-question", 91)
	must(err)
	_, err = db.CompleteReportDelivery(ctx, "owner-question")
	must(err)
	// A published review wait in another graph instance asks for a review.
	reviewing, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "review", StartedAt: at})
	must(err)
	reviewed, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: reviewing.ID, Node: "review", StartedAt: at})
	must(err)
	must(db.FinishGraphNodeAttempt(ctx, reviewed.ID, store.GraphSucceeded, "review", at.Add(time.Second)))
	must(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "owner-review", InstanceID: reviewing.ID, SourceAttemptID: reviewed.ID, Node: "review", Kind: "review", Candidate: []byte(`{"outcome":"completed"}`)}))
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "owner-review", SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#596", TriggerNamespace: "issue-comment", Purpose: "implement-review", BotUserID: 99, Body: "Review", Marker: "marker-review"})
	must(err)
	_, err = db.MarkReportDeliverySent(ctx, "owner-review")
	must(err)
	_, err = db.ConfirmReportDelivery(ctx, "owner-review", 92)
	must(err)
	_, err = db.CompleteReportDelivery(ctx, "owner-review")
	must(err)
	must(collector.Collect(ctx, at.Add(2*time.Second)))
	must(collector.Collect(ctx, at.Add(2*time.Second)))
	for range 32 {
		more, err := service.NoticeStep(ctx, at.Add(2*time.Second))
		must(err)
		if !more {
			break
		}
	}
	mu.Lock()
	counts := map[string]int{}
	for _, text := range visible {
		kind, _, ok := strings.Cut(text, ": ")
		if !ok {
			t.Errorf("missing kind: %s", text)
		}
		counts[kind]++
		if !strings.HasPrefix(text, kind+": #596 Pick a release (implement)") || strings.Contains(text, "github:") || !strings.Contains(text, "https://github.com/acme/repo/issues/596") {
			t.Errorf("missing issue title or link: %s", text)
		}
		if strings.Contains(text, "— Implement question") && (!strings.Contains(text, "Which release should I target?") || !strings.Contains(text, "#issuecomment-91") || strings.Contains(text, "report prose")) {
			t.Errorf("question text: %s", text)
		}
		if strings.Contains(text, "— Implement review") && !strings.Contains(text, "#issuecomment-92") {
			t.Errorf("review text: %s", text)
		}
		if kind == "started" && !strings.Contains(text, "Mode: plan → implement") {
			t.Errorf("mode change missing: %s", text)
		}
		if kind == "completed" && !strings.Contains(text, "about $") {
			t.Errorf("usage estimate missing: %s", text)
		}
		if (kind == "failed" || kind == "stopped") && (strings.Contains(text, "about $") || strings.Contains(text, "tokens")) {
			t.Errorf("invented usage: %s", text)
		}
	}
	retried := 0
	for _, n := range attempts {
		if n == 2 {
			retried++
		}
	}
	mu.Unlock()
	for kind, want := range map[string]int{"started": 1, "stage changed": 0, "waiting for you": 2, "completed": 1, "failed": 1, "stopped": 1} {
		if counts[kind] != want {
			t.Errorf("%s=%d want %d", kind, counts[kind], want)
		}
	}
	if retried != 1 {
		t.Errorf("retried nonce count=%d want 1", retried)
	}
	keys := []string{"wait:owner-question@" + store.FormatTime(at.Add(time.Second)) + "@" + session.TaskRef, fmt.Sprintf("run:%d:start", runs[0].ID)}
	for _, r := range runs {
		keys = append(keys, fmt.Sprintf("run:%d:finish", r.ID))
	}
	for _, r := range runs[1:] {
		if _, err := db.ReadChatNotice(ctx, "discord", fmt.Sprintf("run:%d:start", r.ID)); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("same-mode run %d started again: %v", r.ID, err)
		}
	}
	for _, key := range keys {
		row, err := db.ReadChatNotice(ctx, "discord", key)
		must(err)
		if row.State != "confirmed" || row.ReplyText != "" {
			t.Errorf("unfinished notice %s: %+v", key, row)
		}
	}
	// Purge followed by collection cannot replay previously confirmed events.
	must(service.Purge(ctx, at.Add(25*time.Hour)))
	must(collector.Collect(ctx, at.Add(25*time.Hour)))
	more, err = service.NoticeStep(ctx, at.Add(25*time.Hour))
	must(err)
	if more {
		t.Fatal("history replayed after purge")
	}
}
