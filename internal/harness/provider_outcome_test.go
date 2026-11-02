package harness

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestProviderStructuredTerminalEvidence(t *testing.T) {
	stream := eventStream{sink: io.Discard, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := stream.consume(strings.NewReader("{\"type\":\"result\",\"is_error\":true,\"error\":{\"status_code\":503}}\n")); err != nil {
		t.Fatal(err)
	}
	if stream.providerFailure != agent.ProviderHTTP5xx {
		t.Fatalf("class %s", stream.providerFailure)
	}
	if err := stream.consume(strings.NewReader("{\"type\":\"result\",\"is_error\":true}\n")); err != nil {
		t.Fatal(err)
	}
	if stream.providerFailure != agent.ProviderUnknown {
		t.Fatalf("unknown terminal inherited prior failure: %s", stream.providerFailure)
	}
}
