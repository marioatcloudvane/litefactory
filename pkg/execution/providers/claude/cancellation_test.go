package claude

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/pkg/execution/agent"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreparedRunRetainsCallerCancellationCause(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "provider")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"native\"}'\n/bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := New(Options{Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	sentinel := errors.New("caller stopped this invocation")
	result, err := runner.Run(ctx, agent.Request{Prompt: "prepared", WorkingDirectory: t.TempDir(), Timeout: 5 * time.Second, OnSession: func(context.Context, string) error { cancel(sentinel); return nil }})
	if !result.Started || !errors.Is(err, context.Canceled) || !errors.Is(err, sentinel) {
		t.Fatalf("lost cancellation evidence: %+v %v", result, err)
	}
}
