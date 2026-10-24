package messaging_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestOwnerInboxOrganizedContinuationAfterSourceRevoked(t *testing.T) {
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
	snapshot := strings.Split(inboxJourneyReference(first, "Refresh"), ":")[0]
	if snapshot == "" || !strings.Contains(first, "Owner explicitly requests immediate recovery.") {
		t.Fatalf("organized fixture missing promotion: %s", first)
	}
	revoked, err := db.CurrentConnection(ctx, linear[0].RepoID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(ctx, revoked.RepoID, store.TicketConnection, revoked.Revision); err != nil {
		t.Fatal(err)
	}
	assertSafe := func(text string) {
		t.Helper()
		for _, session := range linear {
			if strings.Contains(strings.ReplaceAll(text, `\#`, `#`), session.TaskRef) {
				t.Fatalf("revoked ticket content leaked: %s", text)
			}
		}
		if strings.Contains(text, "Owner explicitly requests immediate recovery.") || strings.Contains(text, "Review the recorded recovery request.") {
			t.Fatalf("revoked organizer summary leaked: %s", text)
		}
	}
	revokedOpen := run(map[string]string{"operation": "open", "reference": inboxJourneyReference(first, "Open")})
	assertSafe(revokedOpen)
	if strings.Contains(revokedOpen, "Fresh linear fixture") || !strings.HasPrefix(inboxJourneyReference(revokedOpen, "Return"), snapshot+":") {
		t.Fatalf("revoked open did not preserve safe return: %s", revokedOpen)
	}
	firstPartial := run(map[string]string{"operation": "return", "reference": inboxJourneyReference(first, "Refresh")})
	assertSafe(firstPartial)
	if !strings.Contains(firstPartial, "Coverage") || !strings.Contains(firstPartial, at.Format(time.RFC3339)) {
		t.Fatalf("partial view lost captured coverage: %s", firstPartial)
	}
	var found []string
	page := firstPartial
	for {
		assertSafe(page)
		if !strings.HasPrefix(inboxJourneyReference(page, "Refresh"), snapshot+":") {
			t.Fatalf("continuation changed snapshot: %s", page)
		}
		for _, line := range strings.Split(page, "\n") {
			if strings.HasPrefix(line, "github · ") {
				fields := strings.Split(line, " · ")
				if len(fields) != 3 {
					t.Fatalf("invalid row %q", line)
				}
				found = append(found, strings.ReplaceAll(fields[2], `\#`, `#`))
			}
		}
		if reference := inboxJourneyReference(page, "Open"); reference != "" {
			opened := run(map[string]string{"operation": "open", "reference": reference})
			assertSafe(opened)
			if !strings.Contains(opened, "Fresh github fixture") {
				t.Fatalf("authorized open unavailable: %s", opened)
			}
			returned := run(map[string]string{"operation": "return", "reference": inboxJourneyReference(opened, "Return")})
			assertSafe(returned)
			if returned != page {
				t.Fatal("open lost partial browsing position")
			}
		}
		more := inboxJourneyReference(page, "More")
		if more == "" {
			break
		}
		next := run(map[string]string{"operation": "more", "reference": more})
		assertSafe(next)
		if replay := run(map[string]string{"operation": "more", "reference": more}); replay != next {
			t.Fatal("partial continuation replay changed")
		}
		if back := inboxJourneyReference(next, "Back"); back != "" {
			if returned := run(map[string]string{"operation": "back", "reference": back}); returned != page {
				t.Fatal("partial back lost saved position")
			}
		}
		page = next
	}
	expected := make([]string, 0, len(github))
	for _, session := range github {
		expected = append(expected, session.TaskRef)
	}
	if !reflect.DeepEqual(found, expected) {
		t.Fatalf("authorized traversal got %v, want %v", found, expected)
	}
	coverage := run(map[string]string{"operation": "coverage", "reference": inboxJourneyReference(firstPartial, "Coverage details")})
	assertSafe(coverage)
	if !strings.Contains(coverage, "access unavailable") {
		t.Fatalf("revoked source coverage gap missing: %s", coverage)
	}
	if !strings.HasPrefix(inboxJourneyReference(coverage, "Return"), snapshot+":") {
		t.Fatal("coverage lost snapshot")
	}
	if provider.calls != 1 {
		t.Fatalf("navigation reran organizer %d times", provider.calls)
	}
	refreshed := run(map[string]string{"operation": "refresh", "reference": inboxJourneyReference(firstPartial, "Refresh")})
	if strings.HasPrefix(inboxJourneyReference(refreshed, "Refresh"), snapshot+":") || !strings.Contains(refreshed, "github · ") {
		t.Fatalf("refresh hid authorized rows: %s", refreshed)
	}
	changed := run(map[string]string{"source": "github", "reference": inboxJourneyReference(firstPartial, "Refresh")})
	if strings.HasPrefix(inboxJourneyReference(changed, "Refresh"), snapshot+":") || !strings.Contains(changed, "source=github") || !strings.Contains(changed, "github · ") {
		t.Fatalf("changed filters unavailable after revocation: %s", changed)
	}
	at = at.Add(24 * time.Hour)
	expired := run(map[string]string{"operation": "more", "reference": inboxJourneyReference(first, "More")})
	assertSafe(expired)
	if !strings.Contains(strings.ToLower(expired), "expired") {
		t.Fatalf("expired snapshot was not refused: %s", expired)
	}
}

// Saved model wording can refer to the whole captured inbox. Revocation must
// withhold that answer, while a later factual continuation remains deliverable.
func TestOwnerInboxOrganizedSavedDeliveryAfterSourceRevoked(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db := database(t)
	organizedJourneySeed(t, db, ticket.GitHub, at)
	linear := organizedJourneySeed(t, db, ticket.Linear, at)
	provider := &organizedJourneyProvider{}
	sender := &organizedJourneySender{}
	service := messaging.New(db, "discord", "7", []string{"3"}, sender, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.EnableOwnerInbox(&inboxJourneyTickets{})
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
	prepare := func(arguments map[string]string) string {
		t.Helper()
		sequence++
		provider.selector.arguments = arguments
		messages.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: fmt.Sprint(sequence), OwnerID: "7", Text: "Browse recorded owner waits", CreatedAt: at}}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ConversationStep(ctx, at); err != nil {
			t.Fatal(err)
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
		var text strings.Builder
		for _, part := range parts {
			text.WriteString(part.Text)
		}
		return text.String()
	}
	deliver := func() string {
		t.Helper()
		before := len(sender.texts)
		for range 30 {
			work, err := service.ConversationDeliveryStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			at = at.Add(2 * time.Second)
			if !work {
				break
			}
		}
		if len(sender.texts) == before {
			t.Fatal("saved inbox answer did not deliver")
		}
		if len(sender.attempts) < 2 || sender.attempts[0] != sender.attempts[1] {
			t.Fatal("withheld acknowledgment retry changed saved delivery bytes")
		}
		return strings.Join(sender.texts[before:], "")
	}
	assertSafe := func(text string) {
		t.Helper()
		for _, session := range linear {
			if strings.Contains(strings.ReplaceAll(text, `\#`, `#`), session.TaskRef) {
				t.Fatalf("revoked ticket leaked through saved delivery: %s", text)
			}
		}
		if strings.Contains(text, "Owner explicitly requests immediate recovery.") || strings.Contains(text, "Review the recorded recovery request.") {
			t.Fatalf("cached organizer wording leaked after source revocation: %s", text)
		}
	}

	mixed := prepare(map[string]string{"operation": "organize"})
	reference := inboxJourneyReference(mixed, "Refresh")
	if reference == "" || !strings.Contains(strings.ReplaceAll(mixed, `\#`, `#`), linear[5].TaskRef) || provider.calls != 1 || len(sender.attempts) != 0 {
		t.Fatalf("mixed answer was not saved before delivery: %s", mixed)
	}
	connection, err := db.CurrentConnection(ctx, linear[0].RepoID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DisableConnection(ctx, connection.RepoID, store.TicketConnection, connection.Revision); err != nil {
		t.Fatal(err)
	}
	notice := deliver()
	assertSafe(notice)
	if notice == mixed || !strings.Contains(strings.ToLower(notice), "refresh") {
		t.Fatalf("unsafe saved answer was not replaced with a safe notice: %s", notice)
	}

	// Reuse the captured navigation instead of collecting or reasoning again.
	sender.attempts = nil
	partial := prepare(map[string]string{"operation": "return", "reference": reference})
	assertSafe(partial)
	if !strings.Contains(partial, "github · ") || !strings.Contains(partial, "Source access changed") || !strings.HasPrefix(inboxJourneyReference(partial, "Refresh"), strings.Split(reference, ":")[0]+":") {
		t.Fatalf("captured navigation did not recover authorized facts: %s", partial)
	}
	if delivered := deliver(); delivered != partial {
		t.Fatal("authorized continuation delivery differs from immutable saved parts")
	}
	for _, attempt := range sender.attempts {
		assertSafe(attempt)
	}
	if provider.calls != 1 {
		t.Fatalf("saved delivery or continuation reran organizer %d times", provider.calls)
	}
}
