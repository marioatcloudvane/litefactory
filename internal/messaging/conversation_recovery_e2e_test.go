package messaging_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

// This provider records the supplied evidence, rather than retaining any model
// memory. Intake, admission, evidence recovery, Ask preparation and persistence
// are production code backed by a real database.
type capturedEvidenceProvider struct {
	requests  []agent.ConversationRequest
	responses []string
}

func (p *capturedEvidenceProvider) RunConversation(ctx context.Context, request agent.ConversationRequest) (agent.ConversationResult, error) {
	p.requests = append(p.requests, request)
	if len(p.requests) > len(p.responses) {
		return agent.ConversationResult{}, fmt.Errorf("unexpected provider invocation %d", len(p.requests))
	}
	if err := request.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	session := request.Session
	session.Resume = session.Ref
	if err := request.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	return agent.ConversationResult{Session: session, Answer: []byte(p.responses[len(p.requests)-1])}, nil
}

func TestConversationCapturedEvidenceFollowupAndRestart(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	path := filepath.Join(root, "factory.db")
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	var db *store.DB
	open := func() {
		t.Helper()
		var err error
		db, err = store.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	open()
	defer func() {
		if db != nil {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	if _, err := db.UpsertRepo(ctx, store.Repo{Path: root, Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	finishRow(t, db, store.ChatReply{Transport: "discord", MessageID: "90", ChannelRef: "3", Command: "ask", Arguments: map[string]string{"question": "Which format did we discuss?", "project": "app"}, CreatedAt: at.Add(-time.Minute)}, ask.TurnPrefix("Which format did we discuss?", "acme/app")+"The recorded preference is CSV. fixture-secret", false, at.Add(-time.Minute))
	provider := &capturedEvidenceProvider{responses: []string{
		`{"version":1,"kind":"lookup","command":"ask","arguments":[{"name":"project","value":"acme/app"},{"name":"question","value":"What have we discussed?"}]}`,
		`{"version":1,"kind":"answer","text":"The captured preference is CSV."}`,
		`{"version":1,"kind":"answer","text":"The same captured preference remains available."}`,
		`{"version":1,"kind":"answer","text":"The captured preference survived restart."}`,
		`{"version":1,"kind":"lookup","command":"ask","arguments":[{"name":"project","value":"acme/app"},{"name":"question","value":"What is known about issue 42?"}]}`,
		`{"version":1,"kind":"answer","text":"Ask history is excluded for this explicit issue."}`,
	}}
	sender := &journeySender{}
	start := func() *messaging.Service {
		t.Helper()
		s := messaging.New(db, "discord", "7", []string{"3"}, sender, messaging.FactoryStatus{DB: db})
		s.Protect = func(_ context.Context, text string) (string, error) {
			return strings.ReplaceAll(text, "fixture-secret", "[redacted]"), nil
		}
		s.EnableAsk(&ask.Service{Repositories: db, Collector: ask.Collector{Factory: ask.StoreReader{Reader: db}}, History: messaging.AskHistory{DB: db, Transport: "discord"}, Redactor: redact.New(redact.Options{})})
		if err := s.EnableConversations(ctx, messaging.ConversationOptions{Factory: true, Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "evidence", Provider: "claude", Model: "recorded"}}, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }}, at); err != nil {
			t.Fatal(err)
		}
		return s
	}
	service := start()
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	admit := func(id, text string) {
		t.Helper()
		reader := &journeyReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, CreatedAt: at}}}
		intake := messaging.Intake{DB: db, Reader: reader, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: service.Protect}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
		for range 20 {
			worked, err := service.ConversationStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			delivered, err := service.ConversationDeliveryStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			if !worked && !delivered {
				return
			}
		}
		t.Fatal("conversation did not settle")
	}
	admit("101", "For acme/app, remember the owner chose CSV. fixture-secret")
	admit("102", "Continue this discussion with its captured evidence.")
	if len(provider.requests) != 3 {
		t.Fatalf("follow-up provider calls: %d; replies: %v", len(provider.requests), sender.texts)
	}
	before := recoveryEvidence(t, provider.requests[2].Prompt)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil
	at = at.Add(time.Minute)
	open()
	service = start()
	admit("103", "Continue the same discussion after restart.")
	if len(provider.requests) != 4 {
		t.Fatalf("restart provider calls: %d; replies: %v", len(provider.requests), sender.texts)
	}
	after := recoveryEvidence(t, provider.requests[3].Prompt)
	for id, capture := range before {
		if !reflect.DeepEqual(capture, after[id]) {
			t.Fatalf("capture %s changed across restart: before=%v after=%v", id, capture, after[id])
		}
	}
	admit("104", "For acme/app issue 42, what evidence is available?")
	if len(provider.requests) != 6 {
		t.Fatalf("explicit issue provider calls: %d; replies: %v", len(provider.requests), sender.texts)
	}
	// The explicit issue's lookup must explain admission without supplying the
	// unrelated discussion's historical Ask question or answer.
	explicit := provider.requests[5].Prompt
	var scoped struct {
		Evidence []store.ConversationEvidence `json:"retained_evidence"`
	}
	if err := json.Unmarshal([]byte(explicit), &scoped); err != nil {
		t.Fatal(err)
	}
	for _, capture := range scoped.Evidence {
		if capture.Repository != "acme/app" || capture.Issue != "42" {
			t.Fatalf("explicit issue inherited unrelated evidence: %+v", capture)
		}
	}
	if strings.Contains(explicit, "Which format did we discuss?") || strings.Contains(explicit, "The recorded preference is CSV") {
		t.Fatal("explicit issue admitted discussion Ask history")
	}
	if !strings.Contains(strings.ToLower(explicit), "excluded") && !strings.Contains(strings.ToLower(explicit), "omitted") {
		t.Fatal("explicit issue Ask omission was not explained")
	}
}

func recoveryEvidence(t *testing.T, prompt string) map[string]map[string]any {
	t.Helper()
	var value struct {
		Evidence []store.ConversationEvidence `json:"retained_evidence"`
	}
	if err := json.Unmarshal([]byte(prompt), &value); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]map[string]any)
	kinds := make(map[string]bool)
	var content strings.Builder
	for _, capture := range value.Evidence {
		if capture.Entry.OwnerID == "" || capture.Entry.Revision == "" || capture.CapturedAt.IsZero() || capture.RangeEnd < capture.RangeStart || capture.ViewDigest != retainedevidence.Digest([]byte(capture.Content)) {
			t.Fatalf("capture lacks exact identity or bounded view: %+v", capture)
		}
		if strings.Contains(capture.Content, "fixture-secret") {
			t.Fatal("recovered evidence contains unprotected content")
		}
		kinds[capture.Entry.Kind] = true
		content.WriteString(capture.Content)
		key := capture.Entry.Kind + ":" + capture.Entry.OwnerID + ":" + capture.Entry.Revision
		out[key] = map[string]any{"source_turn": capture.SourceTurnID, "entry": capture.Entry, "content": capture.Content, "captured_at": capture.CapturedAt, "expires_at": capture.ExpiresAt, "start": capture.RangeStart, "end": capture.RangeEnd, "view_digest": capture.ViewDigest}
	}
	for _, kind := range []string{"owner", "reply", "lookup", "ask"} {
		if !kinds[kind] {
			t.Fatalf("%s evidence missing from recovery: %s", kind, prompt)
		}
	}
	for _, text := range []string{"owner chose CSV", "The captured preference is CSV", "Which format did we discuss?", "The recorded preference is CSV", "discord:90", "[redacted]"} {
		if !strings.Contains(content.String(), text) {
			t.Fatalf("recovered sources lack %q: %s", text, content.String())
		}
	}
	return out
}
