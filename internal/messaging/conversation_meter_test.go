package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationMeterFollowupRetainsTaskAndRechecksAccess(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	if _, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	var received []Message
	run.s.RegisterCommand(CommandDefinition{Name: "meter", NaturalLanguage: true, Effect: ReadEffect, Arguments: []Argument{{Name: "project", Type: TextArgument}, {Name: "task", Type: IntegerArgument}}}, func(_ context.Context, message Message, _ time.Time) (string, error) {
		received = append(received, message)
		return "Current task usage is unavailable.", nil
	})
	_, issued, err := run.lookup(t.Context(), Invocation{Command: "meter", Arguments: map[string]string{"project": "acme/app", "task": "42"}})
	if err != nil || issued == nil {
		t.Fatalf("initial lookup reference=%+v err=%v", issued, err)
	}
	refs, err := run.references(t.Context())
	if err != nil || len(refs) != 1 {
		t.Fatalf("retained references=%+v err=%v", refs, err)
	}
	if refs[0].ID != issued.ID || refs[0].Arguments["project"] != "acme/app" || refs[0].Arguments["task"] != "42" {
		t.Fatalf("task target changed in retained reference: %+v", refs[0])
	}
	raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "lookup", Command: "meter", Arguments: []ConversationArgument{{Name: "project", Reference: refs[0].ID}, {Name: "task", Reference: refs[0].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	_, followup, err := DecodeConversationEnvelope(raw, ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "102", Text: "And that task?"}}, Commands: run.s.ConversationCommands(), References: refs, OwnerID: run.s.owner, ChannelID: run.c.ChannelRef, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	run.ordinal++
	if _, _, err = run.lookup(t.Context(), followup); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[1].Arguments["project"] != "acme/app" || received[1].Arguments["task"] != "42" {
		t.Fatalf("follow-up read changed target: %+v", received)
	}
	// A retained target never replaces the current channel authorization check.
	run.s.conversations.Load().check = func(context.Context, string) error { return errors.New("access revoked") }
	if _, _, err = run.lookup(t.Context(), followup); err == nil {
		t.Fatal("retained reference bypassed revoked access")
	}
	if len(received) != 2 {
		t.Fatal("reader called after access was revoked")
	}
}
