package messaging

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// authorGate is the standards domain with one open author session in the
// conversation.
type authorGate struct {
	*standardsGateOperations
	session StandardsAuthorSession
}

func (a *authorGate) AuthorSession(context.Context, string) (StandardsAuthorSession, bool, error) {
	return a.session, true, nil
}

func withAuthorSession(f *judgedConversation, running bool) *authorGate {
	gate := &authorGate{standardsGateOperations: f.ops, session: StandardsAuthorSession{ID: "conversation-discord:3:nl-exec:abc", Version: 4, Running: running, Rule: "MUST keep tests fast", Questions: []string{"1. Which suites? (a) Unit (b) All", "2. Enforced how? (a) CI (b) Review"}, QuestionIDs: []string{"q-1", "q-2"}}}
	f.s.EnableStandards(gate)
	return gate
}

// answerAuthor answers the author session's questions with the owner's whole message.
func answerAuthor(p factoryConversationPrompt) string {
	if p.AuthorSession == nil {
		return `{"version":1,"kind":"answer","text":"no author session"}`
	}
	id, start, end := span(p, p.OwnerInput)
	return fmt.Sprintf(`{"version":1,"kind":"propose","command":"standards-author","arguments":[{"name":"action","value":"answer"},{"name":"id","reference":%q},{"name":"text","source_id":%q,"start":%d,"end":%d}]}`, p.AuthorSession.Reference, id, start, end)
}

func TestOwnerAnswersReachTheAuthorWithoutAProposal(t *testing.T) {
	f := newJudgedConversation(t)
	withAuthorSession(f, false)
	f.say(t, "1a, 2b", "", answerAuthor)
	if len(f.ops.executed) != 1 {
		t.Fatalf("answer not passed on: %+v", f.ops.executed)
	}
	got := f.ops.executed[0]
	if got.Operation != "author.answer" || got.Arguments["id"] != "conversation-discord:3:nl-exec:abc" || got.Arguments["text"] != "1a, 2b" || got.MessageID == "" || !strings.Contains(got.OperationKey, ":nl-direct:") {
		t.Fatalf("answer request: %+v", got)
	}
	if _, err := f.s.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3"); err == nil {
		t.Fatal("answering created a proposal")
	}
	// The agent saw the session's questions and that nothing is running.
	p := f.provider.prompts[0]
	if p.AuthorSession == nil || p.AuthorSession.Running || len(p.AuthorSession.Questions) != 2 {
		t.Fatalf("author session not in the prompt: %+v", p.AuthorSession)
	}
	// The turn carries the step's identity, so the draft posted later waits
	// for this reply.
	turn := strings.TrimPrefix(got.OperationKey, "discord:3:nl-direct:")
	link, err := f.s.db.GetConversationActionLink(t.Context(), turn)
	if err != nil || link.ExecutionID != "nl-direct:"+turn {
		t.Fatalf("direct step has no execution identity: %+v %v", link, err)
	}
}

func TestOwnerChangesToAnAuthorProposalReachTheAuthorWithoutAProposal(t *testing.T) {
	f := newJudgedConversation(t)
	withAuthorSession(f, false)
	f.say(t, "make it shorter and crisper", "", func(p factoryConversationPrompt) string {
		id, start, end := span(p, p.OwnerInput)
		return fmt.Sprintf(`{"version":1,"kind":"propose","command":"standards-author","arguments":[{"name":"action","value":"revise"},{"name":"id","reference":%q},{"name":"text","source_id":%q,"start":%d,"end":%d}]}`, p.AuthorSession.Reference, id, start, end)
	})
	if len(f.ops.executed) != 1 {
		t.Fatalf("change not passed on: %+v", f.ops.executed)
	}
	got := f.ops.executed[0]
	if got.Operation != "author.revise" || got.Arguments["id"] != "conversation-discord:3:nl-exec:abc" || got.Arguments["text"] != "make it shorter and crisper" || !strings.Contains(got.OperationKey, ":nl-direct:") {
		t.Fatalf("revise request: %+v", got)
	}
	if _, err := f.s.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3"); err == nil {
		t.Fatal("revising created a proposal")
	}
}

func TestAuthorRunningIsReportedOnlyWhenItRuns(t *testing.T) {
	f := newJudgedConversation(t)
	withAuthorSession(f, true)
	f.say(t, "how is the draft coming along?", "", func(p factoryConversationPrompt) string {
		if p.AuthorSession == nil || !p.AuthorSession.Running {
			return `{"version":1,"kind":"answer","text":"Nothing is running."}`
		}
		return `{"version":1,"kind":"answer","text":"The author is still on it."}`
	})
	if f.last() != "The author is still on it." {
		t.Fatalf("running author not shown to the agent: %q", f.last())
	}
}

func TestStartCompleteApproveAndPublishStayConfirmed(t *testing.T) {
	for _, action := range []string{"start", "complete"} {
		t.Run(action, func(t *testing.T) {
			f := newJudgedConversation(t)
			withAuthorSession(f, false)
			text := "make it a standard: tests stay fast"
			f.say(t, text, "", func(p factoryConversationPrompt) string {
				id, start, end := span(p, "tests stay fast")
				if action == "start" {
					return fmt.Sprintf(`{"version":1,"kind":"propose","command":"standards-author","arguments":[{"name":"action","value":"start"},{"name":"intent","source_id":%q,"start":%d,"end":%d}]}`, id, start, end)
				}
				return fmt.Sprintf(`{"version":1,"kind":"propose","command":"standards-author","arguments":[{"name":"action","value":"complete"},{"name":"id","reference":%q},{"name":"version","reference":%q}]}`, p.AuthorSession.Reference, p.AuthorSession.Reference)
			})
			if len(f.ops.executed) != 0 {
				t.Fatalf("%s ran without confirmation: %+v", action, f.ops.executed)
			}
			if d := f.dialogue(t); d.State != "awaiting_confirmation" {
				t.Fatalf("%s not proposed: %+v", action, d)
			}
			f.say(t, "yes", "", answer("unused"))
			if len(f.ops.executed) != 1 || f.ops.executed[0].Operation != "author."+action {
				t.Fatalf("%s not run once after yes: %+v", action, f.ops.executed)
			}
		})
	}
	for _, action := range []string{"approve", "publish"} {
		inv := Invocation{Command: "standards-proposal", Arguments: map[string]string{"action": action}}
		if standardsDirect(inv) {
			t.Fatalf("proposal %s runs without confirmation", action)
		}
	}
}
