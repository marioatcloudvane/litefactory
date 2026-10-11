package bugchecks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

type pythonRecipeRecorder struct {
	observed, runs []config.BugCheckCommand
	missing        string
	outcome        string
}

func (r *pythonRecipeRecorder) ObserveInputs(_ string, c config.BugCheckCommand) (string, error) {
	r.observed = append(r.observed, c)
	if c.Argv[0] == r.missing {
		return "", errors.New("required executable unavailable")
	}
	return "recorded", nil
}
func (r *pythonRecipeRecorder) Run(_ context.Context, _ string, c config.BugCheckCommand, _ func(CommandResult) error) CommandResult {
	r.runs = append(r.runs, c)
	return CommandResult{Outcome: r.outcome, Output: "recorded recipe execution", CaptureComplete: true, CleanupConfirmed: true}
}

func TestReleasedPythonFastAPIRecipesUseSharedContainedChecks(t *testing.T) {
	raw := releasedPythonProfile(t)
	p, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"setup", "checks"} {
		t.Run(phase, func(t *testing.T) {
			commands := p.Effective.Setup
			if phase == "checks" {
				commands = p.Effective.Checks
			}
			recorder := &pythonRecipeRecorder{outcome: "passed"}
			var retained []CommandResult
			results, err := runStackCommands(t.Context(), t.TempDir(), &p, phase, func(r CommandResult) error { retained = append(retained, r); return nil }, recorder)
			if err != nil || len(results) != len(commands) || len(retained) != len(commands) {
				t.Fatal("shared recipe execution lost results", err)
			}
			for i, c := range commands {
				want := c.BugCheckCommand
				want.Env = []string{"CI", "PYTHONDONTWRITEBYTECODE"}
				if !reflect.DeepEqual(want, recorder.observed[i]) || !reflect.DeepEqual(want, recorder.runs[i]) {
					t.Fatal("recipe or confined cwd changed", c.Name)
				}
			}
			for _, outcome := range []string{"failed", "cancelled"} {
				r := &pythonRecipeRecorder{outcome: outcome}
				results, err := runStackCommands(t.Context(), t.TempDir(), &p, phase, func(CommandResult) error { return nil }, r)
				if err == nil || len(results) != 1 || results[0].Outcome != outcome || len(r.runs) != 1 {
					t.Fatal("failure suppressed", outcome, results, err)
				}
			}
			for _, missing := range p.Effective.Requirements.Executables {
				r := &pythonRecipeRecorder{missing: missing, outcome: "passed"}
				results, err := runStackCommands(t.Context(), t.TempDir(), &p, phase, func(CommandResult) error { return nil }, r)
				if err == nil || !strings.Contains(err.Error(), "inputs unavailable") || len(results) != 0 || len(r.runs) != 0 {
					t.Fatal("missing executable admitted", missing, err)
				}
			}
		})
	}
	isolation, _, _ := stackCommandFixture(t, "exit 0\n")
	isolation.Network = false
	isolation.WritableRoots = []string{"/admitted/cache"}
	isolation.DenyRead = []string{"/private/credential"}
	effective, err := isolation.WithStackProfile(&p)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Network || !reflect.DeepEqual(effective.WritableRoots, isolation.WritableRoots) || !reflect.DeepEqual(effective.DenyRead, isolation.DenyRead) || effective.Binary != isolation.Binary || effective.PATH != isolation.PATH {
		t.Fatal("release widened operator authority")
	}
}

// releasedPythonProfile reads the stack profile of the python-fastapi release the
// canonical source currently names, so the test follows the owner's releases.
func releasedPythonProfile(t *testing.T) []byte {
	t.Helper()
	meta, err := os.ReadFile("../teamsource/python-fastapi/release.json")
	if err != nil {
		t.Fatal(err)
	}
	var release struct {
		Download string `json:"download"`
	}
	if err := json.Unmarshal(meta, &release); err != nil || !strings.HasPrefix(release.Download, "python-fastapi/") || strings.Contains(release.Download, "..") {
		t.Fatalf("python-fastapi release metadata: %q %v", release.Download, err)
	}
	raw, err := os.ReadFile("../../teams/" + release.Download + "/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
