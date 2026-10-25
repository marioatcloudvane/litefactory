package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorkerTimeoutRawMigration(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		seconds    int
		warnings   int
		conflict   bool
	}{
		{"absent", "agents: {claude: {}}", 1800, 0, false},
		{"exact seconds", "agents: {claude: {timeout_seconds: 61}, codex: {}}", 61, 1, false},
		{"scalar alias", "settings: {poll_interval_minutes: &seconds 61}\nagents: {claude: {timeout_seconds: *seconds}}", 61, 1, false},
		{"mapping alias", "agents: {claude: &worker {timeout_seconds: 61}, codex: *worker}", 61, 2, false},
		{"merged legacy", "agents: {claude: &worker {timeout_seconds: 61}, codex: {<<: *worker}}", 61, 2, false},
		{"explicit alias wins", "settings: {poll_interval_minutes: &seconds 61, worker_timeout_seconds: *seconds}\nagents: {claude: {timeout_seconds: 120}}", 61, 1, false},
		{"equal legacy", "agents: {claude: {timeout_seconds: 121}, codex: {timeout_seconds: 121}}", 121, 2, false},
		{"different legacy", "agents: {claude: {timeout_seconds: 61}, codex: {timeout_seconds: 120}}", 0, 0, true},
		{"explicit wins", "settings: {worker_timeout_seconds: 3600}\nagents: {claude: {timeout_seconds: 61}, codex: {timeout_seconds: 120}}", 3600, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Decode(strings.NewReader(tc.body))
			if tc.conflict {
				if err == nil || !strings.Contains(err.Error(), "set settings.worker_timeout_seconds") {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Settings.WorkerTimeoutSeconds != tc.seconds || len(cfg.WorkerTimeoutDiagnostics()) != tc.warnings {
				t.Fatalf("resolved %d, diagnostics %v", cfg.Settings.WorkerTimeoutSeconds, cfg.WorkerTimeoutDiagnostics())
			}
		})
	}
}

func TestWorkerTimeoutBoundaries(t *testing.T) {
	for _, seconds := range []int{0, 59, 60, 86400, 86401} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			body := strings.Replace(string(DefaultFileContents()), "worker_timeout_seconds: 1800", fmt.Sprintf("worker_timeout_seconds: %d", seconds), 1)
			err := parse(t, body)
			if (err == nil) != (seconds >= 60 && seconds <= 86400) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for _, value := range []string{"null", "1.5", "'60'", "false"} {
		if _, err := Decode(strings.NewReader("settings: {worker_timeout_seconds: " + value + "}")); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}

func TestWorkerTimeoutReportsIndependentExplicitTypeFaults(t *testing.T) {
	_, err := Decode(strings.NewReader("settings: {worker_timeout_seconds: null}\nagents: {claude: {timeout_seconds: null}, codex: {timeout_seconds: null}}"))
	if err == nil {
		t.Fatal("invalid explicit timeout inputs accepted")
	}
	for _, key := range []string{"settings.worker_timeout_seconds", "agents.claude.timeout_seconds", "agents.codex.timeout_seconds"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("missing %s fault: %v", key, err)
		}
	}
}
