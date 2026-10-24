package messaging_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// The model fixture selects a capability only. Membership, formatting, retained
// pages, references and delivery all run through production code and SQLite.
type inboxJourneyProvider struct{ arguments map[string]string }

func (p *inboxJourneyProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	session := r.Session
	if session.Resume == "" {
		session.Resume = "inbox-journey"
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	arguments := make([]map[string]string, 0, len(p.arguments))
	for k, v := range p.arguments {
		arguments = append(arguments, map[string]string{"name": k, "value": v})
	}
	sort.Slice(arguments, func(i, j int) bool { return arguments[i]["name"] < arguments[j]["name"] })
	answer, err := json.Marshal(map[string]any{"version": 1, "kind": "lookup", "command": "inbox", "arguments": arguments})
	return agent.ConversationResult{Session: session, Answer: answer}, err
}

type inboxJourneyTickets struct{ opened []store.InboxDecision }

func (r *inboxJourneyTickets) ReadOwnerInboxTicket(_ context.Context, d store.InboxDecision) (messaging.OwnerInboxTicketDetail, error) {
	r.opened = append(r.opened, d)
	return messaging.OwnerInboxTicketDetail{Title: "Fresh " + d.Source + " fixture", URL: "https://fixture.invalid/" + d.Source + "/" + d.Locator.Key, Body: strings.Repeat("Current ticket context 界🙂 ", 150)}, nil
}

func inboxJourneySeed(t *testing.T, db *store.DB, provider ticket.Provider, at time.Time) (store.Session, []string) {
	t.Helper()
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), string(provider)), Forge: "github", Owner: "fixture", Name: string(provider), BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	code := store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), Enabled: true, Provenance: "explicit", ClonePath: repo.Path, BaseBranch: "main"}
	code, err = db.SetConnection(ctx, code, 0)
	if err != nil {
		t.Fatal(err)
	}
	connection := code
	connection.ID = 0
	connection.Revision = 0
	connection.Role = store.TicketConnection
	connection.Provider = provider
	if provider == ticket.Linear {
		connection.Origin = "https://api.linear.app"
		connection.Destination = "team/project"
		connection.Linear = store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"implement":"label"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}
	}
	connection, err = db.SetConnection(ctx, connection, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: provider, Origin: connection.Origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: provider, Origin: connection.Origin, Container: connection.Destination, Key: "42"}}
	if _, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: connection.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	task := string(provider) + ":" + connection.Destination + "#42"
	session, err := db.AdmitSession(ctx, repo, ref, code.Revision, connection.Revision, 42, "publisher", task, "code-id", "project")
	if err != nil {
		t.Fatal(err)
	}
	var seeds []store.ImplementStorySeed
	var keys []string
	for i := 0; i < 61; i++ {
		key := fmt.Sprintf("decision-%03d", i)
		keys = append(keys, "implement:1:"+key)
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	if err = db.EnsureImplementStories(ctx, session.TaskRef, 1, seeds, at); err != nil {
		t.Fatal(err)
	}
	for _, seed := range seeds {
		move := store.ImplementTransition{Parent: session.TaskRef, Generation: 1, Key: seed.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: at}
		if _, err = db.TransitionImplementStory(ctx, move); err != nil {
			t.Fatal(err)
		}
		move.From = store.ImplementRunning
		move.To = store.ImplementParked
		move.ExpectedRevision = 2
		move.Reason = seed.Key + " " + strings.Repeat("Long owner question 界🙂 ", 90)
		if _, err = db.TransitionImplementStory(ctx, move); err != nil {
			t.Fatal(err)
		}
	}
	return session, keys
}

func inboxJourneyReference(text, label string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, label+": ") {
			return strings.TrimPrefix(line, label+": ")
		}
	}
	return ""
}

func TestOwnerInboxChatSavedAnswerJourney(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db := database(t)
	github, githubKeys := inboxJourneySeed(t, db, ticket.GitHub, at)
	linear, linearKeys := inboxJourneySeed(t, db, ticket.Linear, at)
	provider := &inboxJourneyProvider{}
	reader := &inboxJourneyTickets{}
	send := &groundingSender{}
	service := messaging.New(db, "discord", "7", []string{"3"}, send, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.EnableOwnerInbox(reader)
	if err := service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}}, StateRoot: filepath.Join(t.TempDir(), "conversations"), Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	messages := &journeyReader{}
	intake := messaging.Intake{DB: db, Reader: messages, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: service.Protect}
	sequence := 100
	run := func(arguments map[string]string) string {
		t.Helper()
		sequence++
		provider.arguments = arguments
		before := len(send.texts)
		messages.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: fmt.Sprint(sequence), OwnerID: "7", Text: "Browse my recorded owner waits", CreatedAt: at}}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
		for range 30 {
			work, err := service.ConversationStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := service.ConversationDeliveryStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			if !work && !delivery {
				break
			}
		}
		if len(send.texts) == before {
			t.Fatal("inbox capability did not deliver an answer")
		}
		var delivered strings.Builder
		for _, part := range send.texts[before:] {
			if len(utf16.Encode([]rune(part))) > 2000 {
				t.Fatalf("unbounded chat part: %d UTF-16 units", len(utf16.Encode([]rune(part))))
			}
			delivered.WriteString(part)
		}
		conversation, err := db.ReadConversation(ctx, "discord", "3")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := db.LatestConversationTurn(ctx, conversation.ID)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := db.ReadChatReply(ctx, "discord", turn.ReplyID)
		if err != nil {
			t.Fatal(err)
		}
		answer, _, err := db.ConversationReplyAnswer(ctx, reply)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := db.ReadConversationAnswerParts(ctx, answer.ID)
		if err != nil {
			t.Fatal(err)
		}
		var saved strings.Builder
		for _, part := range parts {
			saved.WriteString(part.Text)
		}
		if saved.String() != delivered.String() {
			t.Fatal("transport delivery differs from the saved answer")
		}
		return saved.String()
	}
	first := run(nil)
	for _, required := range []string{at.Format(time.RFC3339), "ascending bytewise", "Coverage:", "More:", "Open:"} {
		if !strings.Contains(first, required) {
			t.Fatalf("page omitted %q: %s", required, first)
		}
	}
	firstMore := inboxJourneyReference(first, "More")
	snapshot := strings.Split(firstMore, ":")[0]
	second := run(map[string]string{"operation": "more", "reference": firstMore})
	if replay := run(map[string]string{"operation": "more", "reference": firstMore}); replay != second {
		t.Fatal("continuation replay changed")
	}
	if back := run(map[string]string{"operation": "back", "reference": inboxJourneyReference(second, "Back")}); back != first {
		t.Fatal("back did not preserve first page")
	}
	open := run(map[string]string{"operation": "open", "reference": inboxJourneyReference(second, "Open")})
	if !strings.Contains(open, "Fresh github fixture") || len(reader.opened) != 1 || reader.opened[0].Locator.Provider != ticket.GitHub {
		t.Fatalf("qualified GitHub detail was not refreshed: %s", open)
	}
	if returned := run(map[string]string{"operation": "return", "reference": inboxJourneyReference(open, "Return")}); returned != second {
		t.Fatal("open lost browse position")
	}
	traverse := func(page string) []string {
		t.Helper()
		var got []string
		pageSnapshot := strings.Split(inboxJourneyReference(page, "Refresh"), ":")[0]
		for {
			if !strings.Contains(page, at.Format(time.RFC3339)) || !strings.Contains(page, "Coverage:") || !strings.Contains(page, "ascending bytewise") {
				t.Fatal("continuation omitted capture, ordering or coverage")
			}
			if !strings.HasPrefix(inboxJourneyReference(page, "Refresh"), pageSnapshot+":") {
				t.Fatal("continuation changed snapshot")
			}
			var row string
			for _, line := range strings.Split(page, "\n") {
				if strings.HasPrefix(line, "github · ") || strings.HasPrefix(line, "linear · ") {
					row = strings.ReplaceAll(line, `\#`, `#`)
				}
				if strings.HasPrefix(line, "Waiting: ") {
					fields := strings.Fields(strings.TrimPrefix(line, "Waiting: "))
					got = append(got, row+"|implement:1:"+fields[0])
				}
			}
			more := inboxJourneyReference(page, "More")
			if more == "" {
				return got
			}
			page = run(map[string]string{"operation": "more", "reference": more})
		}
	}
	qualified := func(source, project, task string, keys []string) []string {
		var out []string
		for _, key := range keys {
			out = append(out, source+" · "+project+" · "+task+"|"+key)
		}
		return out
	}
	githubExpected := qualified("github", "fixture/github", github.TaskRef, githubKeys)
	linearExpected := qualified("linear", "team/project", linear.TaskRef, linearKeys)
	expected := append(append([]string{}, githubExpected...), linearExpected...)
	if got := traverse(first); !reflect.DeepEqual(got, expected) {
		t.Fatalf("captured traversal lost or reordered waits: got %d want %d; first got %q want %q", len(got), len(expected), got[0], expected[0])
	}
	filtered := run(map[string]string{"source": "linear", "project": "team/project", "kind": "parked"})
	if strings.Contains(filtered, github.TaskRef) || !strings.Contains(filtered, "source=linear") || strings.HasPrefix(inboxJourneyReference(filtered, "Refresh"), snapshot+":") {
		t.Fatal("changed filters did not create a distinct filtered snapshot")
	}
	if got := traverse(filtered); !reflect.DeepEqual(got, linearExpected) {
		t.Fatalf("filtered traversal lost or reordered waits: got %d want %d", len(got), len(linearExpected))
	}
	linearOpen := run(map[string]string{"operation": "open", "reference": inboxJourneyReference(filtered, "Open")})
	if !strings.Contains(linearOpen, "Fresh linear fixture") || reader.opened[len(reader.opened)-1].Locator.Provider != ticket.Linear {
		t.Fatal("Linear open selected the wrong provider")
	}
	if _, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: github.TaskRef, Generation: 1, Key: "decision-000", From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: 3, At: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if stable := run(map[string]string{"operation": "return", "reference": inboxJourneyReference(first, "Refresh")}); stable != first {
		t.Fatal("local wait change shifted captured page")
	}
	refreshed := run(map[string]string{"operation": "refresh", "reference": inboxJourneyReference(first, "Refresh")})
	if strings.HasPrefix(inboxJourneyReference(refreshed, "Refresh"), snapshot+":") || strings.Contains(refreshed, "Waiting: decision-000 ") {
		t.Fatal("refresh did not create current membership")
	}
}
