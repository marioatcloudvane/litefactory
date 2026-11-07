package turn

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
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
	for _, release := range catalogueDownloads(t) {
		raw, err := os.ReadFile(filepath.Join("..", "..", "teams", release, "validate", "validate.dot"))
		if err != nil {
			t.Fatal(err)
		}
		profile, err := graphexec.ParseProfile(raw)
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if err := graphexec.Validate(profile, specs); err != nil {
			t.Errorf("released %s no longer validates: %v", release, err)
		}
	}
}

// catalogueDownloads lists the download path of every release teams/teams.json
// ships, so released-graph tests follow the owner's releases.
func catalogueDownloads(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "teams", "teams.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue struct {
		Releases []struct {
			Slug     string `json:"slug"`
			Download string `json:"download"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(raw, &catalogue); err != nil || len(catalogue.Releases) == 0 {
		t.Fatalf("catalogue: %v", err)
	}
	var out []string
	for _, r := range catalogue.Releases {
		if !fs.ValidPath(r.Download) || !strings.HasPrefix(r.Download, r.Slug+"/") {
			t.Fatalf("unsafe catalogue download %q", r.Download)
		}
		out = append(out, r.Download)
	}
	slices.Sort(out)
	return out
}
