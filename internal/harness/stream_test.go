package harness

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const (
	initLine = `{"type":"system","subtype":"init","cwd":"/w","session_id":"1a17a10a-0000-5000-8000-000000000001",` +
		`"tools":["Read","Edit"],"model":"claude-sonnet-4-6","permissionMode":"default","apiKeySource":"none"}`
	resultLine = `{"type":"result","subtype":"success","is_error":false,"duration_ms":1673,"num_turns":1,` +
		`"result":"DONE","total_cost_usd":0.04622315}`
)

func consumeLines(t *testing.T, lines ...string) (*eventStream, *bytes.Buffer) {
	t.Helper()
	var sink bytes.Buffer
	stream := &eventStream{sink: &sink, logger: discardLogger()}
	if err := stream.consume(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	return stream, &sink
}
