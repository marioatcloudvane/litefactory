package providerprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func conversationTestDirectory(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConversationIdleRefusesLiveRecordedProvider(t *testing.T) {
	state := conversationTestDirectory(t)
	cmd := exec.CommandContext(t.Context(), "/bin/sleep", "1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "provider.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConversationIdle(state); !errors.Is(err, agent.ErrConversationBusy) {
		t.Fatalf("live process accepted: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := ConversationIdle(state); err != nil {
		t.Fatalf("exited process not recovered: %v", err)
	}
}

func TestConversationFenceRefusesProcessBeforeLaunch(t *testing.T) {
	state := conversationTestDirectory(t)
	scratch := filepath.Join(state, "scratch")
	if err := os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	fenced := errors.New("stale worker")
	request := agent.ConversationRequest{ConversationID: "conversation", TurnID: "turn", Model: "model", StateDirectory: state, Prompt: "owner", Session: agent.SessionRef{Ref: "session"}, OnSession: func(context.Context, agent.SessionRef) error { return nil }, BeforeLaunch: func(context.Context) error { return fenced }}
	marker := filepath.Join(scratch, "launched")
	err := RunConversationProcess(t.Context(), "/bin/sh", []string{"-c", `touch "$1"`, "provider", marker}, nil, request, scratch, time.Second, func([]byte) {})
	if !errors.Is(err, fenced) {
		t.Fatalf("lost fence error: %v", err)
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale worker launched provider: %v", err)
	}
}
