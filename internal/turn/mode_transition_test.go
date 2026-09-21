package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
)

// Admission chooses the effective mode. Verify that downstream execution uses
// that immutable value even when the fetched issue still has another label.
func TestAdmittedModeReachesPromptAndReportedRun(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan, agent.ModeImplement, agent.ModeValidate, agent.ModeSpike} {
		t.Run(string(mode), func(t *testing.T) {
			var f *fixture
			runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) { return succeeded(req, f.paths.Home()), nil })
			f = newFixture(t, runner)
			// This fixture verifies the retained single-run transport across modes.
			if mode == agent.ModePlan {
				admitLegacyPlanFixture(t, f)
			}
			req := f.request()
			req.Mode = mode
			req.Issue.Labels = []string{"Mode: Research"}
			req.ToolProfile = string(mode)
			if mode == agent.ModeSpike {
				req.ToolProfile = "implement"
			}
			profile := config.DefaultToolProfiles().Permissions[req.ToolProfile]
			req.Tools = profile.Claude.Tools
			req.AllowedTools = profile.Claude.AllowedTools
			result := f.execute(req)
			if !result.Succeeded() {
				t.Fatalf("execution=%+v logs=%s", result, f.logs)
			}
			if len(runner.requests) != 1 {
				t.Fatalf("requests=%+v", runner.requests)
			}
			sent := runner.requests[0]
			if sent.Mode != string(mode) || !strings.Contains(sent.Prompt, "- Mode: "+string(mode)+"\n") {
				t.Fatalf("mode=%q prompt=%s", sent.Mode, sent.Prompt)
			}
			if sent.Permissions.Profile != req.ToolProfile {
				t.Fatalf("permission profile=%q", sent.Permissions.Profile)
			}
			if result.Run.Mode != string(mode) {
				t.Fatalf("report run mode=%q want=%q", result.Run.Mode, mode)
			}
		})
	}
}
