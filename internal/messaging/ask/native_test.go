package ask_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

type nativeRunner struct {
	isolated bool
	calls    int
	requests []agent.ChatRequest
}

func (r *nativeRunner) DoctorIsolation(context.Context) (string, error) {
	if !r.isolated {
		return "", agent.ErrDoctorIsolationUnverified
	}
	return "controlled fixture only", nil
}
func (r *nativeRunner) RunChat(_ context.Context, request agent.ChatRequest) (agent.ChatResult, error) {
	r.calls++
	r.requests = append(r.requests, request)
	return agent.ChatResult{Answer: []byte(`{"points":[]}`)}, request.Validate()
}

type chatLedger struct {
	attempts []store.MeterAttempt
	statuses []string
}

func (l *chatLedger) StartChatMeterAttempt(_ context.Context, repoID int64, provider string) (store.MeterAttempt, error) {
	a := store.MeterAttempt{ID: fmt.Sprintf("attempt-%d", len(l.attempts)+1), ChatRepoID: repoID, CodingAgent: provider}
	l.attempts = append(l.attempts, a)
	return a, nil
}
func (*chatLedger) RecordMeterEvent(context.Context, string, string, agent.UsageEvent) error {
	return nil
}
func (l *chatLedger) FinishMeterAttempt(_ context.Context, _ string, _ string, status string, _ string, _ string, _ []string) error {
	l.statuses = append(l.statuses, status)
	return nil
}

func TestNativeAnswerUsesDoctorOrderBalancedModelAndIndependentMeterCalls(t *testing.T) {
	cfg, err := config.Decode(strings.NewReader("agents:\n  codex: {}\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	closed := &nativeRunner{}
	verified := &nativeRunner{isolated: true}
	ledger := &chatLedger{}
	model := &ask.NativeModel{Config: cfg, Paths: config.NewPaths(t.TempDir()), Providers: map[string]ask.ChatRunner{"codex": closed, "claude": verified}, Meter: ledger}
	for range 2 {
		if _, err := model.Answer(t.Context(), 9, "bounded data"); err != nil {
			t.Fatal(err)
		}
	}
	balanced, _ := cfg.Tiers.ResolveModel("claude", config.TierBalanced)
	if closed.calls != 0 || verified.calls != 2 || len(ledger.attempts) != 2 || ledger.attempts[0].ID == ledger.attempts[1].ID {
		t.Fatalf("gate or meter attempt error: %+v", ledger)
	}
	for i, a := range ledger.attempts {
		if a.ChatRepoID != 9 || a.RunID != 0 || a.CodingAgent != "claude" || verified.requests[i].Model != balanced || verified.requests[i].Usage.AttemptID != a.ID || ledger.statuses[i] != "unknown" {
			t.Fatalf("wrong invocation: %+v %+v", a, verified.requests[i])
		}
	}
	entries, err := os.ReadDir(model.Paths.DoctorScratchDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("ephemeral context retained: %v %v", entries, err)
	}
	verified.isolated = false
	if _, err := model.Answer(t.Context(), 9, "bounded data"); !errors.Is(err, ask.ErrUnavailable) || len(ledger.attempts) != 2 {
		t.Fatalf("unverified call launched or metered: %v", err)
	}
}
