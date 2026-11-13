package codex

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/denypolicy"
)

func TestCodexSelectedDenyPolicyRefusesBeforeProcess(t *testing.T) {
	r, argv := fakeRunner(t, "exit 0\n")
	req := request(t, agent.PathCold)
	policy, err := denypolicy.Resolve(denypolicy.Catalogue{"checks": {Deny: []string{"Bash(make check:*)"}}}, "", "checks", "worker")
	if err != nil {
		t.Fatal(err)
	}
	req.DenyPolicy = policy
	result, err := r.Run(t.Context(), req)
	if err == nil || result != nil {
		t.Fatalf("Run = %v, %v", result, err)
	}
	for _, part := range []string{`node "worker" lf_deny`, `checks`, `entry 1`, `Bash(make check:*)`, `Codex cannot enforce`, `select Claude`} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("refusal %q lacks %q", err, part)
		}
	}
	if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex process started: %v", err)
	}
}

func TestCodexSelectedJudgeDenyPolicyRefusesBeforeProcess(t *testing.T) {
	r, argv := fakeRunner(t, "exit 0\n")
	policy, err := denypolicy.Resolve(denypolicy.Catalogue{"checks": {Deny: []string{"Agent"}}}, "checks", "", "judge")
	if err != nil {
		t.Fatal(err)
	}
	// Refusal precedes even capability probes and private-home preparation.
	request := agent.JudgeRequest{DenyPolicy: policy}
	if _, err := r.RunJudge(t.Context(), request); err == nil || !strings.Contains(err.Error(), "Codex cannot enforce") {
		t.Fatalf("judge policy accepted: %v", err)
	}
	if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex judge process started: %v", err)
	}
}
