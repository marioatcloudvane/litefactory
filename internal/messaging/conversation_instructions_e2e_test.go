package messaging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

// This recorded native seam proves supplied bytes and durable identity, not model recall.
func instructionJourneyExecutable(t *testing.T, provider string) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "instruction-provider")
	script := fmt.Sprintf(`#!/bin/sh
provider=%s
for argument in "$@"; do
 if [ "$argument" = --version ]; then printf '%%s\n' '2.1.299 (Claude Code)'; exit 0; fi
 if [ "$argument" = --help ]; then
  printf '%%s\n' '--settings --agents --system-prompt --restricted --disable-slash-commands --strict-mcp-config --setting-sources --tools --resume --session-id --no-chrome --safe-mode --ignore-user-config --ignore-rules --strict-config --json --profile'
  exit 0
 fi
done
if [ "$1" = sandbox ]; then printf '%%s\n' 'conversation-isolation-verified'; exit 0; fi
# Per-conversation state: the provider runs in <state>/scratch.
state=$(dirname "$(pwd -P)")
step=0
if [ -f "$state/instruction-step" ]; then step=$(cat "$state/instruction-step"); fi
step=$((step + 1))
printf '%%s' "$step" > "$state/instruction-step"
printf '%%s\n' "$@" > "$state/instruction-args-$step"
cat > "$state/instruction-prompt-$step"
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
if [ -n "$resume" ]; then
 [ "$(cat "$state/instruction-session")" = "$resume" ] || exit 21
 session=$resume
else
 [ "$step" = 1 ] || exit 22
 [ -n "$session" ] || session=instruction-native-session
 printf '%%s' "$session" > "$state/instruction-session"
fi
answer='{"version":1,"kind":"answer","text":"Recorded instruction fixture reply."}'
if [ "$step" = 1 ]; then answer='{'; fi
# JSON-string encoding for the two fixed fixture responses.
if [ "$step" = 1 ]; then wire='"{"'; else wire='"{\"version\":1,\"kind\":\"answer\",\"text\":\"Recorded instruction fixture reply.\"}"'; fi
if [ "$provider" = claude ]; then
 printf '{"type":"system","subtype":"init","session_id":"%%s","tools":[],"mcp_servers":[],"claude_code_version":"2.1.299"}\n' "$session"
 printf '{"type":"result","session_id":"%%s","result":%%s,"is_error":false}\n' "$session" "$wire"
else
 printf '{"type":"thread.started","thread_id":"%%s"}\n' "$session"
 printf '{"type":"item.completed","item":{"type":"agent_message","text":%%s}}\n' "$wire"
 printf '%%s\n' '{"type":"turn.completed"}'
fi
`, provider)
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable
}

// A retained binding keeps its capture within a turn and moves onto the
// instructions a restarted daemon loaded at its next turn boundary, keeping
// its conversation and native session.
func TestConversationInstructionsRefreshAtTurnBoundaryAfterRestart(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			ctx := t.Context()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			owner := filepath.Join(root, "owner-instructions")
			roles := []string{"conversation-native", "conversation-factory", "conversation-correction", "conversation-action-outcome"}
			catalogue := "- " + strings.Join(roles, "\n- ") + "\n"
			baseline := fstest.MapFS{"metadata.yaml": {Data: []byte("schema: 1\nversion: instruction-fixture\n")}, "catalogue.yaml": {Data: []byte(catalogue)}}
			for _, role := range roles {
				baseline["agents/"+role+".md"] = &fstest.MapFile{Data: []byte("Shipped " + role + ".\n")}
			}
			writeSources := func(version string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(owner, "agents"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(owner, "catalogue.yaml"), []byte(catalogue), 0600); err != nil {
					t.Fatal(err)
				}
				for _, role := range roles {
					if err := os.WriteFile(filepath.Join(owner, "agents", role+".md"), []byte(version+" "+role+".\n\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			writeSources("Original")
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
				db, err = store.Open(ctx, filepath.Join(root, "factory.db"))
				if err != nil {
					t.Fatal(err)
				}
				if err = db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			open()
			at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			executable := instructionJourneyExecutable(t, provider)
			sent := &journeySender{}
			start := func(second bool) *messaging.Service {
				t.Helper()
				pkg, loadErr := instructionpackage.Load(ctx, baseline, owner)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				service := messaging.New(db, "discord", "7", []string{"3", "4"}, sent, messaging.FactoryStatus{DB: db})
				service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
				bindings := []messaging.ConversationBinding{{Channel: "3", Name: "retained", Provider: provider, Model: "fixture"}}
				if second {
					bindings[0].Name = "renamed"
					bindings = append(bindings, messaging.ConversationBinding{Channel: "4", Name: "new", Provider: provider, Model: "fixture"})
				}
				if enableErr := service.EnableConversations(ctx, messaging.ConversationOptions{Factory: true, Instructions: pkg, Bindings: bindings, StateRoot: filepath.Join(root, "conversations"), Providers: map[string]messaging.ConversationProvider{provider: conversationAdapter(t, provider, executable, root)}, CheckAccess: func(context.Context, string) error { return nil }}, at); enableErr != nil {
					t.Fatal(enableErr)
				}
				return service
			}
			service := start(false)
			if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			turn := func(channel, id string) {
				t.Helper()
				reader := &journeyReader{messages: []store.NaturalMessage{{ChannelRef: channel, SourceID: id, OwnerID: "7", Text: "Explain this fixture.", CreatedAt: at}}}
				intake := messaging.Intake{DB: db, Reader: reader, Transport: "discord", Owner: "7", Channels: []string{channel}, Protect: service.Protect}
				if err := intake.Poll(ctx, at); err != nil {
					t.Fatal(err)
				}
				for range 10 {
					work, e := service.ConversationStep(ctx, at)
					if e != nil {
						t.Fatal(e)
					}
					delivery, e := service.ConversationDeliveryStep(ctx, at)
					if e != nil {
						t.Fatal(e)
					}
					if !work && !delivery {
						if len(sent.texts) == 0 || !strings.Contains(sent.texts[len(sent.texts)-1], "Recorded instruction fixture reply.") {
							t.Fatalf("instruction turn %s/%s did not complete the recorded main path: replies=%q", channel, id, sent.texts)
						}
						return
					}
				}
				t.Fatal("instruction journey did not settle")
			}
			turn("3", "101")
			before, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil {
				t.Fatal(err)
			}
			rawBefore, _, err := db.ReadConversationCapture(ctx, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			verifyCapture := func(raw []byte, version string) {
				t.Helper()
				capture, e := instructionpackage.RestoreConversation(raw)
				if e != nil {
					t.Fatal(e)
				}
				got := []string{capture.Native(), capture.Factory(), capture.Correction(), capture.ActionOutcome()}
				for i, role := range roles {
					if got[i] != version+" "+role+".\n\n" {
						t.Fatalf("%s capture = %q", role, got[i])
					}
				}
			}
			verifyCapture(rawBefore, "Original")
			writeSources("Edited")
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = nil
			open()
			service = start(true)
			turn("3", "102")
			after, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil {
				t.Fatal(err)
			}
			rawAfter, _, err := db.ReadConversationCapture(ctx, after.ID)
			if err != nil {
				t.Fatal(err)
			}
			if before.ID != after.ID || before.SessionHandle != after.SessionHandle || after.Name != "renamed" || after.HealthReason != "" {
				t.Fatal("restart changed the conversation or its native identity")
			}
			if bytes.Equal(rawBefore, rawAfter) {
				t.Fatal("new instructions did not reach the retained binding")
			}
			verifyCapture(rawAfter, "Edited")
			if err := db.ActivateNaturalScan(ctx, "4", "200"); err != nil {
				t.Fatal(err)
			}
			turn("4", "201")
			fresh, err := db.ReadConversation(ctx, "discord", "4")
			if err != nil {
				t.Fatal(err)
			}
			freshRaw, _, err := db.ReadConversationCapture(ctx, fresh.ID)
			if err != nil {
				t.Fatal(err)
			}
			verifyCapture(freshRaw, "Edited")
			for _, check := range []struct {
				state    string
				versions []string
			}{{before.StateDir, []string{"Original", "Original", "Edited"}}, {fresh.StateDir, []string{"Edited", "Edited"}}} {
				for step := 1; step <= len(check.versions); step++ {
					version := check.versions[step-1]
					args, e := os.ReadFile(filepath.Join(check.state, fmt.Sprintf("instruction-args-%d", step)))
					if e != nil {
						t.Fatal(e)
					}
					native := version + " conversation-native.\n\n"
					want := "--system-prompt\n" + native + "\n"
					if provider == "codex" {
						want = "developer_instructions=" + strconv.Quote(native) + "\n"
					}
					if !strings.Contains(string(args), want) {
						t.Fatalf("%s launch %d omitted captured native instructions: %s", provider, step, args)
					}
					prompt, e := os.ReadFile(filepath.Join(check.state, fmt.Sprintf("instruction-prompt-%d", step)))
					if e != nil {
						t.Fatal(e)
					}
					var fields map[string]json.RawMessage
					if e = json.Unmarshal(prompt, &fields); e != nil {
						t.Fatal(e)
					}
					var factory string
					if e = json.Unmarshal(fields["instructions"], &factory); e != nil {
						t.Fatal(e)
					}
					if factory != version+" conversation-factory.\n\n" {
						t.Fatalf("factory composition changed: %q", factory)
					}
					if step == 2 {
						if len(fields["correction"]) == 0 {
							t.Fatalf("%s launch %d has no correction boundary; prompt=%s", provider, step, prompt)
						}
						var correction string
						if e = json.Unmarshal(fields["correction"], &correction); e != nil {
							t.Fatal(e)
						}
						if correction != version+" conversation-correction.\n\n" {
							t.Fatalf("correction composition changed: %q", correction)
						}
					}
				}
			}
			if len(sent.texts) != 3 {
				t.Fatalf("delivered %d replies, want three", len(sent.texts))
			}
		})
	}
}
