package messaging_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

// The executable fixtures model native persistence, not provider isolation or a
// model's memory. Intake, adapters, SQLite, service and outbox are production code.
func TestConversationRestartJourney(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			ctx := t.Context()
			root, rootErr := filepath.EvalSymlinks(t.TempDir())
			if rootErr != nil {
				t.Fatal(rootErr)
			}
			databasePath := filepath.Join(root, "factory.db")
			stateRoot := filepath.Join(root, "conversations")
			executable := conversationExecutable(t, provider)
			at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
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
				var err error
				db, err = store.Open(ctx, databasePath)
				if err != nil {
					t.Fatal(err)
				}
				if err = db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			open()
			sent := &journeySender{}
			start := func() *messaging.Service {
				t.Helper()
				service := messaging.New(db, "discord", "7", []string{"3"}, sent, messaging.FactoryStatus{DB: db})
				service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
				runner := conversationAdapter(t, provider, executable, root)
				if err := service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner-work", Provider: provider, Model: "fixture"}}, StateRoot: stateRoot, Providers: map[string]messaging.ConversationProvider{provider: runner}, CheckAccess: func(context.Context, string) error { return nil }}, at); err != nil {
					t.Fatal(err)
				}
				return service
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
			}
			drain := func() {
				t.Helper()
				for range 10 {
					work, err := service.ConversationStep(ctx, at)
					if err != nil {
						t.Fatal(err)
					}
					delivery, err := service.ConversationDeliveryStep(ctx, at)
					if err != nil {
						t.Fatal(err)
					}
					if !work && !delivery {
						return
					}
				}
				t.Fatal("conversation failed to settle")
			}
			admit("101", "Remember that my chosen flower is marigold.")
			drain()
			before, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || before.SessionHandle == "" || before.HealthReason != "" {
				t.Fatalf("first session not recorded: %+v %v", before, err)
			}
			if len(sent.texts) != 1 || sent.texts[0] != "I will remember your flower." {
				t.Fatalf("first reply: %v", sent.texts)
			}
			// The fixture emits a recorded native compaction event, replaces its
			// earlier fact file with a summary and keeps the same native identity.
			admit("102", "Compact our earlier conversation while keeping the chosen flower.")
			drain()
			if len(sent.texts) != 2 || sent.texts[1] != "Earlier context compacted." {
				t.Fatalf("compaction reply: %v", sent.texts)
			}
			if _, err := os.Stat(filepath.Join(before.StateDir, "fixture-flower")); !os.IsNotExist(err) {
				t.Fatalf("fixture retained original context instead of compacting: %v", err)
			}
			summary, err := os.ReadFile(filepath.Join(before.StateDir, "fixture-summary"))
			if err != nil || string(summary) != "marigold" {
				t.Fatalf("compacted context missing: %q %v", summary, err)
			}
			compaction, err := os.ReadFile(filepath.Join(before.StateDir, "fixture-compaction"))
			if err != nil || string(compaction) != before.SessionHandle {
				t.Fatalf("compaction changed native identity: %q %v", compaction, err)
			}
			// Every adapter invocation exits its executable. Reopening the database and
			// constructing a fresh adapter/service now models a factory/provider restart.
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = nil
			open()
			service = start()
			admit("103", "What flower did I choose?")
			drain()
			after, err := db.ReadConversation(ctx, "discord", "3")
			if err != nil || after.ID != before.ID || after.SessionHandle != before.SessionHandle || after.StateDir != before.StateDir || after.Name != "owner-work" || after.HealthReason != "" {
				t.Fatalf("restart changed continuity: before=%+v after=%+v error=%v", before, after, err)
			}
			if len(sent.texts) != 3 || sent.texts[2] != "Your flower is marigold." {
				t.Fatalf("follow-up lost retained context: %v", sent.texts)
			}
			log, err := os.ReadFile(filepath.Join(before.StateDir, "fixture-launches"))
			if err != nil {
				t.Fatal(err)
			}
			launches := strings.Split(strings.TrimSpace(string(log)), "\n")
			if len(launches) != 3 || launches[0] != "cold "+before.SessionHandle || launches[1] != "resume "+before.SessionHandle || launches[2] != "resume "+before.SessionHandle {
				t.Fatalf("did not resume exact native session once: %q", log)
			}
		})
	}
}

func conversationAdapter(t *testing.T, provider, executable, home string) messaging.ConversationProvider {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if provider == "claude" {
		claudeConfig := config.Claude{Binary: executable, TimeoutSeconds: 60, Tools: []string{"Read"}}
		runner, err := harness.New(harness.Options{Paths: config.NewPaths(home), Claude: claudeConfig, Model: "fixture", Environ: []string{"PATH=/usr/bin:/bin"}, Logger: logger})
		if err != nil {
			t.Fatal(err)
		}
		return runner
	}
	codexConfig := config.Codex{Binary: executable, TimeoutSeconds: 60}
	runner, err := codex.New(codex.Options{Paths: config.NewPaths(home), Codex: codexConfig, Model: "fixture", Environ: []string{"PATH=/usr/bin:/bin"}, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func conversationExecutable(t *testing.T, provider string) string {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "provider-fixture")
	// The native instructions are taken from the shipped package at test time,
	// encoded exactly as the conversation prompt carries them.
	needle := filepath.Join(dir, "native-instructions")
	if err := os.WriteFile(needle, []byte(shippedNativeInstructionsEncoded(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
provider=%s
native_needle='%s'
# Capability responses are fixture contracts, not native security evidence.
for argument in "$@"; do
 if [ "$argument" = --version ]; then printf '%%s\n' '2.1.296 (Claude Code)'; exit 0; fi
 if [ "$argument" = --help ]; then
  printf '%%s\n' '--settings --agents --system-prompt --restricted --disable-slash-commands --strict-mcp-config --setting-sources --tools --resume --session-id --no-chrome --safe-mode --ignore-user-config --ignore-rules --strict-config --json --profile'
  exit 0
 fi
done
if [ "$1" = sandbox ]; then
 printf '%%s\n' 'conversation-isolation-verified'
 exit 0
fi
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
prompt=$(cat)
# Every owner turn must carry fixed factory instructions after compaction too.
native=$(cat "$native_needle") || exit 26
[ -n "$native" ] || exit 26
case "$prompt" in *"$native"*) ;; *) exit 26;; esac
if [ -n "$resume" ]; then
 printf 'resume %%s\n' "$resume" >> "$state/fixture-attempts"
 [ "$(cat "$state/fixture-session")" = "$resume" ] || exit 21
 if [ -f "$state/fixture-summary" ]; then
  [ "$(cat "$state/fixture-summary")" = marigold ] || exit 22
 else
  [ "$(cat "$state/fixture-flower")" = marigold ] || exit 22
 fi
 case "$prompt" in *marigold*) exit 23;; esac
 session=$resume
 answer='Your flower is marigold.'
 printf 'resume %%s\n' "$session" >> "$state/fixture-launches"
else
 printf 'cold\n' >> "$state/fixture-attempts"
 [ ! -f "$state/fixture-session" ] || exit 24
 case "$prompt" in *marigold*) ;; *) exit 25;; esac
 if [ -z "$session" ]; then session=codex-native-session; fi
 printf '%%s' "$session" > "$state/fixture-session"
 printf 'marigold' > "$state/fixture-flower"
 printf 'cold %%s\n' "$session" >> "$state/fixture-launches"
 answer='I will remember your flower.'
fi
compact=
case "$prompt" in *'Compact our earlier conversation'*)
 compact=yes
 printf 'marigold' > "$state/fixture-summary"
 rm "$state/fixture-flower" || exit 27
 printf '%%s' "$session" > "$state/fixture-compaction"
 answer='Earlier context compacted.'
 ;; esac
interrupted=
case "$prompt" in *'Interrupt before answering this input'*) interrupted=yes;; esac
if [ "$provider" = claude ]; then
 printf '{"type":"system","subtype":"init","session_id":"%%s","tools":[],"mcp_servers":[],"claude_code_version":"2.1.296"}\n' "$session"
 if [ "$compact" = yes ]; then printf '{"type":"system","subtype":"compact_boundary","session_id":"%%s","compact_metadata":{"trigger":"auto"}}\n' "$session"; fi
 [ "$interrupted" = yes ] && exit 0
 printf '{"type":"result","session_id":"%%s","result":"%%s","is_error":false}\n' "$session" "$answer"
else
 printf '{"type":"thread.started","thread_id":"%%s"}\n' "$session"
 if [ "$compact" = yes ]; then printf '%%s\n' '{"type":"item.completed","item":{"type":"context_compaction"}}'; fi
 [ "$interrupted" = yes ] && exit 0
 printf '{"type":"item.completed","item":{"type":"agent_message","text":"%%s"}}\n' "$answer"
 printf '%%s\n' '{"type":"turn.completed"}'
fi
`, provider, needle)
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable
}

// journeySender records delivered texts and confirms each with a fresh prompt ID.
type journeySender struct{ texts []string }

func (p *journeySender) Deliver(_ context.Context, _ messaging.Target, text string) messaging.Delivery {
	p.texts = append(p.texts, text)
	return messaging.Delivery{Confirmed: true, PromptID: fmt.Sprintf("%d", 900+len(p.texts))}
}

// A newest-first external page exercises the production ordered collector.
type journeyReader struct{ messages []store.NaturalMessage }

func (*journeyReader) CheckIntakeCapability(context.Context, string) error { return nil }
func (r *journeyReader) ReadIntakePage(_ context.Context, channel, owner, before string) (messaging.IntakePage, error) {
	if before != "" || len(r.messages) == 0 {
		return messaging.IntakePage{}, nil
	}
	return messaging.IntakePage{Messages: r.messages, HighID: r.messages[0].SourceID, LowID: r.messages[len(r.messages)-1].SourceID, Count: len(r.messages)}, nil
}

// shippedNativeInstructionsEncoded returns the shipped native conversation
// instructions JSON-encoded without the surrounding quotes, the form in which
// every conversation prompt carries them.
func shippedNativeInstructionsEncoded(t *testing.T) string {
	t.Helper()
	pkg, err := instructionpackage.ShippedConversationPackage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.CaptureConversation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(capture.Native())
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 3 {
		t.Fatal("shipped native conversation instructions are empty")
	}
	return string(encoded[1 : len(encoded)-1])
}
