package response

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type authorRunner struct {
	verified bool
	calls    int
	answer   []byte
	request  agent.ChatRequest
}

func (r *authorRunner) DoctorIsolation(context.Context) (string, error) {
	if !r.verified {
		return "", agent.ErrDoctorIsolationUnverified
	}
	return "fixture isolation", nil
}
func (r *authorRunner) RunChat(_ context.Context, req agent.ChatRequest) (agent.ChatResult, error) {
	r.calls++
	r.request = req
	return agent.ChatResult{Answer: r.answer}, req.Validate()
}

type authorLedger struct {
	session, provider string
	finished          bool
}

func (*authorLedger) StartChatMeterAttempt(context.Context, int64, string) (store.MeterAttempt, error) {
	return store.MeterAttempt{}, errors.New("authoring cannot fabricate chat ownership")
}
func (l *authorLedger) StartAuthoringMeterAttempt(_ context.Context, session, provider, model, inputDigest, attemptID, token string) (store.MeterAttempt, error) {
	if model == "" || len(inputDigest) != 64 {
		return store.MeterAttempt{}, errors.New("missing prepared input or model")
	}
	l.session, l.provider = session, provider
	return store.MeterAttempt{ID: "authoring-usage"}, nil
}
func (*authorLedger) RecordMeterEvent(context.Context, string, string, agent.UsageEvent) error {
	return nil
}
func (l *authorLedger) FinishMeterAttempt(context.Context, string, string, string, string, string, []string) error {
	l.finished = true
	return nil
}

func TestAuthoringProviderBoundsIsolationAndAttribution(t *testing.T) {
	t.Parallel()
	cfg, err := config.Decode(strings.NewReader("agents:\n  codex: {}\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	closed := &authorRunner{}
	verified := &authorRunner{verified: true, answer: []byte(`{"schema_version":1}`)}
	ledger := &authorLedger{}
	m := &NativeModel{Config: cfg, Paths: config.NewPaths(t.TempDir()), Providers: map[string]ChatRunner{"codex": closed, "claude": verified}, Meter: ledger}
	a := &Authoring{Model: m}
	call := standards.AuthoringCall{Session: "session", Attempt: "attempt", Token: "token", Prompt: "bounded data"}
	if _, err = a.Answer(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	provider, model, reference := a.Metadata()
	if provider != "claude" || model == "" {
		t.Errorf("missing provenance: %s %s", provider, model)
	}
	if closed.calls != 0 || verified.calls != 1 || ledger.session != "session" || ledger.provider != "claude" || !ledger.finished || reference != "authoring-usage" {
		t.Fatalf("incorrect isolation or attribution: %+v %+v", verified, ledger)
	}
	if verified.request.Usage == nil || verified.request.Usage.AttemptID != reference {
		t.Fatal("usage capture missing")
	}
	if verified.request.MCPLaunch != nil {
		t.Fatal("a session without a project was granted tools")
	}
	entries, err := os.ReadDir(m.Paths.DoctorScratchDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch retained: %v %v", entries, err)
	}
	// A project-scoped call never runs without its read tools.
	scoped := call
	scoped.Project = &projectread.Project{ID: 1, Name: "acme/app"}
	if _, err = a.Answer(t.Context(), scoped); err == nil || verified.calls != 1 {
		t.Fatal("project-scoped call ran without project reads")
	}
	oversized := call
	oversized.Prompt = strings.Repeat("x", (256<<10)+1)
	if _, err = a.Answer(t.Context(), oversized); err == nil || verified.calls != 1 {
		t.Fatal("oversized prompt launched")
	}
	verified.answer = make([]byte, (32<<10)+1)
	if _, err = a.Answer(t.Context(), call); err == nil {
		t.Fatal("oversized response accepted")
	}
	verified.verified = false
	if _, err = a.Answer(t.Context(), call); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unverified fallback: %v", err)
	}
}
