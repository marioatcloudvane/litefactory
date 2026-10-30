package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// judgedProvider is the conversation agent: each turn's answer is chosen by
// the test from the prompt the factory sent.
type judgedProvider struct {
	mu      sync.Mutex
	answer  func(factoryConversationPrompt) string
	prompts []factoryConversationPrompt
}

func (p *judgedProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, agent.ConversationError(err, "before_launch", true)
	}
	session := r.Session
	if session.Resume == "" {
		session.Resume = "judged-session"
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	var prompt factoryConversationPrompt
	if err := json.Unmarshal([]byte(r.Prompt), &prompt); err != nil {
		return agent.ConversationResult{}, err
	}
	p.mu.Lock()
	p.prompts = append(p.prompts, prompt)
	answer := p.answer
	p.mu.Unlock()
	// The outcome narration has no owner input of its own.
	if prompt.OwnerInput == "" {
		return agent.ConversationResult{Session: session, Answer: []byte(`{"version":1,"kind":"answer","text":"Done."}`)}, nil
	}
	return agent.ConversationResult{Session: session, Answer: []byte(answer(prompt))}, nil
}

type judgedConversation struct {
	s        *Service
	ops      *standardsGateOperations
	provider *judgedProvider
	sender   *standardsDeliverySender
	next     int
	at       time.Time
}

func newJudgedConversation(t *testing.T) *judgedConversation {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	f := &judgedConversation{ops: &standardsGateOperations{}, provider: &judgedProvider{}, sender: &standardsDeliverySender{result: Delivery{Confirmed: true}}, next: 100, at: time.Now().UTC().Truncate(time.Second)}
	f.s = New(db, "discord", "7", []string{"3"}, f.sender, FactoryStatus{DB: db})
	f.s.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	f.ops.db = db
	f.s.EnableStandards(f.ops)
	installGateActions(t, f.s, func() time.Time { return f.at })
	binding := ConversationBinding{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}
	if err = f.s.EnableConversations(ctx, ConversationOptions{Bindings: []ConversationBinding{binding}, StateRoot: filepath.Join(t.TempDir(), "sessions"), Providers: map[string]ConversationProvider{"claude": f.provider}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, f.at); err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	return f
}

// say sends one owner message, runs its turn to the end and delivers the
// reply. It returns the message's source ID.
func (f *judgedConversation) say(t *testing.T, text, replyTo string, answer func(factoryConversationPrompt) string) string {
	t.Helper()
	ctx := t.Context()
	f.next++
	f.at = f.at.Add(6 * time.Second)
	id := fmt.Sprint(f.next)
	f.provider.mu.Lock()
	f.provider.answer = answer
	f.provider.mu.Unlock()
	scan, err := f.s.db.ReadNaturalScan(ctx, "3")
	if err != nil {
		t.Fatal(err)
	}
	following := scan
	following.HighWater = id
	if err = f.s.db.StageNaturalScan(ctx, scan, following, []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", ReplyTo: replyTo, Text: text, CreatedAt: f.at}}, true); err != nil {
		t.Fatal(err)
	}
	for {
		more, err := f.s.ConversationStep(ctx, f.at)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	deliverAll(t, f.s, f.sender, "reply-"+id, f.at)
	if err := f.s.ownerActions.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *judgedConversation) dialogue(t *testing.T) owneraction.Proposal {
	t.Helper()
	page, err := f.s.ownerActions.History(t.Context(), mustGatePrincipal(t, f.s), "", 100)
	if err != nil || len(page.Proposals) == 0 {
		t.Fatalf("proposal missing: %+v %v", page, err)
	}
	return page.Proposals[0]
}

func (f *judgedConversation) last() string { return f.sender.texts[len(f.sender.texts)-1] }

// span cites part of the owner's current message.
func span(p factoryConversationPrompt, part string) (string, int, int) {
	start := strings.Index(p.OwnerInput, part)
	return p.OwnerSources[len(p.OwnerSources)-1].SourceID, start, start + len(part)
}

// agentProposesAnalysis answers with a log analysis proposal for "litefactory" and
// the window named in the owner's message.
func agentProposesAnalysis(window string) func(factoryConversationPrompt) string {
	return func(p factoryConversationPrompt) string {
		id, ps, pe := span(p, "litefactory")
		_, ws, we := span(p, window)
		return fmt.Sprintf(`{"version":1,"kind":"propose","command":"standards-analysis","arguments":[{"name":"project","source_id":%q,"start":%d,"end":%d},{"name":"window","source_id":%q,"start":%d,"end":%d}]}`, id, ps, pe, id, ws, we)
	}
}

func confirmWith(reference func(factoryConversationPrompt) string, words string) func(factoryConversationPrompt) string {
	return func(p factoryConversationPrompt) string {
		id, start, end := span(p, words)
		return fmt.Sprintf(`{"version":1,"kind":"confirm","proposal":%q,"confirmation":{"source_id":%q,"start":%d,"end":%d}}`, reference(p), id, start, end)
	}
}

func openReference(p factoryConversationPrompt) string {
	if p.OpenProposal == nil {
		return "none"
	}
	return p.OpenProposal.Reference
}

func answer(text string) func(factoryConversationPrompt) string {
	return func(factoryConversationPrompt) string {
		return fmt.Sprintf(`{"version":1,"kind":"answer","text":%q}`, text)
	}
}

func TestAgentJudgedConfirmationRunsTheOpenProposal(t *testing.T) {
	for _, yes := range []string{"yeah man, do it!", "sure, go ahead", "jo passt"} {
		t.Run(yes, func(t *testing.T) {
			f := newJudgedConversation(t)
			f.say(t, "analyze litefactory for the last 7 days", "", agentProposesAnalysis("last 7 days"))
			d := f.dialogue(t)
			if d.State != "awaiting_confirmation" || d.ProposalMessage == "" {
				t.Fatalf("no open proposal: %+v", d)
			}
			f.say(t, yes, "", confirmWith(openReference, yes))
			if len(f.ops.executed) != 1 || !strings.HasSuffix(f.ops.executed[0].OperationKey, "owner-action:"+d.ID) {
				t.Fatalf("agent-judged yes did not run the stored proposal once: %+v", f.ops.executed)
			}
			if !maps.Equal(f.ops.executed[0].Arguments, d.Facts.Invocation.Arguments) {
				t.Fatalf("executed different arguments: %+v", f.ops.executed[0].Arguments)
			}
			// Single use: the same yes again runs nothing.
			f.say(t, yes, "", confirmWith(openReference, yes))
			if len(f.ops.executed) != 1 {
				t.Fatalf("proposal ran twice: %+v", f.ops.executed)
			}
		})
	}
}

func TestAgentJudgedConfirmationRefusesFabricatedSpans(t *testing.T) {
	f := newJudgedConversation(t)
	request := f.say(t, "analyze litefactory for the last 7 days", "", agentProposesAnalysis("last 7 days"))
	fabricated := map[string]func(factoryConversationPrompt) string{
		// The owner's earlier request, not the current message.
		"earlier message": func(p factoryConversationPrompt) string {
			return fmt.Sprintf(`{"version":1,"kind":"confirm","proposal":%q,"confirmation":{"source_id":%q,"start":0,"end":7}}`, openReference(p), request)
		},
		// Words the owner never wrote: past the end of the message.
		"out of bounds": func(p factoryConversationPrompt) string {
			id, _, _ := span(p, "")
			return fmt.Sprintf(`{"version":1,"kind":"confirm","proposal":%q,"confirmation":{"source_id":%q,"start":0,"end":%d}}`, openReference(p), id, len(p.OwnerInput)+9)
		},
		"quoted": confirmWith(openReference, "do it"),
	}
	texts := map[string]string{"earlier message": "hmm", "out of bounds": "hmm", "quoted": "> do it"}
	for name, fake := range fabricated {
		f.say(t, texts[name], "", fake)
		if len(f.ops.executed) != 0 {
			t.Fatalf("%s: a fabricated confirmation ran: %+v", name, f.ops.executed)
		}
	}
	// None of it cancelled the open proposal.
	if d := f.dialogue(t); d.State != "awaiting_confirmation" {
		t.Fatalf("proposal no longer open: %+v", d)
	}
}

func TestAgentJudgedConfirmationNeedsTheOneOpenProposal(t *testing.T) {
	f := newJudgedConversation(t)
	f.say(t, "analyze litefactory for the last 7 days", "", agentProposesAnalysis("last 7 days"))
	first := f.dialogue(t)
	// A changed request is a new proposal that replaces the first.
	f.say(t, "no wait, analyze litefactory for yesterday", "", agentProposesAnalysis("yesterday"))
	second := f.dialogue(t)
	if second.ID == first.ID || second.State != "awaiting_confirmation" {
		t.Fatalf("second proposal did not replace the first: %+v", second)
	}
	f.say(t, "yeah do it", "", confirmWith(func(factoryConversationPrompt) string { return first.ID }, "yeah do it"))
	if len(f.ops.executed) != 0 {
		t.Fatalf("the replaced proposal ran: %+v", f.ops.executed)
	}
	f.say(t, "yeah do it", "", confirmWith(openReference, "yeah do it"))
	if len(f.ops.executed) != 0 {
		t.Fatal("ambiguous pending selection executed")
	}
	if _, err := f.s.ownerActions.Cancel(t.Context(), mustGatePrincipal(t, f.s), first.ID); err != nil {
		t.Fatal(err)
	}
	f.say(t, "yeah do it", "", confirmWith(openReference, "yeah do it"))
	if len(f.ops.executed) != 1 || !maps.Equal(f.ops.executed[0].Arguments, second.Facts.Invocation.Arguments) {
		t.Fatalf("wrong exact proposal: %+v", f.ops.executed)
	}

	// With nothing open, a confirmation has nothing to cite.
	f.say(t, "yeah do it", "", confirmWith(openReference, "yeah do it"))
	if len(f.ops.executed) != 1 {
		t.Fatalf("a confirmation without an open proposal ran: %+v", f.ops.executed)
	}
}

func TestOtherMessagesKeepTheProposalOpen(t *testing.T) {
	f := newJudgedConversation(t)
	f.say(t, "analyze litefactory for the last 7 days", "", agentProposesAnalysis("last 7 days"))
	f.say(t, "what does that cost?", "", func(p factoryConversationPrompt) string {
		if p.OpenProposal == nil || p.OpenProposal.Text == "" {
			return `{"version":1,"kind":"answer","text":"no open proposal shown"}`
		}
		return `{"version":1,"kind":"answer","text":"About one or two dollars."}`
	})
	if f.last() != "About one or two dollars." {
		t.Fatalf("agent did not see the open proposal: %q", f.last())
	}
	if d := f.dialogue(t); d.State != "awaiting_confirmation" {
		t.Fatalf("a question cancelled the proposal: %+v", d)
	}
	// A reply to the exact proposal selects it after an intervening question.
	proposal := f.dialogue(t)
	f.say(t, "yes", proposal.ProposalMessage, answer("unused"))
	if len(f.ops.executed) != 1 {
		t.Fatalf("exact reply did not confirm: %+v", f.ops.executed)
	}
}

func TestAgentJudgedCancelDropsTheProposal(t *testing.T) {
	f := newJudgedConversation(t)
	f.say(t, "analyze litefactory for the last 7 days", "", agentProposesAnalysis("last 7 days"))
	f.say(t, "nah, forget it", "", func(p factoryConversationPrompt) string {
		return fmt.Sprintf(`{"version":1,"kind":"cancel","proposal":%q}`, openReference(p))
	})
	if d := f.dialogue(t); d.State != "cancelled" || len(f.ops.executed) != 0 {
		t.Fatalf("cancel did not drop the proposal: %+v %+v", d, f.ops.executed)
	}
}

func TestPlainWordsStillConfirmWithoutTheAgent(t *testing.T) {
	f := newJudgedConversation(t)
	f.say(t, "analyze litefactory for the last 7 days", "", agentProposesAnalysis("last 7 days"))
	calls := len(f.provider.prompts)
	f.say(t, "yes", "", answer("the agent was asked"))
	if len(f.ops.executed) != 1 {
		t.Fatalf("plain yes did not confirm: %+v", f.ops.executed)
	}
	// Only the outcome narration reached the agent.
	for _, p := range f.provider.prompts[calls:] {
		if p.OwnerInput != "" {
			t.Fatalf("plain yes went to the agent: %+v", p)
		}
	}
}
