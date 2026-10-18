package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestJudgeRetainedInstructionValidation(t *testing.T) {
	pkg, err := instructionpackage.New(t.Context(), config.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), instructionpackage.JudgeRole)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(capture.Instructions())
	cfg := judgeConfiguration{Role: string(capture.Instructions()), RoleDigest: hex.EncodeToString(sum[:]), Instructions: raw, InstructionDigest: capture.Snapshot().EffectiveDigest}
	if _, err := validateJudgeInstructions(cfg); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*judgeConfiguration)
	}{
		{"historical", func(c *judgeConfiguration) { c.Instructions = nil }},
		{"damaged", func(c *judgeConfiguration) { c.Instructions = []byte(`{"schema":1}`) }},
		{"role", func(c *judgeConfiguration) { c.Role += "changed" }},
		{"role digest", func(c *judgeConfiguration) { c.RoleDigest = "wrong" }},
		{"closure digest", func(c *judgeConfiguration) { c.InstructionDigest = "wrong" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := cfg
			test.change(&invalid)
			if _, err := validateJudgeInstructions(invalid); err == nil {
				t.Fatal("invalid retained instructions accepted")
			}
		})
	}
}

func TestJudgeHistoricalCaptureRefusesLaunchAndAllowsRecovery(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	source, err := service.DB.ResolveJudgeSource(t.Context(), request.Ref.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := service.Config.ResolveJudge(request.Options.JudgeOptions, request.TaskProvider, config.Kind(source.Provider))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := json.Marshal(judgeConfiguration{Requested: request.Options, Effective: invocation, Role: "historical role"})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(JudgeEvidenceSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DB.AdmitJudgeRequest(t.Context(), store.JudgeRequest{CallerKind: "manual", Task: request.Ref.String(), TicketKey: request.TicketKey, CommentID: request.RemoteCommentID, SessionID: request.SessionID, TriggerCommentID: request.CommentID, Source: source, Configuration: configuration, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	runner := &recoveryJudgeRunner{}
	service.Providers[agent.Kind(invocation.Provider)] = runner
	if _, err = service.Execute(t.Context(), request); err == nil || !strings.Contains(err.Error(), "historical") {
		t.Fatalf("historical launch refusal: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("launched historical capture")
	}
	request.RecoverOnly = true
	recovered, err := service.Execute(t.Context(), request)
	if err != nil || !strings.Contains(recovered, "no assessment was rerun") {
		t.Fatalf("report-only recovery: %s %v", recovered, err)
	}
	request.RecoverOnly = false
	if _, err = service.Execute(t.Context(), request); err != nil {
		t.Fatalf("saved historical report: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("recovery launched historical capture")
	}
}

func TestJudgeCaptureAdmissionBoundaries(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	request.GraphInstanceID = 1
	if _, err := service.captureJudgeConfiguration(t.Context(), request, config.JudgeInvocation{}); err == nil {
		t.Fatal("workflow missing capture accepted")
	}
	request.RecoverOnly = true
	cfg, err := service.captureJudgeConfiguration(t.Context(), request, config.JudgeInvocation{})
	if err != nil || len(cfg.Instructions) != 0 {
		t.Fatalf("historical report-only capture: %+v %v", cfg, err)
	}
	if _, err := validateJudgeInstructions(cfg); err == nil {
		t.Fatal("historical report-only configuration can launch")
	}
	request.GraphInstanceID = 0
	request.RecoverOnly = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.captureJudgeConfiguration(ctx, request, config.JudgeInvocation{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture: %v", err)
	}
	cfg.Instructions = make([]byte, 2*instructionpackage.MaxCaptureBytes+1)
	if _, err := validateJudgeInstructions(cfg); err == nil {
		t.Fatal("oversized retained capture accepted")
	}
	if _, err := service.launch(t.Context(), store.JudgeRequest{}, &cfg, JudgeEvidenceSnapshot{}, config.JudgeInvocation{}); err == nil {
		t.Fatal("invalid capture reached launch allocation")
	}
}

func TestJudgePersistenceFailurePreventsProvider(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	runner := &recoveryJudgeRunner{}
	service.Providers[agent.Kind("codex")] = runner
	service.Providers[agent.Kind("claude")] = runner
	if err := service.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(t.Context(), request); err == nil {
		t.Fatal("closed durable store accepted execution")
	}
	if len(runner.calls) != 0 {
		t.Fatal("launched without durable admission")
	}
}
