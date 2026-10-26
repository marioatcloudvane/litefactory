package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// This seam records native-session identity and supplied prompts. It makes no
// claim about a live provider's memory or isolation.
type tolerantJourneyProvider struct {
	outputs  []string
	requests []agent.ConversationRequest
}

func (p *tolerantJourneyProvider) RunConversation(ctx context.Context, request agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := request.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, agent.ConversationError(err, "before_launch", true)
	}
	p.requests = append(p.requests, request)
	session := request.Session
	if session.Resume == "" {
		session.Resume = "tolerant-native-session"
	}
	if err := request.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	if len(p.requests) > len(p.outputs) {
		return agent.ConversationResult{}, fmt.Errorf("unexpected provider replay")
	}
	return agent.ConversationResult{Session: session, Answer: []byte(p.outputs[len(p.requests)-1])}, nil
}

type tolerantJourneySender struct{ texts []string }

func (s *tolerantJourneySender) Deliver(_ context.Context, _ Target, text string) Delivery {
	s.texts = append(s.texts, text)
	return Delivery{Confirmed: true, PromptID: fmt.Sprintf("reply-%d", len(s.texts))}
}

// Intake, the conversation coordinator, correction journal, SQLite restart and
// saved-part delivery are production code. The simulated crash happens after a
// safe pending boundary has been reclaimed, before any further provider input.
func TestConversationTolerantCorrectionRestartJourney(t *testing.T) {
	ctx := t.Context()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "factory.db")
	var db *store.DB
	defer func() {
		if db != nil {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	open := func() {
		t.Helper()
		db, err = store.Open(ctx, dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	open()
	if _, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(root, "app"), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	// Leading and trailing source spaces must survive the quote delimiters.
	const literal = `  acme/app  `
	input := `"` + literal + `"`
	lookup, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "lookup", Command: "status", Arguments: []ConversationArgument{{Name: "project", SourceID: "101", Start: 1, End: 1 + len(literal)}}})
	if err != nil {
		t.Fatal(err)
	}
	const prose = "I kept your exact project wording. The registered project has no recorded work."
	provider := &tolerantJourneyProvider{outputs: []string{
		`{"version":1,"kind":"execute"}`,
		`{"version":1,"kind":"answer","text":"bad","unexpected":true}`,
		string(lookup),
		prose,
	}}
	sender := &tolerantJourneySender{}
	binding := ConversationBinding{Channel: "2", Name: "owner", Provider: "claude", Model: "fixture"}
	start := func() *Service {
		t.Helper()
		service := New(db, "discord", "1", []string{"2"}, sender, FactoryStatus{DB: db})
		service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
		if err := service.EnableConversations(ctx, ConversationOptions{Bindings: []ConversationBinding{binding}, StateRoot: filepath.Join(root, "sessions"), Providers: map[string]ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
			t.Fatal(err)
		}
		return service
	}
	service := start()
	conversation, err := db.ReadConversation(ctx, "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	scan, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "101"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "101", OwnerID: "1", Text: input, CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	// Pause once the first malformed response has saved its correction. Temporary
	// access loss is a supported safe defer boundary, not an uncertain model call.
	checks := 0
	service.conversations.Load().check = func(context.Context, string) error {
		checks++
		// Coordinator access precedes invoke and BeforeLaunch access checks.
		if checks == 4 {
			return &ConversationAccessError{}
		}
		return nil
	}
	if worked, err := service.ConversationStep(ctx, at); err != nil || !worked {
		t.Fatalf("first turn: worked=%t err=%v", worked, err)
	}
	pending, err := db.LatestConversationTurn(ctx, conversation.ID)
	if err != nil || pending.State != "pending" || len(provider.requests) != 1 {
		t.Fatalf("first correction not safely deferred: %+v calls=%d err=%v", pending, len(provider.requests), err)
	}
	before, err := db.ListConversationExchanges(ctx, pending.ID)
	if err != nil || len(before) != 2 || before[1].Kind != "correction" || !before[1].Complete {
		t.Fatalf("missing correction boundary: %+v err=%v", before, err)
	}
	savedPrompt := before[1].Request
	var correction factoryConversationPrompt
	if err := json.Unmarshal([]byte(savedPrompt), &correction); err != nil || correction.Correction == "" || correction.CorrectionReason == "" {
		t.Fatalf("missing persisted defect feedback: %+v err=%v", correction, err)
	}
	// Reclaim but send nothing: this is the precise crash boundary under review.
	crashed, err := db.ClaimConversationTurn(ctx, conversation.ID, "exited-test-worker", at, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil
	at = at.Add(11 * time.Minute)
	open()
	service = start()
	for range 5 {
		if worked, err := service.ConversationStep(ctx, at); err != nil {
			t.Fatal(err)
		} else if !worked {
			break
		}
	}
	finished, err := db.LatestConversationTurn(ctx, conversation.ID)
	if err != nil || finished.ID != crashed.ID || finished.State != "completed" || finished.Generation <= crashed.Generation {
		t.Fatalf("restart failed to fence and complete same turn: %+v err=%v", finished, err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("provider calls=%d, want four shared-budget calls", len(provider.requests))
	}
	if provider.requests[1].Prompt != savedPrompt {
		t.Fatal("restart did not use the exact saved correction prompt")
	}
	for i, request := range provider.requests[1:] {
		if request.Session.Resume != "tolerant-native-session" {
			t.Fatalf("continuation %d changed session: %+v", i+1, request.Session)
		}
	}
	journal, err := db.ListConversationExchanges(ctx, finished.ID)
	if err != nil {
		t.Fatal(err)
	}
	corrections, lookups := 0, 0
	for i, exchange := range journal {
		if exchange.Ordinal != i+1 {
			t.Fatalf("noncontiguous journal: %+v", exchange)
		}
		switch exchange.Kind {
		case "correction":
			corrections++
		case "lookup":
			lookups++
			var boundary struct {
				Invocation Invocation `json:"normalized_invocation"`
			}
			if err := json.Unmarshal([]byte(exchange.Request), &boundary); err != nil {
				t.Fatal(err)
			}
			if boundary.Invocation.Arguments["project"] != literal {
				t.Fatalf("quoted source bytes changed: %q", boundary.Invocation.Arguments["project"])
			}
		}
	}
	if corrections != 2 || lookups != 1 {
		t.Fatalf("journal corrections=%d lookups=%d, want 2 and 1", corrections, lookups)
	}
	var lastPrompt factoryConversationPrompt
	if err := json.Unmarshal([]byte(provider.requests[3].Prompt), &lastPrompt); err != nil {
		t.Fatal(err)
	}
	if lastPrompt.Correction != "" || lastPrompt.CorrectionReason != "" {
		t.Fatal("successful lookup retained stale correction feedback")
	}
	if worked, err := service.ConversationDeliveryStep(ctx, at); err != nil || !worked {
		t.Fatalf("delivery: worked=%t err=%v", worked, err)
	}
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], prose) {
		t.Fatalf("useful plain prose lost in delivery: %q", sender.texts)
	}
	saved, err := db.ReadConversation(ctx, "discord", "2")
	if err != nil || saved.SessionHandle != "tolerant-native-session" || saved.HealthReason != "" {
		t.Fatalf("restart lost healthy identity: %+v err=%v", saved, err)
	}
	if err := db.CompleteConversationTurn(ctx, crashed, "completed", "stale worker", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale worker fence: %v", err)
	}
}

func TestConversationRecoveryRefusesUnsafeCorrectionBoundaries(t *testing.T) {
	for _, name := range []string{"uncertain provider", "incomplete correction", "missing native identity", "changed owner source", "three corrections"} {
		t.Run(name, func(t *testing.T) {
			provider := &factoryUnitProvider{outputs: []string{"unexpected invocation"}}
			run, at := factoryUnitRun(t, provider)
			capture, err := run.s.restoreConversationInstructions(t.Context(), run.c.ID, at)
			if err != nil {
				t.Fatal(err)
			}
			prompt := factoryConversationPrompt{Instructions: capture.Factory(), OwnerInput: run.t.Source, OwnerSources: []DialogueTurn{{SourceID: run.t.SourceID, Text: run.t.Source}}, Commands: run.s.ConversationCommands()}
			if name != "missing native identity" {
				if err := run.s.db.SaveConversationSession(t.Context(), run.t, "saved-session"); err != nil {
					t.Fatal(err)
				}
				run.c.SessionHandle = "saved-session"
			}
			reserve := func(ordinal int, kind string, complete bool) {
				t.Helper()
				encoded, err := json.Marshal(prompt)
				if err != nil {
					t.Fatal(err)
				}
				attempt := ""
				if kind == "provider" {
					attempt = fmt.Sprintf("%s:%d", run.t.ID, (ordinal+1)/2)
				}
				if err := run.s.db.SaveConversationExchange(t.Context(), run.t, store.ConversationExchange{TurnID: run.t.ID, Ordinal: ordinal, Kind: kind, Request: string(encoded), ProviderAttempt: attempt, CapturedAt: at, Complete: complete}); err != nil {
					t.Fatal(err)
				}
			}
			reserve(1, "provider", false)
			if name != "uncertain provider" {
				prompt.Correction = capture.Correction()
				prompt.CorrectionReason = "response envelope is invalid"
				if name == "changed owner source" {
					prompt.OwnerSources[0].Text = "invented replacement source"
				}
				reserve(2, "correction", name != "incomplete correction")
			}
			if name == "three corrections" {
				reserve(3, "provider", false)
				reserve(4, "correction", true)
				reserve(5, "provider", false)
				reserve(6, "correction", true)
			}
			resumable, err := run.s.recoverFactoryBoundary(t.Context(), run.c, run.b, run.t, at.Add(11*time.Minute))
			allowedBudgetStop := name == "three corrections" && errors.Is(err, errConversationBudget)
			if (err != nil && !allowedBudgetStop) || resumable {
				t.Fatalf("unsafe boundary resumed: resumable=%t err=%v", resumable, err)
			}
			if provider.calls != 0 {
				t.Fatal("unsafe recovery replayed provider input")
			}
			saved, err := run.s.db.LatestConversationTurn(t.Context(), run.c.ID)
			if err != nil || saved.State != "running" || saved.Generation != run.t.Generation {
				t.Fatalf("rejected boundary changed ownership: %+v err=%v", saved, err)
			}
		})
	}
}
