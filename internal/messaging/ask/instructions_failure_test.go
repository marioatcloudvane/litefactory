package ask_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type instructionFailureModel struct{ calls int }

func (m *instructionFailureModel) Answer(context.Context, int64, string) ([]byte, error) {
	m.calls++
	return []byte(`{"points":[],"gaps":[]}`), nil
}

func TestAskInstructionFailurePreventsModelLaunch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, instructions string
		cancel             bool
	}{
		{name: "invalid shipped format", instructions: "<!-- main -->\nNo correction section.\n"},
		{name: "canceled capture", instructions: "<!-- main -->\nMain instructions.\n<!-- correction -->\nCorrection instructions.\n", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkg, err := instructionpackage.Load(t.Context(), fstest.MapFS{
				"metadata.yaml":      {Data: []byte("schema: 1\nversion: ask-fixture\n")},
				"catalogue.yaml":     {Data: []byte("- chat-ask\n")},
				"agents/chat-ask.md": {Data: []byte(tc.instructions)},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			model := &instructionFailureModel{}
			r := redact.New(redact.Options{})
			svc := &ask.Service{Instructions: pkg, Model: model, Redactor: r}
			reply, repoID, err := svc.ReplyPrepared(ctx, ask.Prepared{
				Repository: store.Repo{ID: 7},
				Bundle:     answerBundle(),
				Options:    ask.RenderOptions{Question: "What is open?", Redactor: r},
			})
			if err == nil || (tc.cancel && !errors.Is(err, context.Canceled)) {
				t.Fatalf("instruction failure returned %v, want error (canceled=%t)", err, tc.cancel)
			}
			if model.calls != 0 || reply != "" || repoID != 7 {
				t.Fatalf("failed capture launched or produced an answer: calls=%d reply=%q repo=%d", model.calls, reply, repoID)
			}
		})
	}
}
