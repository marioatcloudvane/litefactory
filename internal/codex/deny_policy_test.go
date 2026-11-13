package codex

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
)

func TestCodexSelectedDenyPolicyRefusesBeforeProcess(t *testing.T) {
	r, argv := selectedTestRunner(t, "exit 0\n")
	req := request(t, agent.PathCold)
	policy, err := denypolicy.Resolve(denypolicy.Catalogue{"checks": {Deny: []string{"Read(secrets/*)"}}}, "", "checks", "worker")
	if err != nil {
		t.Fatal(err)
	}
	req.DenyPolicy = policy
	result, err := r.Run(t.Context(), req)
	if err == nil || result != nil {
		t.Fatalf("Run = %v, %v", result, err)
	}
	for _, part := range []string{`node "worker" lf_deny`, `checks`, `entry 1`, `Read(secrets/*)`, `Codex cannot enforce`} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("refusal %q lacks %q", err, part)
		}
	}
	if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex process started: %v", err)
	}
}

func TestCodexSelectedJudgeDenyPolicyRefusesBeforeProcess(t *testing.T) {
	r, argv := selectedTestRunner(t, "exit 0\n")
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

func TestCompileDenyPolicy(t *testing.T) {
	policy, err := denypolicy.Resolve(denypolicy.Catalogue{"checks": {Deny: []string{"Bash(make check:*)", "Bash(/usr/bin/git push --force:*)", "Agent"}}}, "checks", "", "worker")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileDenyPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	want := "prefix_rule(pattern=[\"make\", \"check\"], decision=\"forbidden\")\n" + "prefix_rule(pattern=[\"/usr/bin/git\", \"push\", \"--force\"], decision=\"forbidden\")\n"
	if compiled.Rules != want || !compiled.DisableDelegation {
		t.Fatalf("compiled = %#v, want rules %q and delegation disabled", compiled, want)
	}
	if empty, err := compileDenyPolicy(nil); empty != nil || err != nil {
		t.Fatalf("empty = %v, %v", empty, err)
	}
}

func TestCompileDenyPolicyRejectsWholeSelection(t *testing.T) {
	for _, entry := range []string{"Read(secret)", "Edit(secret)", "Agent(worker)", "Bash(make check)", "Bash(echo 'text':*)", "Bash(echo $HOME:*)", "Bash(echo *:*)", "Bash(echo a;b:*)", "Bash(echo a|b:*)", "Bash(VAR=value cmd:*)", "Bash(echo ~:*)", "Bash(echo `id`:*)", "Bash(echo [a]:*)", "Bash(echo {a,b}:*)", "Bash(echo a\\b:*)", "Bash( :*)", "Bash( echo:*)"} {
		t.Run(entry, func(t *testing.T) {
			policy, err := denypolicy.Resolve(denypolicy.Catalogue{"checks": {Deny: []string{"Bash(make check:*)", entry}}}, "", "checks", "worker")
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := compileDenyPolicy(policy)
			if compiled != nil || err == nil {
				t.Fatalf("compiled = %v, %v", compiled, err)
			}
			for _, part := range []string{"Codex", "checks", "entry 2", `node "worker" lf_deny`, "correct"} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("refusal %q lacks %q", err, part)
				}
			}
		})
	}
}

func TestValidateDenyVersion(t *testing.T) {
	for _, output := range []string{"codex-cli 0.160.0", "codex-cli 0.160.0\n"} {
		if err := validateDenyVersion(output); err != nil {
			t.Fatal(err)
		}
	}
	for _, output := range []string{"codex-cli 0.159.0", "codex-cli 0.161.0", "codex-cli 0.160.0-dev", "0.160.0", "codex-cli 0.160.0\nunexpected"} {
		if err := validateDenyVersion(output); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
}

// Selected launch fixtures supply graph delivery directly and do not locate
// repository instructions through runtime.Caller, which changes under trimpath.
func selectedTestRunner(t *testing.T, body string) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	binary, argv := filepath.Join(dir, "codex"), filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s ' \"$@\" > " + strconv.Quote(argv) + "\n" + body
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := New(Options{
		Paths:   config.NewPaths(filepath.Join(dir, "lfhome")),
		Codex:   config.Codex{Binary: binary, TimeoutSeconds: 30},
		Model:   "gpt-test",
		Environ: []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "USER=test"},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, argv
}
