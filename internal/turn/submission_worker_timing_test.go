package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestNonGraphFinalizationBoundsBlockedTransportJoin(t *testing.T) {
	f := newRetainedFixture(t, "research")
	f.submit(t, f.result)
	entered := make(chan struct{})
	release := make(chan struct{})
	broker, err := submission.Start(t.Context(), submission.Options{Directory: launchFixtureDirectory(t), AllowResult: true, MaxWorkers: 1, RequestTimeout: time.Second, Handler: func(context.Context, submission.Request) (json.RawMessage, error) {
		close(entered)
		<-release
		return json.RawMessage(`{"state":"recorded"}`), nil
	}})
	mustProgress(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = submission.Call(t.Context(), broker.DescriptorPath(), "preflight", []byte(`{}`))
	}()
	defer func() { close(release); mustProgress(t, broker.Close()); <-done }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("transport handler did not start")
	}
	worker, cancel := agent.WithWorkerDeadline(t.Context(), time.Now().Add(50*time.Millisecond))
	defer cancel()
	p := &preparedMCP{service: f.service, resultBroker: broker}
	result, err := p.finalize(worker, &preparation{}, nil, &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "provider failed"}, nil)
	if err != nil || result == nil || !strings.Contains(result.Diagnostic, "factory helper cleanup unavailable") {
		t.Fatalf("blocked transport did not surface bounded cleanup: %+v %v", result, err)
	}
	receipt, err := f.service.Receipt(t.Context(), f.opts.Attempt.RunID, "req-1")
	mustProgress(t, err)
	if receipt.Receipt == nil || receipt.Receipt.Latest.Input != "accepted" || receipt.Receipt.Latest.Engineering != "pending" || receipt.Receipt.Latest.Acceptance != "pending" || receipt.Receipt.Latest.Publication != "pending" {
		t.Fatalf("transport timeout changed durable input or invented workflow acceptance: %+v", receipt.Receipt)
	}
}
