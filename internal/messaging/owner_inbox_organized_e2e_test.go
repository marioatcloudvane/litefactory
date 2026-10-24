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
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Each configured source has several distinct ticket identities. One ticket has
// two independent recovery requests so ticket counts cannot mask request loss.
func organizedJourneySeed(t *testing.T, db *store.DB, provider ticket.Provider, at time.Time) []store.Session {
	t.Helper()
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), string(provider)), Forge: "github", Owner: "organized", Name: string(provider), BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), Enabled: true, Provenance: "explicit", ClonePath: repo.Path, BaseBranch: "main"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	connection := code
	connection.ID, connection.Revision, connection.Role, connection.Provider = 0, 0, store.TicketConnection, provider
	if provider == ticket.Linear {
		connection.Origin, connection.Destination = "https://api.linear.app", "team/organized"
		connection.Linear = store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "organized", AuthKind: "api_key", ModeLabelsJSON: `{"implement":"label"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}
	}
	connection, err = db.SetConnection(ctx, connection, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sessions []store.Session
	for i := range 6 {
		key := fmt.Sprint(42 + i)
		ref := ticket.Ref{Identity: ticket.Identity{Provider: provider, Origin: connection.Origin, RemoteID: fmt.Sprint(700 + i)}, Locator: ticket.Locator{Provider: provider, Origin: connection.Origin, Container: connection.Destination, Key: key}}
		if _, err := db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: connection.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
			t.Fatal(err)
		}
		task := string(provider) + ":" + connection.Destination + "#" + key
		session, err := db.AdmitSession(ctx, repo, ref, code.Revision, connection.Revision, int64(42+i), "publisher", task, "code-id", "project")
		if err != nil {
			t.Fatal(err)
		}
		seeds := []store.ImplementStorySeed{{Key: "decision-000", Position: 0, Branch: "lf/" + key}}
		if i == 0 {
			seeds = append(seeds, store.ImplementStorySeed{Key: "decision-001", Position: 1, Branch: "lf/" + key + "-second"})
		}
		if err := db.EnsureImplementStories(ctx, session.TaskRef, 1, seeds, at); err != nil {
			t.Fatal(err)
		}
		for _, seed := range seeds {
			move := store.ImplementTransition{Parent: session.TaskRef, Generation: 1, Key: seed.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: at.Add(-time.Duration(i) * time.Minute)}
			if _, err := db.TransitionImplementStory(ctx, move); err != nil {
				t.Fatal(err)
			}
			move.From, move.To, move.ExpectedRevision, move.Reason = store.ImplementRunning, store.ImplementParked, 2, "Choose recovery for "+task+" "+seed.Key
			if provider == ticket.Linear && i == 5 {
				move.Reason += "\nUrgency: Owner explicitly requests immediate recovery."
			}
			if _, err := db.TransitionImplementStory(ctx, move); err != nil {
				t.Fatal(err)
			}
		}
		sessions = append(sessions, session)
	}
	return sessions
}

// The conversation model selects only the inbox capability. The separate
// organizer returns controlled wording and one evidence-backed forward move.
type organizedJourneyProvider struct {
	selector inboxJourneyProvider
	calls    int
	expected []string
}

func (p *organizedJourneyProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if r.Model != "organizer-fixture" {
		return p.selector.RunConversation(ctx, r)
	}
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	session := r.Session
	session.Resume = "organized-inbox-fixture"
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	p.calls++
	var input struct {
		Tickets []struct {
			Key       string                `json:"key"`
			Decisions []store.InboxDecision `json:"decisions"`
			Evidence  []struct {
				ID string `json:"id"`
			} `json:"evidence"`
		} `json:"tickets"`
	}
	var envelope struct {
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal([]byte(r.Prompt), &envelope); err != nil {
		return agent.ConversationResult{}, err
	}
	if err := json.Unmarshal(envelope.Snapshot, &input); err != nil {
		return agent.ConversationResult{}, err
	}
	type outputTicket struct {
		Key             string   `json:"key"`
		DecisionKeys    []string `json:"decision_keys"`
		Summary         string   `json:"summary"`
		PromotionReason string   `json:"promotion_reason"`
		EvidenceIDs     []string `json:"evidence_ids"`
	}
	var ordinary, promoted []outputTicket
	for _, item := range input.Tickets {
		row := outputTicket{Key: item.Key, Summary: "Review the recorded recovery request.", DecisionKeys: []string{}, EvidenceIDs: []string{}}
		for _, decision := range item.Decisions {
			row.DecisionKeys = append(row.DecisionKeys, decision.Key)
		}
		if len(item.Evidence) > 0 {
			row.PromotionReason = "Owner explicitly requests immediate recovery."
			row.EvidenceIDs = []string{item.Evidence[0].ID}
			promoted = append(promoted, row)
		} else {
			ordinary = append(ordinary, row)
		}
	}
	ordered := append(append([]outputTicket{}, promoted...), ordinary...)
	p.expected = nil
	for _, row := range ordered {
		p.expected = append(p.expected, row.Key)
	}
	answer, err := json.Marshal(struct {
		Version int            `json:"version"`
		Layout  string         `json:"layout"`
		Tickets []outputTicket `json:"tickets"`
	}{1, "action_groups", ordered})
	return agent.ConversationResult{Session: session, Answer: answer}, err
}

// One withheld acknowledgment exercises saved-answer delivery replay before
// navigation. Only confirmed parts form the visible transport answer.
type organizedJourneySender struct {
	texts    []string
	attempts []string
}

func (s *organizedJourneySender) Deliver(_ context.Context, _ messaging.Target, text string) messaging.Delivery {
	s.attempts = append(s.attempts, text)
	if len(s.attempts) == 1 {
		return messaging.Delivery{RetryAfter: time.Second}
	}
	s.texts = append(s.texts, text)
	return messaging.Delivery{Confirmed: true}
}

func TestOwnerInboxOrganizedChatSavedAnswerJourney(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db := database(t)
	github := organizedJourneySeed(t, db, ticket.GitHub, at)
	linear := organizedJourneySeed(t, db, ticket.Linear, at)
	provider := &organizedJourneyProvider{}
	reader := &inboxJourneyTickets{}
	send := &organizedJourneySender{}
	service := messaging.New(db, "discord", "7", []string{"3"}, send, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.EnableOwnerInbox(reader)
	if err := service.EnableConversations(ctx, messaging.ConversationOptions{
		Bindings:    []messaging.ConversationBinding{{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}},
		StateRoot:   filepath.Join(t.TempDir(), "conversations"),
		Providers:   map[string]messaging.ConversationProvider{"claude": provider},
		Reasoners:   map[string]messaging.ConversationReasoner{"claude": {Model: "organizer-fixture"}},
		CheckAccess: func(context.Context, string) error { return nil }, Factory: true,
	}, at); err != nil {
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
		provider.selector.arguments = arguments
		before := len(send.texts)
		messages.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: fmt.Sprint(sequence), OwnerID: "7", Text: "Browse my recorded owner waits", CreatedAt: at}}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
		for step := range 30 {
			work, err := service.ConversationStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := service.ConversationDeliveryStep(ctx, at.Add(time.Duration(step)*2*time.Second))
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
	first := run(map[string]string{"operation": "organize"})
	if provider.calls != 1 {
		t.Fatalf("organizer calls=%d, want 1: %s", provider.calls, first)
	}
	for _, required := range []string{"Organized owner inbox captured " + at.Format(time.RFC3339), "Tickets: 12;", "distinct requests: 14", "Coverage:", "More:", "Open:", "Owner explicitly requests immediate recovery."} {
		if !strings.Contains(first, required) {
			t.Fatalf("organized page omitted %q: %s", required, first)
		}
	}
	if len(send.attempts) < 2 || send.attempts[0] != send.attempts[1] {
		t.Fatal("delivery replay changed saved answer bytes")
	}
	snapshot := strings.Split(inboxJourneyReference(first, "Refresh"), ":")[0]
	scope := store.InboxScope{Owner: "7", Transport: "discord", Conversation: "3"}
	facts, err := db.ReadInboxSnapshot(ctx, scope, snapshot, 0, 100, at)
	if err != nil {
		t.Fatal(err)
	}
	ticketByKey := map[string]string{}
	decisionsByTicket := map[string][]store.InboxDecision{}
	for _, decision := range facts.Entries {
		ticketByKey[store.InboxTicketIdentity(decision)] = decision.Ticket
		decisionsByTicket[decision.Ticket] = append(decisionsByTicket[decision.Ticket], decision)
	}
	if len(facts.Entries) != 14 || len(ticketByKey) != 12 {
		t.Fatalf("fixture membership requests=%d tickets=%d", len(facts.Entries), len(ticketByKey))
	}
	expected := make([]string, 0, len(provider.expected))
	for _, key := range provider.expected {
		expected = append(expected, ticketByKey[key])
	}
	if expected[0] != linear[5].TaskRef {
		t.Fatalf("fixture promotion order=%v", expected)
	}
	calls := provider.calls
	second := run(map[string]string{"operation": "more", "reference": inboxJourneyReference(first, "More")})
	if replay := run(map[string]string{"operation": "more", "reference": inboxJourneyReference(first, "More")}); replay != second {
		t.Fatal("continuation replay changed saved order or bytes")
	}
	if previous := run(map[string]string{"operation": "back", "reference": inboxJourneyReference(second, "Back")}); previous != first {
		t.Fatal("back did not preserve first page")
	}
	var got []string
	details := 0
	page := first
	for {
		if !strings.Contains(page, at.Format(time.RFC3339)) || !strings.HasPrefix(inboxJourneyReference(page, "Refresh"), snapshot+":") {
			t.Fatal("continuation changed capture time or snapshot")
		}
		if !strings.Contains(page, "Coverage:") {
			t.Fatal("continuation lost source coverage")
		}
		rows := 0
		for _, line := range strings.Split(page, "\n") {
			if strings.HasPrefix(line, "github · ") || strings.HasPrefix(line, "linear · ") {
				rows++
				pieces := strings.Split(line, " · ")
				if len(pieces) < 3 {
					t.Fatalf("invalid ticket row: %s", line)
				}
				task := strings.ReplaceAll(pieces[2], `\#`, `#`)
				got = append(got, task)
			}
			if strings.HasPrefix(line, "Open: ") {
				reference := strings.TrimPrefix(line, "Open: ")
				seenRequests := map[string]bool{}
				for {
					detail := run(map[string]string{"operation": "open", "reference": reference})
					if len(reader.opened) == 0 {
						t.Fatal("ticket open did not refresh provider detail")
					}
					selected := reader.opened[len(reader.opened)-1]
					if !strings.Contains(detail, "Fresh "+selected.Source+" fixture") {
						t.Fatal("ticket open lost refreshed qualified provider detail")
					}
					if seenRequests[selected.Key] {
						t.Fatalf("request repeated in details: %s", selected.Key)
					}
					seenRequests[selected.Key] = true
					if !strings.Contains(detail, strings.TrimPrefix(selected.WaitID, "implement:1:")) || !strings.Contains(detail, selected.Action) || !strings.Contains(strings.ReplaceAll(detail, `\#`, `#`), selected.Destination) {
						t.Fatalf("detail lost protected request/action/destination for %s: %s", selected.WaitID, detail)
					}
					details++
					if returned := run(map[string]string{"operation": "return", "reference": inboxJourneyReference(detail, "Return")}); returned != page {
						t.Fatal("open and return lost saved position")
					}
					reference = inboxJourneyReference(detail, "Requests more")
					if reference == "" {
						if len(seenRequests) != len(decisionsByTicket[selected.Ticket]) {
							t.Fatal("detail inventory omitted distinct ticket request")
						}
						break
					}
				}
			}
		}
		if !strings.Contains(page, fmt.Sprintf("Tickets: 12; remaining: %d; distinct requests: 14.", len(expected)-len(got))) {
			t.Fatalf("page counts do not match retained traversal: %s", page)
		}
		if rows > 10 {
			t.Fatalf("page has %d tickets", rows)
		}
		more := inboxJourneyReference(page, "More")
		if more == "" {
			break
		}
		page = run(map[string]string{"operation": "more", "reference": more})
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("traversal changed or lost tickets: got %v want %v", got, expected)
	}
	if details != 14 {
		t.Fatalf("reachable distinct requests=%d, want 14", details)
	}
	if provider.calls != calls {
		t.Fatalf("navigation reran organizer: calls=%d want %d", provider.calls, calls)
	}
	// The original capture remains readable after a current wait is resolved.
	if _, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: github[0].TaskRef, Generation: 1, Key: "decision-000", From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: 3, At: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Minute)
	if stable := run(map[string]string{"operation": "return", "reference": inboxJourneyReference(first, "Refresh")}); stable != first {
		t.Fatal("current wait changes shifted saved organized view")
	}
	fresh := run(map[string]string{"operation": "organize"})
	if strings.HasPrefix(inboxJourneyReference(fresh, "Refresh"), snapshot+":") || !strings.Contains(fresh, "distinct requests: 13") || !strings.Contains(fresh, at.Format(time.RFC3339)) {
		t.Fatalf("fresh organize did not capture current waits: %s", fresh)
	}
	if provider.calls != calls+1 {
		t.Fatal("fresh organized request did not reason exactly once")
	}
	filtered := run(map[string]string{"operation": "more", "reference": inboxJourneyReference(fresh, "Refresh"), "source": "linear", "project": "team/organized"})
	if !strings.Contains(filtered, "source=linear") || !strings.Contains(filtered, "Tickets: 6;") || !strings.Contains(filtered, "distinct requests: 7") || strings.Contains(filtered, "github · ") {
		t.Fatalf("changed filters did not capture independent organized facts: %s", filtered)
	}
	if provider.calls != calls+2 {
		t.Fatal("changed filters did not reason exactly once")
	}
}
