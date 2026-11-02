package codex

import (
	"io"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestProviderStructuredTerminalEvidence(t *testing.T) {
	stream := consume(strings.NewReader("{\"type\":\"turn.failed\",\"error\":{\"status_code\":503}}\n"), io.Discard)
	if stream.providerFailure != agent.ProviderHTTP5xx {
		t.Fatalf("class %s", stream.providerFailure)
	}
	var incremental streamState
	incremental.consumeLine([]byte(`{"type":"turn.failed","error":{"type":"authentication_error"}}`))
	if incremental.providerFailure != agent.ProviderAuthentication {
		t.Fatalf("class %s", incremental.providerFailure)
	}
	incremental.consumeLine([]byte(`{"type":"turn.failed","error":{"message":"unclassified"}}`))
	if incremental.providerFailure != agent.ProviderUnknown {
		t.Fatalf("unknown terminal inherited prior failure: %s", incremental.providerFailure)
	}
}
