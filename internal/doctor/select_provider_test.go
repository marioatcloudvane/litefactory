package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// isolationByProvider opens isolation only for the listed providers.
type isolationByProvider map[string]bool

func (r isolationByProvider) IsolationEvidence(_ context.Context, provider string) (string, error) {
	if r[provider] {
		return "test isolation for " + provider, nil
	}
	return "", ErrIsolationUnavailable
}

func (isolationByProvider) Run(context.Context, Invocation, *ResultService) (InvocationOutcome, error) {
	return InvocationOutcome{}, errors.New("not launched in selection tests")
}

func selectionService(t *testing.T, yaml string, runner Runner) *Service {
	t.Helper()
	cfg, err := config.Decode(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Config: cfg, Runner: runner}
}

func TestSelectProviderFallsBackToNextConfiguredAgent(t *testing.T) {
	s := selectionService(t, "agents:\n  codex: {}\n  claude: {}\n", isolationByProvider{"claude": true})
	for _, preferred := range []config.Kind{"", "codex"} {
		provider, model, observation, isolationErr, err := s.selectProvider(t.Context(), preferred)
		if err != nil || isolationErr != nil {
			t.Fatalf("preferred %q: %v %v", preferred, err, isolationErr)
		}
		if provider != "claude" || model == "" || observation == "" {
			t.Fatalf("preferred %q: got %q %q %q", preferred, provider, model, observation)
		}
	}
}

func TestSelectProviderPrefersIssueAgentWhenIsolated(t *testing.T) {
	s := selectionService(t, "agents:\n  codex: {}\n  claude: {}\n", isolationByProvider{"claude": true, "codex": true})
	if provider, _, _, _, err := s.selectProvider(t.Context(), "claude"); err != nil || provider != "claude" {
		t.Fatalf("issue agent not preferred: %q %v", provider, err)
	}
	if provider, _, _, _, err := s.selectProvider(t.Context(), ""); err != nil || provider != "codex" {
		t.Fatalf("first configured agent not chosen: %q %v", provider, err)
	}
}

func TestSelectProviderReportsRefusalWhenNoneIsolated(t *testing.T) {
	s := selectionService(t, "agents:\n  codex: {}\n  claude: {}\n", isolationByProvider{})
	provider, model, _, isolationErr, err := s.selectProvider(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "codex" || model == "" || !errors.Is(isolationErr, ErrIsolationUnavailable) {
		t.Fatalf("got %q %q %v", provider, model, isolationErr)
	}
}
