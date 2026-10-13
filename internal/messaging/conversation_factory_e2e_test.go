package messaging_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

// External native programs are controlled fixtures. Production adapters enforce
// their launch contract; this fixture makes no native isolation or model-memory claim.
func factoryConversationExecutable(t *testing.T, provider string, responses []string) string {
	t.Helper()
	directory := t.TempDir()
	for i, response := range responses {
		escaped, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, fmt.Sprintf("response-%d", i+1)), escaped, 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(directory, "provider-fixture")
	script := fmt.Sprintf(`#!/bin/sh
provider=%s
responses='%s'
for argument in "$@"; do
 if [ "$argument" = --version ]; then printf '%%s\n' '2.1.296 (Claude Code)'; exit 0; fi
 if [ "$argument" = --help ]; then
  printf '%%s\n' '--settings --agents --system-prompt --restricted --disable-slash-commands --strict-mcp-config --setting-sources --tools --resume --session-id --no-chrome --safe-mode --ignore-user-config --ignore-rules --strict-config --json --profile'
  exit 0
 fi
done
if [ "$1" = sandbox ]; then printf '%%s\n' 'conversation-isolation-verified'; exit 0; fi
session=
resume=
codex_resume=
previous=
for argument in "$@"; do
 if [ "$previous" = --session-id ]; then session=$argument; fi
 if [ "$previous" = --resume ]; then resume=$argument; fi
 if [ "$argument" = resume ]; then codex_resume=yes; fi
 if [ "$codex_resume" = yes ] && [ "$argument" = - ]; then resume=$previous; fi
 previous=$argument
done
state=${CLAUDE_CONFIG_DIR:-$CODEX_HOME}
state=${state%%/home}
[ -n "$state" ] || exit 20
step=0
if [ -f "$state/fixture-step" ]; then step=$(cat "$state/fixture-step"); fi
step=$((step + 1))
if [ -n "$resume" ]; then
 [ "$(cat "$state/fixture-session")" = "$resume" ] || exit 21
 session=$resume
 printf 'resume %%s\n' "$session" >> "$state/fixture-launches"
else
 [ "$step" = 1 ] || exit 22
 [ -n "$session" ] || session=codex-native-session
 printf '%%s' "$session" > "$state/fixture-session"
 printf 'cold %%s\n' "$session" >> "$state/fixture-launches"
fi
cat > "$state/fixture-prompt-$step"
if [ "$step" = 1 ]; then
 case "$(cat "$state/fixture-prompt-$step")" in *acme/app*42*) ;; *) exit 25;; esac
 printf 'acme/app#42' > "$state/fixture-target"
fi
if [ "$step" = 2 ]; then
 case "$(cat "$state/fixture-prompt-$step")" in *'no running tasks or owner waits found.'*) ;; *) exit 28;; esac
fi
if [ "$step" = 3 ]; then
 [ "$(cat "$state/fixture-target")" = acme/app#42 ] || exit 26
 case "$(cat "$state/fixture-prompt-$step")" in *'For that issue, answer CSV output.'*) ;; *) exit 27;; esac
fi
printf '%%s' "$step" > "$state/fixture-step"
[ -f "$responses/response-$step" ] || exit 23
answer=$(cat "$responses/response-$step")
if [ "$step" = 4 ]; then
 reference=$(sed -n 's/.*"id":"\([^" ]*\)".*/\1/p' "$state/fixture-prompt-$step" | tail -n 1)
 [ -n "$reference" ] || exit 24
 answer=$(printf '%%s' "$answer" | sed "s/@REFERENCE@/$reference/g")
fi
if [ "$provider" = claude ]; then
 printf '{"type":"system","subtype":"init","session_id":"%%s","tools":[],"mcp_servers":[],"claude_code_version":"2.1.296"}\n' "$session"
 printf '{"type":"result","session_id":"%%s","result":%%s,"is_error":false}\n' "$session" "$answer"
else
 printf '{"type":"thread.started","thread_id":"%%s"}\n' "$session"
 printf '{"type":"item.completed","item":{"type":"agent_message","text":%%s}}\n' "$answer"
 printf '%%s\n' '{"type":"turn.completed"}'
fi
`, provider, directory)
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable
}

type factoryJourneyAnswers struct {
	calls                  int
	target, revision, text string
}

func (*factoryJourneyAnswers) Current(context.Context, string) (messaging.Question, error) {
	return messaging.Question{Text: "Which output should issue 42 provide?", Token: "123456789abc"}, nil
}
func (a *factoryJourneyAnswers) Answer(_ context.Context, target, revision, text string, _ time.Time) (messaging.Question, error) {
	a.calls++
	a.target, a.revision, a.text = target, revision, text
	return messaging.Question{Reason: "answer recorded"}, nil
}

func assertFactoryJourneySessions(t *testing.T, state, session string, count int) {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(state, "fixture-launches"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != count {
		t.Fatalf("native invocation count %d, want %d: %s", len(lines), count, log)
	}
	for i, line := range lines {
		want := "resume " + session
		if i == 0 {
			want = "cold " + session
		}
		if line != want {
			t.Fatalf("invocation %d changed native session: %q, want %q", i+1, line, want)
		}
	}
}

// One main path covers both native adapters, the production intake, coordinator,
// store, proposal delivery, deterministic authorization and existing action handler.
func TestConversationFactoryOwnerJourney(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			ctx := t.Context()
			db := database(t)
			at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: root, Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.GetOrCreateSession(ctx, repo, 42); err != nil {
				t.Fatal(err)
			}
			followup := "For that issue, answer CSV output."
			start := strings.Index(followup, "CSV output")
			responses := []string{
				`{"version":1,"kind":"lookup","command":"status","arguments":[]}`,
				`{"version":1,"kind":"answer","text":"No tasks are running or waiting on the owner. We are discussing acme/app issue 42."}`,
				`{"version":1,"kind":"lookup","command":"question","arguments":[{"name":"project","value":"acme/app"},{"name":"issue","value":"42"}]}`,
				fmt.Sprintf(`{"version":1,"kind":"propose","command":"answer","arguments":[{"name":"project","reference":"@REFERENCE@"},{"name":"issue","reference":"@REFERENCE@"},{"name":"revision","reference":"@REFERENCE@"},{"name":"text","source_id":"102","start":%d,"end":%d}]}`, start, start+len("CSV output")),
				`{"version":1,"kind":"answer","text":"Your CSV output answer was recorded for acme/app issue 42."}`,
			}
			executable := factoryConversationExecutable(t, provider, responses)
			effects := &factoryJourneyAnswers{}
			sent := &journeySender{}
			service := messaging.New(db, "discord", "7", []string{"3"}, sent, messaging.FactoryStatus{DB: db})
			service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
			service.EnableResearch(nil, effects)
			if err = service.EnableConversations(ctx, messaging.ConversationOptions{
				Bindings:    []messaging.ConversationBinding{{Channel: "3", Name: "factory-work", Provider: provider, Model: "fixture"}},
				StateRoot:   filepath.Join(root, "conversations"),
				Providers:   map[string]messaging.ConversationProvider{provider: conversationAdapter(t, provider, executable, root)},
				CheckAccess: func(context.Context, string) error { return nil }, Factory: true,
			}, at); err != nil {
				t.Fatal(err)
			}
			if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			reader := &journeyReader{}
			intake := messaging.Intake{DB: db, Reader: reader, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: service.Protect}
			drain := func() {
				t.Helper()
				for range 20 {
					work, stepErr := service.ConversationStep(ctx, at)
					if stepErr != nil {
						t.Fatal(stepErr)
					}
					delivery, deliveryErr := service.ConversationDeliveryStep(ctx, at)
					if deliveryErr != nil {
						t.Fatal(deliveryErr)
					}
					if !work && !delivery {
						return
					}
				}
				t.Fatal("factory conversation failed to settle")
			}
			admit := func(id, text, reply string) {
				t.Helper()
				reader.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, ReplyTo: reply, CreatedAt: at}}
				if pollErr := intake.Poll(ctx, at); pollErr != nil {
					t.Fatal(pollErr)
				}
				drain()
			}
			admit("101", "What is the current factory state for acme/app issue 42?", "")
			conversation, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || conversation.SessionHandle == "" {
				t.Fatalf("session missing: %+v %v", conversation, err)
			}
			if len(sent.texts) != 1 || sent.texts[0] != "No tasks are running or waiting on the owner. We are discussing acme/app issue 42." {
				t.Fatalf("lookup leaked or answer missing: %v", sent.texts)
			}
			firstTurn, err := db.LatestConversationTurn(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			exchanges, err := db.ListConversationExchanges(ctx, firstTurn.ID)
			if err != nil {
				t.Fatal(err)
			}
			evidenceFound := false
			for _, exchange := range exchanges {
				if exchange.Kind == "evidence" && strings.Contains(exchange.Result, `"command":"status"`) && strings.Contains(exchange.Result, `"source":"factory:status"`) && !exchange.CapturedAt.IsZero() {
					evidenceFound = true
				}
			}
			if !evidenceFound {
				t.Fatalf("answer lost current lookup evidence: %+v", exchanges)
			}
			admit("102", followup, "")
			proposal, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
			if err != nil {
				t.Fatal(err)
			}
			if proposal.State != "awaiting_confirmation" || proposal.PromptID == "" || effects.calls != 0 {
				t.Fatalf("proposal was not delivered before authorization: %+v effects=%d replies=%v", proposal, effects.calls, sent.texts)
			}
			for _, content := range []string{"Proposed action: answer", "acme/app", "42", "CSV output", "123456789abc", proposal.Code} {
				if !strings.Contains(sent.texts[len(sent.texts)-1], content) {
					t.Fatalf("protected proposal omitted %q: %v", content, sent.texts)
				}
			}
			admit("103", "yes", proposal.PromptID)
			if effects.calls != 1 || effects.target != "github:acme/app#42" || effects.revision != "123456789abc" || effects.text != "CSV output" {
				t.Fatalf("confirmed a different effect: %+v", effects)
			}
			after, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || after.SessionHandle != conversation.SessionHandle {
				t.Fatalf("confirmation changed session: %+v %v", after, err)
			}
			turn, err := db.LatestConversationTurn(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			link, err := db.GetConversationActionLink(ctx, turn.ID)
			if err != nil || link.ExecutionID == "" || !strings.Contains(link.Outcome, "answer recorded") {
				t.Fatalf("saved action outcome missing: %+v %v", link, err)
			}
			execution, err := db.ReadNaturalExecution(ctx, link.ExecutionID)
			if err != nil || execution.State != "finished" || execution.Outcome != link.Outcome {
				t.Fatalf("saved execution differs from narration link: %+v %v", execution, err)
			}
			if len(sent.texts) != 3 || !strings.Contains(sent.texts[2], "Your CSV output answer was recorded for acme/app issue 42.") || !strings.Contains(sent.texts[2], "answer recorded") {
				t.Fatalf("saved outcome narration missing: %v", sent.texts)
			}
			assertFactoryJourneySessions(t, after.StateDir, after.SessionHandle, 5)
			prompt, err := os.ReadFile(filepath.Join(after.StateDir, "fixture-prompt-5"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(prompt), "answer recorded") {
				t.Fatalf("narration did not receive saved outcome: %s", prompt)
			}
		})
	}
}
