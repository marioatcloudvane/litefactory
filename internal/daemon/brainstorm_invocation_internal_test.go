package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
)

func decodeConfig(t *testing.T, yaml string) *config.Config {
	t.Helper()
	cfg, err := config.Decode(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Without judge.agent, brainstorm and task discussion use the factory's default agent.
func TestFactoryAdvisoryInvocationUsesDefaultAgentWithoutJudgeAgent(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"agents:\n  codex: {}\n", "codex"},
		{"agents:\n  claude: {}\n  codex: {}\n", "claude"},
	} {
		got, err := factoryAdvisoryInvocation(decodeConfig(t, tc.yaml))
		if err != nil {
			t.Fatalf("%q: %v", tc.yaml, err)
		}
		if string(got.Provider) != tc.want || got.Model == "" {
			t.Fatalf("%q: got %+v, want provider %s", tc.yaml, got, tc.want)
		}
	}
}

// An explicit judge.agent still wins over the default agent.
func TestFactoryAdvisoryInvocationHonoursExplicitJudgeAgent(t *testing.T) {
	got, err := factoryAdvisoryInvocation(decodeConfig(t, "agents:\n  claude: {}\n  codex: {}\njudge:\n  agent: codex\n"))
	if err != nil || got.Provider != config.KindCodex {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// An explicit but unusable setting refuses with the reason.
func TestFactoryAdvisoryInvocationRefusesUnusableJudgeAgent(t *testing.T) {
	_, err := factoryAdvisoryInvocation(decodeConfig(t, "agents:\n  claude: {}\njudge:\n  agent: other\n"))
	if err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatalf("err = %v", err)
	}
}

type recordingConnectorServices struct {
	remoteConnectorServices
	reason string
}

func (r *recordingConnectorServices) SetBrainstormUnavailable(reason string) { r.reason = reason }

// A failed brainstorm setup hands its reason to the connector.
func TestReportBrainstormUnavailableReachesConnector(t *testing.T) {
	server := &recordingConnectorServices{}
	d := &Daemon{remoteConnector: &remoteConnectorRuntime{server: server}}
	d.reportBrainstormUnavailable(context.Background(), errors.New(`unsupported judge provider "x"`), redact.New(redact.Options{Environ: []string{}}))
	if server.reason != `unsupported judge provider "x"` {
		t.Fatalf("reason = %q", server.reason)
	}
}
