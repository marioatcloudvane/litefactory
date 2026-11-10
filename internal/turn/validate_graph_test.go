package turn

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// checkValidateDot parses a mutated copy of the packaged graph and checks it
// against the Validate registry.
func checkValidateDot(t *testing.T, mutate func(string) string) error {
	t.Helper()
	dot := string(captureValidate(t).Files["validate.dot"])
	mutated := mutate(dot)
	if mutated == dot {
		t.Fatal("the mutation changed nothing")
	}
	profile, err := graphexec.ParseProfile([]byte(mutated))
	if err != nil {
		return err
	}
	specs, err := ValidateFlowSpecs()
	if err != nil {
		t.Fatal(err)
	}
	return graphexec.Validate(profile, specs)
}

func TestAgentBudgetAttributeIsValidatedAndBounded(t *testing.T) {
	for value, ok := range map[string]bool{"1": true, "15": true, "60": true, "0": false, "1440": true, "1441": false, "-3": false, "15m": false, "": false} {
		_, present, err := graphexec.AgentBudget(map[string]string{graphexec.BudgetAttribute: value})
		if ok && (err != nil || !present) || !ok && err == nil {
			t.Errorf("budget %q: present=%v err=%v", value, present, err)
		}
	}
	if m, present, err := graphexec.AgentBudget(nil); err != nil || present || m != 0 {
		t.Errorf("no budget: %v %v %v", m, present, err)
	}
	if m, _, _ := graphexec.AgentBudget(map[string]string{graphexec.BudgetAttribute: "15"}); m != 15 {
		t.Errorf("15 minutes is %v", m)
	}
	// The generic agent action admits the attribute for every workflow.
	if _, ok := graphexec.AgentAttributes()[graphexec.BudgetAttribute]; !ok {
		t.Error("the agent action does not declare lf_budget")
	}
}

// #617: the repair outcome of the resolve step is optional, so a graph without
// it, such as the released team packages, still validates.
func TestValidateGraphWithoutTheRepairOutcomeStillValidates(t *testing.T) {
	err := checkValidateDot(t, func(s string) string {
		s = strings.Replace(s, `,stale,repair`, `,stale`, 1)
		return strings.Replace(s, ` owner_resolve -> repair_budget      [lf_on="repair"];`+"\n", "", 1)
	})
	if err != nil {
		t.Fatalf("a graph without the repair outcome is refused: %v", err)
	}
	specs, err := ValidateFlowSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range sourceTeams(t) {
		source, err := teamsourcetest.Workflow(t.Context(), team, "validate")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := fs.ReadFile(source, "validate.dot")
		if err != nil {
			t.Fatal(err)
		}
		profile, err := graphexec.ParseProfile(raw)
		if err != nil {
			t.Fatalf("%s: %v", team, err)
		}
		if err := graphexec.Validate(profile, specs); err != nil {
			t.Errorf("team %s no longer validates: %v", team, err)
		}
	}
}

// sourceTeams lists every installable team folder under teams/source, so
// team-graph tests follow what `litefactory team install` can install.
func sourceTeams(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(teamsourcetest.RepoRoot(), "teams", "source"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(teamsourcetest.RepoRoot(), "teams", "source", entry.Name(), "release.json")); err == nil {
			out = append(out, entry.Name())
		}
	}
	if len(out) == 0 {
		t.Fatal("teams/source holds no team")
	}
	slices.Sort(out)
	return out
}
