package turn

import (
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

// AC-001: the declared structure is enforced: bound, access, budget, limits, ends.
func TestValidateGraphRefusesAWeakenedFlow(t *testing.T) {
	cases := map[string]struct {
		mutate func(string) string
		want   string
	}{
		"three repair rounds": {func(s string) string { return strings.Replace(s, `lf_rounds="2"`, `lf_rounds="3"`, 1) }, "lf_rounds"},
		"an agent bound of three": {func(s string) string {
			return strings.Replace(s, `lf_budget="15", lf_limit="2"`, `lf_budget="15", lf_limit="3"`, 1)
		}, "exceeds 2 repair rounds"},
		"no time budget":           {func(s string) string { return strings.Replace(s, ` lf_budget="15",`, ``, 1) }, "lf_budget"},
		"a budget over the bound":  {func(s string) string { return strings.Replace(s, `lf_budget="15"`, `lf_budget="61"`, 1) }, "lf_budget"},
		"a read-only repair agent": {func(s string) string { return strings.Replace(s, `lf_access="write"`, `lf_access="read"`, 1) }, "lf_access=write"},
		"a kept repair that skips the suite": {func(s string) string {
			return strings.Replace(s, `repair_check  -> suite         [lf_on="kept"]`, `repair_check  -> review        [lf_on="kept"]`, 1)
		}, "must run the suite on the new commit"},
		"a failed suite that skips the repair budget": {func(s string) string {
			return strings.Replace(s, `suite -> repair_budget [lf_on="failed"]`, `suite -> review        [lf_on="failed"]`, 1)
		}, "must reach the repair budget"},
		"a passed suite that skips the review": {func(s string) string {
			return strings.Replace(s, `suite -> review        [lf_on="passed"]`, `suite -> outcome       [lf_on="passed"]`, 1)
		}, "must go to review"},
		"a handback that skips the review": {func(s string) string {
			return strings.Replace(s, `repair_check  -> review        [lf_on="handback"]`, `repair_check  -> outcome       [lf_on="handback"]`, 1)
		}, "must go to review"},
		"an end that is not its terminal": {func(s string) string {
			return strings.Replace(s, `outcome -> owner_resolve [lf_on="handed_back"]`, `outcome -> owner_report [lf_on="handed_back"]`, 1)
		}, "must reach the owner resolution"},
		"a push that skips its step": {func(s string) string {
			return strings.Replace(s, `owner_resolve -> owner_push         [lf_on="push"]`, `owner_resolve -> owner_report       [lf_on="push"]`, 1)
		}, "must push"},
		"a close that ends without closing": {func(s string) string {
			return strings.Replace(s, `owner_close -> owner_report       [lf_on="open"]`, `owner_close -> delivered    [lf_on="open"]`, 1)
		}, "must report"},
		"an unclosed run that succeeds": {func(s string) string {
			return strings.Replace(s, `owner_merge -> owner_report       [lf_on="done"]`, `owner_merge -> delivered    [lf_on="done"]`, 1)
		}, "must report"},
		"a second agent": {func(s string) string {
			return strings.Replace(s, ` // ---- Review the final`, ` extra [lf_action="lf_agent", lf_outcomes="success,failed", lf_agent_markdown="validate-code", lf_limit="1"];`+"\n // ---- Review the final", 1)
		}, ""},
		"a limit of zero": {func(s string) string { return strings.Replace(s, `lf_limit="32"`, `lf_limit="0"`, 1) }, "lf_limit"},
		"too many files":  {func(s string) string { return strings.Replace(s, `lf_max_files="3"`, `lf_max_files="9"`, 1) }, "lf_max_files"},
		"an unknown forbidden kind": {func(s string) string {
			return strings.Replace(s, `package,dependency,schema,api,tests,criteria`, `package,dependency,schema,api,tests,magic`, 1)
		}, "lf_forbid"},
		"a wrong reviewer set": {func(s string) string {
			return strings.Replace(s, `validate-acceptance,validate-code,validate-architecture`, `validate-code,validate-acceptance,validate-architecture`, 1)
		}, "lf_roles"},
	}
	for name, c := range cases {
		err := checkValidateDot(t, c.mutate)
		if err == nil {
			t.Errorf("%s: the weakened graph was accepted", name)
			continue
		}
		if c.want != "" && !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", name, err, c.want)
		}
	}
}

func TestValidateGraphMustStartAtTheSuite(t *testing.T) {
	err := checkValidateDot(t, func(s string) string { return strings.Replace(s, `start="suite"`, `start="review"`, 1) })
	if err == nil || !strings.Contains(err.Error(), "must start at the suite step") {
		t.Fatalf("%v", err)
	}
}

func TestAgentBudgetAttributeIsValidatedAndBounded(t *testing.T) {
	for value, ok := range map[string]bool{"1": true, "15": true, "60": true, "0": false, "61": false, "-3": false, "15m": false, "": false} {
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
