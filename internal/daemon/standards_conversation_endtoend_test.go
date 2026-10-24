package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// These response-only fixtures establish application behavior, not live Discord
// availability, authentic model behavior or native-provider isolation.
type standardsJourneyProvider struct {
	command, action string
	arguments       map[string]string
}

func (f *standardsJourneyProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	session := r.Session
	session.Resume = "standards-journey-session"
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	var prompt struct {
		OwnerSources []messaging.DialogueTurn `json:"owner_sources"`
		Evidence     json.RawMessage          `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(r.Prompt), &prompt); err != nil {
		return agent.ConversationResult{}, err
	}
	envelope := messaging.ConversationEnvelope{Version: 1, Kind: "lookup", Command: f.command}
	if len(prompt.Evidence) > 0 && string(prompt.Evidence) != "null" {
		var evidence struct {
			FactID string `json:"fact_id"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal(prompt.Evidence, &evidence); err != nil {
			return agent.ConversationResult{}, err
		}
		envelope = messaging.ConversationEnvelope{Version: 1, Kind: "answer", Blocks: []presentation.AnswerBlock{{Text: evidence.Result, FactRefs: []string{evidence.FactID}}}}
	} else {
		if f.command != "standards" {
			envelope.Kind = "propose"
		}
		envelope.Arguments = append(envelope.Arguments, messaging.ConversationArgument{Name: "action", Value: f.action})
		for name, value := range f.arguments {
			arg := messaging.ConversationArgument{Name: name, Value: value}
			if envelope.Kind == "propose" {
				source := prompt.OwnerSources[len(prompt.OwnerSources)-1]
				start := strings.Index(source.Text, value)
				if start < 0 {
					return agent.ConversationResult{}, fmt.Errorf("fixture value %s missing from ordinary owner message", name)
				}
				arg = messaging.ConversationArgument{Name: name, SourceID: source.SourceID, Start: start, End: start + len(value)}
			}
			envelope.Arguments = append(envelope.Arguments, arg)
		}
	}
	raw, err := json.Marshal(envelope)
	return agent.ConversationResult{Answer: raw, Session: session}, err
}

type standardsJourneyAuthor struct{ drafts int }

func (f *standardsJourneyAuthor) Answer(_ context.Context, c standards.AuthoringCall) ([]byte, error) {
	prompt := c.Prompt
	f.drafts++
	if f.drafts == 1 {
		return []byte(`{"schema_version":1,"category_id":"quality","rule":"MUST run relevant checks","description":"Run checks.","evidence":"The PR records the check output.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[{"text":"Which checks?","options":["Relevant package checks","All checks"]}]}`), nil
	}
	if !strings.Contains(prompt, `"answer":"Relevant package checks"`) {
		return nil, fmt.Errorf("saved answer absent from resumed authoring prompt")
	}
	return []byte(`{"schema_version":1,"category_id":"quality","rule":"MUST run relevant checks","description":"Run relevant package checks before sharing changes.","evidence":"The PR records the check output.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[]}`), nil
}

type standardsJourneySender struct {
	texts   []string
	adapter *discord.Adapter
}

func (s *standardsJourneySender) Deliver(ctx context.Context, target messaging.Target, text string) messaging.Delivery {
	s.texts = append(s.texts, text)
	return s.adapter.Deliver(ctx, target, text)
}

type standardsJourneyReader struct{ message store.NaturalMessage }

func (*standardsJourneyReader) CheckIntakeCapability(context.Context, string) error { return nil }
func (r *standardsJourneyReader) ReadIntakePage(_ context.Context, _, _, before string) (messaging.IntakePage, error) {
	if before != "" {
		return messaging.IntakePage{}, nil
	}
	return messaging.IntakePage{Messages: []store.NaturalMessage{r.message}, HighID: r.message.SourceID, LowID: r.message.SourceID, Count: 1}, nil
}

func TestStandardsConversationOwnerMainPath(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "factory.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 71}
	seed, err := book.Create(ctx, "conversation-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = book.Publish(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	authorFixture := &standardsJourneyAuthor{}
	provider := &standardsJourneyProvider{}
	remoteID := 500
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/channels/3/messages" {
			t.Errorf("unexpected Discord request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload struct {
			Content         string `json:"content"`
			Nonce           string `json:"nonce"`
			AllowedMentions struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
		}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.Content == "" || payload.Nonce == "" || len(payload.AllowedMentions.Parse) != 0 {
			t.Errorf("unsafe or incomplete Discord payload: %+v", payload)
		}
		remoteID++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"%d"}`, remoteID)
	}))
	defer api.Close()
	sender := &standardsJourneySender{adapter: discord.New(discord.Options{Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client()})}
	at := time.Now().UTC()
	root := t.TempDir()
	var service *messaging.Service
	var author *standards.Authoring
	setup := func() {
		book = &standards.Service{Store: db, OwnerID: 71}
		author = &standards.Authoring{Store: db, Book: book, Runner: authorFixture}
		service = messaging.New(db, "discord", "7", []string{"3"}, sender, messaging.FactoryStatus{DB: db})
		service.Protect = func(_ context.Context, s string) (string, error) { return s, nil }
		service.EnableStandards(&ConversationStandards{DB: db, Book: book, Authoring: author, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }})
		if e := service.EnableConversations(ctx, messaging.ConversationOptions{Factory: true, Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner-standards", Provider: "claude", Model: "fixture"}}, StateRoot: root, Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }}, at); e != nil {
			t.Fatal(e)
		}
	}
	setup()
	if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	next := 100
	say := func(text string) string {
		t.Helper()
		next++
		at = at.Add(time.Second)
		reader := &standardsJourneyReader{message: store.NaturalMessage{ChannelRef: "3", SourceID: fmt.Sprint(next), OwnerID: "7", Text: text, ReplyTo: fmt.Sprint(500 + len(sender.texts)), CreatedAt: at}}
		intake := messaging.Intake{DB: db, Reader: reader, Owner: "7", Transport: "discord", Channels: []string{"3"}, Protect: service.Protect}
		if e := intake.Poll(ctx, at); e != nil {
			t.Fatal(e)
		}
		first := len(sender.texts)
		for range 100 {
			work, e := service.ConversationStep(ctx, at)
			if e != nil {
				t.Fatal(e)
			}
			delivered, e := service.ConversationDeliveryStep(ctx, at)
			if e != nil {
				t.Fatal(e)
			}
			if !work && !delivered {
				break
			}
		}
		return strings.Join(sender.texts[first:], "\n")
	}
	read := func(action, text string, args map[string]string) string {
		t.Helper()
		provider.command = "standards"
		provider.action = action
		provider.arguments = args
		return say(text)
	}
	change := func(command, action, text string, args map[string]string) {
		t.Helper()
		provider.command = command
		provider.action = action
		provider.arguments = args
		preview := say(text)
		if preview == "" {
			t.Fatal("mutation preview absent")
		}
		say("yes")
	}
	discovery := read("discover", "What standards operations can I use?", nil)
	if !strings.Contains(strings.ToLower(discovery), "standard") {
		t.Fatalf("standards discovery absent: %s", discovery)
	}
	change("standards-author", "start", "Start a create draft named conversation-author about Make checks explicit in quality.", map[string]string{"id": "conversation-author", "mode": "create", "intent": "Make checks explicit", "category_ids": "quality"})
	draft, err := author.Show(ctx, "conversation-author", 0)
	if err != nil {
		t.Fatal(err)
	}
	change("standards-author", "continue", fmt.Sprintf("Continue conversation-author version %d.", draft.Version), map[string]string{"id": draft.ID, "version": fmt.Sprint(draft.Version)})
	draft, err = author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Questions) != 1 {
		t.Fatalf("expected saved question: %+v", draft)
	}
	question := draft.Questions[0]
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	setup()
	read("author.inspect", "Show saved draft conversation-author.", map[string]string{"id": draft.ID, "version": fmt.Sprint(draft.Version)})
	resumed, err := author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Version != draft.Version || resumed.Questions[0].ID != question.ID {
		t.Fatal("restart changed saved question")
	}
	change("standards-author", "answer", fmt.Sprintf("For conversation-author version %d answer %s with Relevant package checks.", draft.Version, question.ID), map[string]string{"id": draft.ID, "version": fmt.Sprint(draft.Version), "question_id": question.ID, "text": "Relevant package checks"})
	draft, err = author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	change("standards-author", "continue", fmt.Sprintf("Continue conversation-author version %d.", draft.Version), map[string]string{"id": draft.ID, "version": fmt.Sprint(draft.Version)})
	draft, err = author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	change("standards-author", "complete", fmt.Sprintf("Complete conversation-author version %d into a proposal.", draft.Version), map[string]string{"id": draft.ID, "version": fmt.Sprint(draft.Version)})
	draft, err = author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Proposal == nil {
		t.Fatal("author completion did not retain shared proposal")
	}
	proposal := *draft.Proposal
	reviewText := read("proposal.review", fmt.Sprintf("Review proposal %s version %d in full.", proposal.ProposalID, proposal.Version), map[string]string{"id": proposal.ProposalID, "version": fmt.Sprint(proposal.Version)})
	if !strings.Contains(reviewText, "Run relevant package checks before sharing changes.") {
		t.Fatalf("complete review body absent: %s", reviewText)
	}
	change("standards-proposal", "approve", fmt.Sprintf("Approve proposal %s version %d with digest %s.", proposal.ProposalID, proposal.Version, proposal.ChangeDigest), map[string]string{"id": proposal.ProposalID, "version": fmt.Sprint(proposal.Version), "digest": proposal.ChangeDigest})
	reviewed, err := book.Inspect(ctx, proposal.ProposalID, proposal.Version)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Approval == nil {
		t.Fatalf("exact proposal not approved: %+v", reviewed.Proposal)
	}
	change("standards-proposal", "publish", fmt.Sprintf("Publish proposal %s version %d with digest %s.", proposal.ProposalID, proposal.Version, proposal.ChangeDigest), map[string]string{"id": proposal.ProposalID, "version": fmt.Sprint(proposal.Version), "digest": proposal.ChangeDigest})
	current, err := book.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.Publication == nil || current.Publication.ProposalID != proposal.ProposalID || current.Publication.Version != proposal.Version || current.Publication.ChangeDigest != proposal.ChangeDigest {
		t.Fatalf("publication does not match reviewed proposal: %+v", current.Publication)
	}
	result := read("book.current", "Inspect the resulting published standards book revision.", nil)
	if !strings.Contains(result, fmt.Sprint(current.ID)) || !strings.Contains(result, current.Digest) || !strings.Contains(result, proposal.ProposalID) {
		t.Fatalf("published revision absent: %s", result)
	}
}
