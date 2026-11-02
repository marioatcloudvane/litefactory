package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

type recordingRunner struct {
	requests []agent.Request
}

func (r *recordingRunner) Run(ctx context.Context, request agent.Request) (agent.Result, error) {
	r.requests = append(r.requests, request)
	return (fakeRunner{}).Run(ctx, request)
}

func TestExecutionExample(t *testing.T) {
	var output bytes.Buffer
	execution := &recordingRunner{}
	directory := t.TempDir()
	if err := run(t.Context(), &output, execution, directory); err != nil {
		t.Fatal(err)
	}
	const want = "prepare: ready\nShipment A contains two parcels.\nsummarize: written\n"
	if got := output.String(); got != want {
		t.Fatalf("consumer activities and navigation: got %q, want %q", got, want)
	}
	if len(execution.requests) != 1 {
		t.Fatalf("agent invocations = %d, want 1", len(execution.requests))
	}
	request := execution.requests[0]
	if request.Prompt != "Shipment A: two parcels." || request.WorkingDirectory != directory {
		t.Fatalf("consumer prepared request = %+v", request)
	}
}
