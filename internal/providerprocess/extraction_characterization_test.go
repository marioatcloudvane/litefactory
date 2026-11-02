package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestExtractionConversationBarrierPublishesPIDBeforeExec(t *testing.T) {
	state := conversationTestDirectory(t)
	request := agent.ConversationRequest{ConversationID: "conversation", TurnID: "turn", Model: "model", StateDirectory: state, Prompt: "owner", NativeInstructions: "fixture", Session: agent.SessionRef{Ref: "session"}, OnSession: func(context.Context, agent.SessionRef) error { return nil }, BeforeLaunch: func(context.Context) error { return nil }}
	script := `test "$(cat "$1/provider.pid")" = "$$" || exit 71; printf 'pid acknowledged\n'`
	var output string
	err := RunConversationProcess(t.Context(), "/bin/sh", []string{"-c", script, "provider", state}, nil, request, state, 2*time.Second, func(line []byte) { output += string(line) })
	if err != nil {
		t.Fatal(err)
	}
	if output != "pid acknowledged" {
		t.Fatalf("output %q", output)
	}
	if _, err := os.Stat(filepath.Join(state, "provider.pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed marker retained: %v", err)
	}
}

func TestExtractionConversationJournalFailureCannotExecProvider(t *testing.T) {
	state := conversationTestDirectory(t)
	launched := filepath.Join(state, "launched")
	request := agent.ConversationRequest{ConversationID: "conversation", TurnID: "turn", Model: "model", StateDirectory: state, Prompt: "owner", NativeInstructions: "fixture", Session: agent.SessionRef{Ref: "session"}, OnSession: func(context.Context, agent.SessionRef) error { return nil }, BeforeLaunch: func(context.Context) error { return os.Mkdir(filepath.Join(state, "provider.pid"), 0700) }}
	err := RunConversationProcess(t.Context(), "/bin/sh", []string{"-c", `touch "$1"`, "provider", launched}, nil, request, state, 2*time.Second, func([]byte) {})
	var failure *agent.ConversationFailure
	if !errors.As(err, &failure) || failure.Phase != "ownership_persistence" || failure.NoProcessStarted {
		t.Fatalf("failure evidence: %v, %#v", err, failure)
	}
	if _, err := os.Stat(launched); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider escaped journal barrier: %v", err)
	}
}
