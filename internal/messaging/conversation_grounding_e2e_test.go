package messaging_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

const groundingSecret = "fixture-private-api-key"
const groundingFact = "Issue 42 has seven pending checks. Caveat: this is a captured observation."

type groundingResearch struct{ calls int }

func (r *groundingResearch) Current(context.Context, string) (messaging.Question, error) {
	r.calls++
	return messaging.Question{Text: groundingFact + " Secret: " + groundingSecret, Token: "123456789abc"}, nil
}
func (*groundingResearch) Answer(context.Context, string, string, string, time.Time) (messaging.Question, error) {
	return messaging.Question{}, fmt.Errorf("the read journey must not execute an action")
}

type groundingSender struct {
	texts    []string
	payloads []presentation.Payload
}

func (s *groundingSender) Deliver(_ context.Context, _ messaging.Target, text string) messaging.Delivery {
	s.texts = append(s.texts, text)
	return messaging.Delivery{Confirmed: true, PromptID: fmt.Sprintf("grounding-%d", len(s.texts))}
}
func (s *groundingSender) DeliverReply(_ context.Context, _ messaging.Target, payload presentation.Payload) messaging.Delivery {
	s.payloads = append(s.payloads, payload)
	return messaging.Delivery{Confirmed: true, PromptID: fmt.Sprintf("details-%d", len(s.payloads))}
}

// The fixture supplies native stream events; adapter launches, retained session,
// intake, typed lookup, SQLite outbox and details delivery are production code.
func groundingExecutable(t *testing.T, provider, explanation string) string {
	t.Helper()
	directory := t.TempDir()
	response, err := json.Marshal(map[string]any{"version": 1, "kind": "answer", "blocks": []map[string]any{{"fact_refs": []string{"@FACT@"}}, {"text": explanation}}})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(string(response))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "answer"), wire, 0600); err != nil {
		t.Fatal(err)
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
printf '%%s' "$step" > "$state/fixture-step"
if [ "$step" = 1 ]; then
 answer='"{\"version\":1,\"kind\":\"lookup\",\"command\":\"question\",\"arguments\":[{\"name\":\"project\",\"value\":\"acme/app\"},{\"name\":\"issue\",\"value\":\"42\"}]}"'
elif [ "$step" = 2 ]; then
 fact=$(sed -n 's/.*"fact_id":"\([^" ]*\)".*/\1/p' "$state/fixture-prompt-$step")
 [ -n "$fact" ] || exit 23
 answer=$(sed "s/@FACT@/$fact/g" "$responses/answer")
else
 exit 24
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
	if err = os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable
}

func TestConversationGroundingDetailsOwnerJourney(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			ctx := t.Context()
			at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
			db := database(t)
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
			explanation := strings.Repeat("Seven checks imply **task 987** will finish tomorrow. 界🙂 ", 90)
			executable := groundingExecutable(t, provider, explanation)
			send := &groundingSender{}
			reader := &groundingResearch{}
			service := messaging.New(db, "discord", "7", []string{"3"}, send, messaging.FactoryStatus{DB: db})
			service.Protect = func(_ context.Context, text string) (string, error) {
				return strings.ReplaceAll(text, groundingSecret, "[redacted]"), nil
			}
			service.EnableResearch(nil, reader)
			if err = service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "factory-work", Provider: provider, Model: "fixture"}}, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{provider: conversationAdapter(t, provider, executable, root)}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
				t.Fatal(err)
			}
			if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			messages := &journeyReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: "Explain the checks for acme/app issue 42.", CreatedAt: at}}}
			intake := messaging.Intake{DB: db, Reader: messages, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: service.Protect}
			if err = intake.Poll(ctx, at); err != nil {
				t.Fatal(err)
			}
			for range 40 {
				work, e := service.ConversationStep(ctx, at)
				if e != nil {
					t.Fatal(e)
				}
				delivery, e := service.ConversationDeliveryStep(ctx, at)
				if e != nil {
					t.Fatal(e)
				}
				if !work && !delivery {
					break
				}
			}
			if len(send.texts) < 3 {
				t.Fatalf("complete multipart answer missing: %q", send.texts)
			}
			var selector string
			var deliveredExplanation strings.Builder
			var explanationFragments strings.Builder
			for i, text := range send.texts {
				if len(utf16.Encode([]rune(text))) > 2000 {
					t.Fatalf("part %d exceeds Discord budget", i+1)
				}
				if !strings.Contains(text, fmt.Sprintf("part %d of %d", i+1, len(send.texts))) {
					t.Fatalf("part %d lost ordered numbering: %q", i+1, text)
				}
				marker := "/details answer:"
				start := strings.Index(text, marker)
				if start < 0 {
					t.Fatalf("part %d lost answer selector: %q", i+1, text)
				}
				got := strings.Fields(text[start+len(marker):])[0]
				if selector == "" {
					selector = got
				}
				if got != selector {
					t.Fatalf("parts refer to different answers: %q != %q", got, selector)
				}
				if strings.Contains(text, "Seven checks") && !strings.Contains(text, "Unverified") {
					t.Fatalf("part %d lost warning: %q", i+1, text)
				}
				deliveredExplanation.WriteString(text)
				_, content, found := strings.Cut(text, "\n\n")
				if !found {
					t.Fatalf("part %d has no content", i+1)
				}
				label, fragment, found := strings.Cut(content, "\n")
				if !found {
					t.Fatalf("part %d has no grounding label", i+1)
				}
				if strings.HasPrefix(label, "Unverified") {
					explanationFragments.WriteString(strings.ReplaceAll(fragment, `\*`, "*"))
				}
			}
			if explanationFragments.String() != explanation {
				t.Fatalf("multipart answer changed accepted explanation: got %d bytes, want %d", len(explanationFragments.String()), len(explanation))
			}
			combined := strings.ReplaceAll(strings.ReplaceAll(deliveredExplanation.String(), `\[`, "["), `\]`, "]")
			for _, want := range []string{groundingFact, "factory:question", "acme/app", "Unverified", "[redacted]"} {
				if !strings.Contains(combined, want) {
					t.Fatalf("answer omitted %q: %s", want, combined)
				}
			}
			if strings.Contains(combined, groundingSecret) {
				t.Fatal("answer disclosed unredacted tool data")
			}
			conversation, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil {
				t.Fatal(err)
			}
			assertFactoryJourneySessions(t, conversation.StateDir, conversation.SessionHandle, 2)
			turn, err := db.LatestConversationTurn(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			exchanges, err := db.ListConversationExchanges(ctx, turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			var evidence string
			for _, exchange := range exchanges {
				if exchange.Kind == "evidence" {
					evidence = exchange.Result
				}
			}
			if evidence == "" || strings.Contains(evidence, groundingSecret) {
				t.Fatalf("saved redacted evidence missing: %q", evidence)
			}
			if _, inserted, e := service.Admit(ctx, messaging.Message{Transport: "discord", ID: "102", Channel: "3", Sender: "7", Command: "details", Arguments: map[string]string{"answer": selector}}, at); e != nil || !inserted {
				t.Fatalf("details admission: inserted=%t err=%v", inserted, e)
			}
			for range 40 {
				more, e := service.Step(ctx, at)
				if e != nil {
					t.Fatal(e)
				}
				delivery, deliveryErr := service.ConversationDeliveryStep(ctx, at)
				if deliveryErr != nil {
					t.Fatal(deliveryErr)
				}
				if !more && !delivery {
					break
				}
			}
			if len(send.payloads) == 0 {
				t.Fatal("details did not export an attachment")
			}
			var export strings.Builder
			var exportJSON strings.Builder
			for _, payload := range send.payloads {
				if payload.Attachment == nil || payload.Attachment.Name != presentation.RawFilename {
					t.Fatalf("unexpected details payload: %+v", payload)
				}
				if len(payload.Attachment.Content) > 64*1024 {
					t.Fatal("details attachment exceeds raw-content bound")
				}
				export.WriteString(payload.Attachment.Content)
				_, segment, found := strings.Cut(payload.Attachment.Content, "\n\n")
				if !found {
					t.Fatal("details attachment lacks export manifest")
				}
				exportJSON.WriteString(segment)
			}
			raw := export.String()
			if !strings.Contains(raw, selector) || !strings.Contains(raw, "factory:question") || !strings.Contains(raw, "captured_at") || !strings.Contains(raw, "arguments") || !strings.Contains(raw, "[redacted]") {
				t.Fatalf("details lost identity or metadata: %s", raw)
			}
			var saved map[string]any
			if err = json.Unmarshal([]byte(evidence), &saved); err != nil {
				t.Fatal(err)
			}
			var exported struct {
				AnswerID string           `json:"answer_id"`
				Results  []map[string]any `json:"results"`
			}
			if err = json.Unmarshal([]byte(exportJSON.String()), &exported); err != nil {
				t.Fatal(err)
			}
			if exported.AnswerID != selector || len(exported.Results) != 1 || !reflect.DeepEqual(exported.Results[0], saved) {
				t.Fatalf("details differs from this answer's complete saved result: %+v, want %+v", exported, saved)
			}
			if strings.Contains(raw, groundingSecret) || strings.Contains(raw, "owner_sources") || strings.Contains(raw, "instructions") || strings.Contains(raw, explanation) {
				t.Fatal("details disclosed data outside saved redacted tool results")
			}
			if reader.calls != 1 {
				t.Fatalf("details repeated lookup: %d reads", reader.calls)
			}
			assertFactoryJourneySessions(t, conversation.StateDir, conversation.SessionHandle, 2)
		})
	}
}
