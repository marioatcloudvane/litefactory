package graphexec

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

const implementTestDOT = `digraph implement {
 graph [team="builtin", mode="implement", profile_version="1", start="select_path"];
 select_path [lf_action="lf_implement_select", lf_outcomes="coordinator,worker", lf_limit="4"];
 merge [lf_action="lf_implement_merge", lf_outcomes="merged,parked,none,retry", lf_limit="64"];
 ready [lf_action="lf_implement_ready", lf_outcomes="started,waiting,drained", lf_limit="64"];
 await [lf_action="lf_implement_await", lf_outcomes="go", lf_limit="64"];
 handoff [lf_action="lf_implement_handoff", lf_outcomes="posted", lf_limit="16"];
 any_parked [lf_action="lf_implement_any_parked", lf_outcomes="true,false", lf_limit="16"];
 ask_owner [lf_action="lf_implement_ask", lf_outcomes="published", lf_source="merge", lf_limit="16"];
 budget [lf_action="lf_implement_budget", lf_outcomes="run,park", lf_limit="8"];
 worker_agent [lf_action="lf_agent", lf_outcomes="success,failed", lf_agent_markdown="story-worker", lf_skills="implement-result", lf_limit="3"];
 worker_result [lf_action="lf_implement_result", lf_outcomes="completed,stopped", lf_source="worker_agent", lf_limit="3"];
 story_done [lf_action="lf_implement_story_done", lf_outcomes="done,retry", lf_limit="3"];
 park_story [lf_action="lf_implement_park", lf_outcomes="parked", lf_limit="4"];
 waiting [lf_terminal="true"];
 finished [lf_terminal="true"];
 worker_done [lf_terminal="true"];
 worker_parked [lf_terminal="true"];
 select_path -> merge [lf_on="coordinator"];
 select_path -> budget [lf_on="worker"];
 merge -> merge [lf_on="merged"];
 merge -> merge [lf_on="parked"];
 merge -> ready [lf_on="none"];
 merge -> await [lf_on="retry"];
 ready -> await [lf_on="started"];
 ready -> await [lf_on="waiting"];
 ready -> handoff [lf_on="drained"];
 await -> merge [lf_on="go"];
 handoff -> any_parked [lf_on="posted"];
 any_parked -> ask_owner [lf_on="true"];
 any_parked -> finished [lf_on="false"];
 ask_owner -> waiting [lf_on="published"];
 budget -> worker_agent [lf_on="run"];
 budget -> park_story [lf_on="park"];
 worker_agent -> worker_result [lf_on="success"];
 worker_agent -> budget [lf_on="failed"];
 worker_result -> story_done [lf_on="completed"];
 worker_result -> park_story [lf_on="stopped"];
 story_done -> worker_done [lf_on="done"];
 story_done -> budget [lf_on="retry"];
 park_story -> worker_parked [lf_on="parked"];
}
`

func implementTestRegistry(t *testing.T) Registry {
	t.Helper()
	var registry Registry
	for _, spec := range append(ImplementSpecs(), ActionSpec{Name: "lf_agent", Mode: agent.ModeImplement, Outcomes: []string{"success", "failed"}, Attributes: AgentAttributes()}) {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	registry.Policy = ValidateImplementGraph
	return registry
}

func validateImplementDOT(t *testing.T, dot string) error {
	t.Helper()
	profile, err := ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	return Validate(profile, implementTestRegistry(t))
}

func TestImplementGraphValidates(t *testing.T) {
	if err := validateImplementDOT(t, implementTestDOT); err != nil {
		t.Fatal(err)
	}
}

// implementReportDoneDOT is the graph that reports completion on the parent:
// any_parked false reaches report_done, which ends at finished.
func implementReportDoneDOT(t *testing.T) string {
	t.Helper()
	dot := strings.Replace(implementTestDOT, ` any_parked -> finished [lf_on="false"];`, ` any_parked -> report_done [lf_on="false"];
 report_done -> finished [lf_on="published"];`, 1)
	dot = strings.Replace(dot, ` budget [lf_action=`, ` report_done [lf_action="lf_implement_report_done", lf_outcomes="published", lf_source="merge", lf_limit="16"];
 budget [lf_action=`, 1)
	if dot == implementTestDOT {
		t.Fatal("the report_done graph changed nothing")
	}
	return dot
}

func TestImplementGraphValidatesWithAndWithoutTheCompletionReply(t *testing.T) {
	if err := validateImplementDOT(t, implementTestDOT); err != nil {
		t.Fatalf("a graph without report_done (released teams, running instances): %v", err)
	}
	dot := implementReportDoneDOT(t)
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatalf("a graph with report_done: %v", err)
	}
	for name, tc := range map[string]struct{ old, new, want string }{
		"the reply names no merge step": {`lf_outcomes="published", lf_source="merge", lf_limit="16"];
 budget`, `lf_outcomes="published", lf_source="ready", lf_limit="16"];
 budget`, "must name the merge step"},
		"the reply does not end": {` report_done -> finished [lf_on="published"];`, ` report_done -> merge [lf_on="published"];`, "must end at a terminal"},
	} {
		broken := strings.Replace(dot, tc.old, tc.new, 1)
		if broken == dot {
			t.Fatalf("%s: the mutation changed nothing", name)
		}
		if err := validateImplementDOT(t, broken); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

func TestImplementSpecsOfTheWorkerAndTheBatchedQuestion(t *testing.T) {
	want := map[string][]string{
		ImplementSelectAction:     {"coordinator", "worker"},
		ImplementBudgetAction:     {"run", "park"},
		ImplementResultAction:     {"completed", "incomplete", "blocked", "stopped"},
		ImplementStoryDoneAction:  {"done", "retry"},
		ImplementParkAction:       {"parked"},
		ImplementAwaitAction:      {"go"},
		ImplementAnyParkedAction:  {"true", "false"},
		ImplementAskAction:        {"published"},
		ImplementReportDoneAction: {"published"},
		ImplementResolvedAction:   {"resolved", "blocked", "retry"},
		SyncBaseAction:            {"synced", "current", "conflict", "retry", "deferred"},
	}
	got := map[string][]string{}
	for _, spec := range ImplementSpecs() {
		got[spec.Name] = spec.Outcomes
	}
	for name, outcomes := range want {
		if !slices.Equal(got[name], outcomes) {
			t.Errorf("%s outcomes = %v, want %v", name, got[name], outcomes)
		}
	}
	if len(got) != len(want)+3 {
		t.Errorf("specs = %d, want the story-002 three and %d more", len(got), len(want))
	}
}

func TestImplementGraphCheckerRefusesBrokenStructure(t *testing.T) {
	for name, tc := range map[string]struct{ old, new, want string }{
		"unbounded node": {`lf_outcomes="go", lf_limit="64"]`, `lf_outcomes="go"]`, "positive lf_limit"},
		"limit too big":  {`lf_outcomes="go", lf_limit="64"]`, `lf_outcomes="go", lf_limit="9999"]`, "positive lf_limit"},
		"wrong start":    {`start="select_path"`, `start="merge"`, "path selector"},
		"saved without the result decision": {
			` worker_result -> story_done [lf_on="completed"];`, ` worker_result -> park_story [lf_on="completed"];`, "must save the story",
		},
		"a failed attempt that ends the story quietly": {
			` worker_agent -> budget [lf_on="failed"];`, ` worker_agent -> worker_parked [lf_on="failed"];`, "budget decision",
		},
		"a failed save that skips the budget": {
			` story_done -> budget [lf_on="retry"];`, ` story_done -> worker_done [lf_on="retry"];`, "budget decision",
		},
		"a stopped answer that does not park": {
			` worker_result -> park_story [lf_on="stopped"];`, ` worker_result -> worker_done [lf_on="stopped"];`, "must park the story",
		},
		"the question does not end at a wait": {
			` ask_owner -> waiting [lf_on="published"];`, ` ask_owner -> merge [lf_on="published"];`, "terminal owner wait",
		},
		"the question names no merge step": {`lf_source="merge", lf_limit="16"`, `lf_source="ready", lf_limit="16"`, "owner reply resumes at"},
		"the await node is renamed":        {` await [lf_action="lf_implement_await"`, ` pause [lf_action="lf_implement_await"`, "undeclared"},
	} {
		dot := strings.Replace(implementTestDOT, tc.old, tc.new, 1)
		if dot == implementTestDOT {
			t.Fatalf("%s: the mutation changed nothing", name)
		}
		if name == "the await node is renamed" {
			dot = strings.ReplaceAll(dot, "-> await", "-> pause")
			dot = strings.ReplaceAll(dot, "await ->", "pause ->")
			if err := validateImplementDOT(t, dot); err == nil || !strings.Contains(err.Error(), "await node to run") {
				t.Errorf("%s: err = %v", name, err)
			}
			continue
		}
		if err := validateImplementDOT(t, dot); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

func TestImplementGraphCheckerRequiresExactlyOneWorker(t *testing.T) {
	dot := strings.Replace(implementTestDOT, ` park_story [lf_action="lf_implement_park"`, ` second [lf_action="lf_agent", lf_outcomes="success,failed", lf_agent_markdown="story-worker", lf_skills="implement-result", lf_limit="3"];
 second -> worker_result [lf_on="success"];
 second -> budget [lf_on="failed"];
 park_story [lf_action="lf_implement_park"`, 1)
	if err := validateImplementDOT(t, dot); err == nil || !strings.Contains(err.Error(), "exactly one worker agent") {
		t.Fatalf("%v", err)
	}
}

func TestImplementGraphCheckerRefusesForeignActions(t *testing.T) {
	var registry Registry
	for _, spec := range append(ImplementSpecs(), ActionSpec{Name: "lf_agent", Mode: agent.ModeImplement, Outcomes: []string{"success", "failed"}, Attributes: AgentAttributes()}, ControlSpec(ResearchIfAction, ImplementControlPolicy())) {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	dot := strings.Replace(implementTestDOT, `any_parked [lf_action="lf_implement_any_parked", lf_outcomes="true,false", lf_limit="16"]`, `any_parked [lf_action="lf_if", lf_field="response", lf_condition="completed", lf_outcomes="true,false", lf_limit="16"]`, 1)
	profile, err := ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateImplementGraph(profile); err == nil || !strings.Contains(err.Error(), "does not implement") {
		t.Fatalf("%v", err)
	}
	if err := ValidateImplementGraph(nil); err == nil {
		t.Fatal("nil profile")
	}
	profile.Mode = agent.ModePlan
	if err := ValidateImplementGraph(profile); err == nil {
		t.Fatal("another mode")
	}
}

func TestImplementControlPolicyIsTheWorkersOutcomes(t *testing.T) {
	policy := ImplementControlPolicy()
	if policy.Mode != agent.ModeImplement || !slices.Equal(policy.Responses, []string{"completed", "incomplete", "blocked"}) {
		t.Fatalf("%+v", policy)
	}
}

// implementRetryDOT is the current graph shape: an incomplete answer goes back to
// the budget for another attempt, a blocked answer parks at once.
func implementRetryDOT(t *testing.T) string {
	t.Helper()
	dot := strings.Replace(implementTestDOT, `lf_outcomes="completed,stopped"`, `lf_outcomes="completed,incomplete,blocked"`, 1)
	dot = strings.Replace(dot, ` worker_result -> park_story [lf_on="stopped"];`, ` worker_result -> budget [lf_on="incomplete"];
 worker_result -> park_story [lf_on="blocked"];`, 1)
	if dot == implementTestDOT {
		t.Fatal("the retry graph changed nothing")
	}
	return dot
}

func TestImplementResultCheckerAcceptsBothShapesAndRefusesWrongRouting(t *testing.T) {
	if err := validateImplementDOT(t, implementTestDOT); err != nil {
		t.Fatalf("completed,stopped (released graphs): %v", err)
	}
	dot := implementRetryDOT(t)
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatalf("completed,incomplete,blocked: %v", err)
	}
	for name, tc := range map[string]struct{ old, new, want string }{
		"incomplete parks": {
			` worker_result -> budget [lf_on="incomplete"];`, ` worker_result -> park_story [lf_on="incomplete"];`, "outcome incomplete must reach the budget decision",
		},
		"blocked retries": {
			` worker_result -> park_story [lf_on="blocked"];`, ` worker_result -> budget [lf_on="blocked"];`, "outcome blocked must park the story",
		},
		"incomplete without blocked": {
			`lf_outcomes="completed,incomplete,blocked"`, `lf_outcomes="completed,incomplete"`, "no single edge for outcome blocked",
		},
		"blocked without incomplete": {
			`lf_outcomes="completed,incomplete,blocked"`, `lf_outcomes="completed,blocked"`, "no single edge for outcome incomplete",
		},
		"neither shape": {
			`lf_outcomes="completed,incomplete,blocked"`, `lf_outcomes="completed"`, "must declare stopped, or incomplete and blocked",
		},
		"both shapes": {
			`lf_outcomes="completed,incomplete,blocked"`, `lf_outcomes="completed,incomplete,blocked,stopped"`, "declares stopped together with",
		},
	} {
		broken := strings.Replace(dot, tc.old, tc.new, 1)
		if broken == dot {
			t.Fatalf("%s: the mutation changed nothing", name)
		}
		if err := validateImplementDOT(t, broken); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

// stoppedImplementShape rewrites a released implement graph to the earlier
// result shape (completed,stopped, with stopped parking the story) that running
// instances captured from older releases still use.
func stoppedImplementShape(t *testing.T, graph string) string {
	t.Helper()
	out := strings.Replace(graph, `lf_outcomes="completed,incomplete,blocked"`, `lf_outcomes="completed,stopped"`, 1)
	out = regexp.MustCompile(`(?m)^[ \t]*worker_result[ \t]*->[ \t]*budget[ \t]*\[lf_on="incomplete"\];\n`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`(worker_result[ \t]*->[ \t]*park_story[ \t]*)\[lf_on="blocked"\]`).ReplaceAllString(out, `${1}[lf_on="stopped"]`)
	if !strings.Contains(out, `lf_outcomes="completed,stopped"`) || !strings.Contains(out, `[lf_on="stopped"]`) || strings.Contains(out, `[lf_on="incomplete"]`) {
		t.Fatal("the released implement graph has no derivable stopped shape")
	}
	return out
}

// TestReleasedImplementGraphWithStoppedStillValidates keeps every implement graph
// the catalogue ships valid, in its own result shape and in the earlier stopped
// shape that running instances pinned to older releases still use. It reads
// whatever releases teams/teams.json lists.
func TestReleasedImplementGraphWithStoppedStillValidates(t *testing.T) {
	raw, err := os.ReadFile("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalogue struct {
		Releases []struct {
			Download string `json:"download"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(raw, &catalogue); err != nil || len(catalogue.Releases) == 0 {
		t.Fatalf("catalogue: %v", err)
	}
	for _, r := range catalogue.Releases {
		if !fs.ValidPath(r.Download) {
			t.Fatalf("unsafe catalogue download %q", r.Download)
		}
		graph, err := os.ReadFile(filepath.Join("../../teams", filepath.FromSlash(r.Download), "implement/implement.dot"))
		if err != nil {
			t.Fatal(err)
		}
		if err := validateImplementDOT(t, string(graph)); err != nil {
			t.Errorf("%s: %v", r.Download, err)
		}
		shape := string(graph)
		if !strings.Contains(shape, `lf_outcomes="completed,stopped"`) {
			shape = stoppedImplementShape(t, shape)
		}
		if err := validateImplementDOT(t, shape); err != nil {
			t.Errorf("%s stopped shape: %v", r.Download, err)
		}
	}
}

// implementSyncDOT adds the base sync at the start of the coordinator path, with
// its resolver pair, to the test graph.
func implementSyncDOT(t *testing.T) string {
	t.Helper()
	dot := strings.Replace(implementTestDOT, ` select_path -> merge [lf_on="coordinator"];`, ` select_path -> sync_start [lf_on="coordinator"];
 sync_start -> merge [lf_on="synced"];
 sync_start -> merge [lf_on="current"];
 sync_start -> sync_start_resolve [lf_on="conflict"];
 sync_start -> await [lf_on="retry"];
 sync_start_resolve -> sync_start_resolved [lf_on="success"];
 sync_start_resolve -> sync_start [lf_on="failed"];
 sync_start_resolved -> sync_start [lf_on="resolved"];
 sync_start_resolved -> sync_start [lf_on="retry"];
 sync_start_resolved -> waiting [lf_on="blocked"];`, 1)
	dot = strings.Replace(dot, ` budget [lf_action=`, ` sync_start [lf_action="lf_sync_base", lf_outcomes="synced,current,conflict,retry", lf_limit="8"];
 sync_start_resolve [lf_action="lf_agent", lf_outcomes="success,failed", lf_agent_markdown="sync-resolver", lf_skills="implement-result", lf_limit="3"];
 sync_start_resolved [lf_action="lf_implement_resolved", lf_outcomes="resolved,blocked,retry", lf_source="sync_start_resolve", lf_limit="3"];
 budget [lf_action=`, 1)
	if dot == implementTestDOT {
		t.Fatal("the sync graph changed nothing")
	}
	return dot
}

func TestImplementGraphValidatesWithTheBaseSyncAndRefusesBrokenSyncs(t *testing.T) {
	dot := implementSyncDOT(t)
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatalf("a graph with the base sync and its resolver: %v", err)
	}
	for name, tc := range map[string]struct{ old, new, want string }{
		"a conflict that is skipped": {
			` sync_start -> sync_start_resolve [lf_on="conflict"];`, ` sync_start -> merge [lf_on="conflict"];`, "must hand the conflict to a resolver agent",
		},
		"a failed resolver that ends quietly": {
			` sync_start_resolve -> sync_start [lf_on="failed"];`, ` sync_start_resolve -> finished [lf_on="failed"];`, "must return to a base sync",
		},
		"a resolution that skips the sync": {
			` sync_start_resolved -> sync_start [lf_on="resolved"];`, ` sync_start_resolved -> merge [lf_on="resolved"];`, "outcome resolved must return to a base sync",
		},
		"a retry that skips the sync": {
			` sync_start_resolved -> sync_start [lf_on="retry"];`, ` sync_start_resolved -> merge [lf_on="retry"];`, "outcome retry must return to a base sync",
		},
		"a blocked resolver that does not wait": {
			` sync_start_resolved -> waiting [lf_on="blocked"];`, ` sync_start_resolved -> merge [lf_on="blocked"];`, "terminal owner wait",
		},
		"the decision names another agent": {
			`lf_source="sync_start_resolve"`, `lf_source="worker_agent"`, "must name the resolver agent",
		},
		"an unbounded sync": {
			`lf_outcomes="synced,current,conflict,retry", lf_limit="8"]`, `lf_outcomes="synced,current,conflict,retry"]`, "positive lf_limit",
		},
	} {
		broken := strings.Replace(dot, tc.old, tc.new, 1)
		if broken == dot {
			t.Fatalf("%s: the mutation changed nothing", name)
		}
		if err := validateImplementDOT(t, broken); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

// TestImplementGraphAcceptsAnOwnerWaitAtTheBaseSync keeps the owner reply
// resuming at the coordinator base sync valid, so main reaches the feature
// branch before resumed stories start, and refuses a sync that cannot reach the
// merge step.
func TestImplementGraphAcceptsAnOwnerWaitAtTheBaseSync(t *testing.T) {
	dot := strings.Replace(implementSyncDOT(t), `lf_implement_ask", lf_outcomes="published", lf_source="merge"`, `lf_implement_ask", lf_outcomes="published", lf_source="sync_start"`, 1)
	if !strings.Contains(dot, `lf_source="sync_start"`) {
		t.Fatal("the owner wait was not moved to the base sync")
	}
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatalf("an owner wait at the base sync: %v", err)
	}
	for name, tc := range map[string]struct{ old, new string }{
		"a sync whose success skips the merge step": {` sync_start -> merge [lf_on="synced"];`, ` sync_start -> ready [lf_on="synced"];`},
		"a sync whose success paths differ":         {` sync_start -> merge [lf_on="current"];`, ` sync_start -> await [lf_on="current"];`},
	} {
		broken := strings.Replace(dot, tc.old, tc.new, 1)
		if broken == dot {
			t.Fatalf("%s: the mutation changed nothing", name)
		}
		if err := validateImplementDOT(t, broken); err == nil || !strings.Contains(err.Error(), "owner reply resumes at") {
			t.Errorf("%s: err = %v, want the owner reply source refused", name, err)
		}
	}
}

// TestSyncBaseIsOneGenericStep keeps lf_sync_base a generic step any graph can
// declare under its own mode, with the same outcomes.
func TestSyncBaseIsOneGenericStep(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeImplement, agent.ModeValidate, agent.ModeSpike, agent.ModeResearch} {
		var registry Registry
		if err := registry.Register(SyncBaseSpec(mode)); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		spec, ok := registry.Resolve(SyncBaseAction)
		if !ok || spec.Mode != mode || !slices.Equal(spec.Outcomes, []string{"synced", "current", "conflict", "retry", "deferred"}) {
			t.Fatalf("%s: %+v %v", mode, spec, ok)
		}
	}
	var registry Registry
	bug := SyncBaseSpec("")
	bug.Workflow = agent.WorkflowBug
	if err := registry.Register(bug); err != nil {
		t.Fatalf("the Bug workflow: %v", err)
	}
}

// TestEveryShippedImplementGraphValidates checks the editable team sources, which
// declare the base sync, and every released graph, which does not, against the
// same checker.
func TestEveryShippedImplementGraphValidates(t *testing.T) {
	paths := []string{
		"../teamsource/implement/implement.dot",
		"../teamsource/python-fastapi/implement/implement.dot",
		"../teamsource/swift-ios/implement/implement.dot",
		"../../teams/source/dotnet-web/implement/implement.dot",
	}
	released, err := filepath.Glob("../../teams/*/v*/implement/implement.dot")
	if err != nil || len(released) == 0 {
		t.Fatalf("released graphs: %v %v", released, err)
	}
	for i, path := range append(paths, released...) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if i < len(paths) && !strings.Contains(string(raw), `lf_action="lf_sync_base"`) {
			t.Errorf("%s: the source graph declares no base sync", path)
		}
		if err := validateImplementDOT(t, string(raw)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestImplementWorkerSyncAdmission(t *testing.T) {
	dot := strings.Replace(implementSyncDOT(t), ` budget -> worker_agent [lf_on="run"];`, ` sync_before_worker [lf_action="lf_sync_base", lf_outcomes="synced,current,conflict,retry", lf_limit="8"];
 budget -> sync_before_worker [lf_on="run"];
 sync_before_worker -> worker_agent [lf_on="synced"];
 sync_before_worker -> worker_agent [lf_on="current"];
 sync_before_worker -> sync_before_worker [lf_on="retry"];
 sync_before_worker -> park_story [lf_on="conflict"];`, 1)
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string][2]string{
		"budget bypass":       {` budget -> sync_before_worker [lf_on="run"];`, ` budget -> worker_agent [lf_on="run"];`},
		"selector bypass":     {` select_path -> budget [lf_on="worker"];`, ` select_path -> worker_agent [lf_on="worker"];`},
		"gate budget bypass":  {` select_path -> budget [lf_on="worker"];`, ` select_path -> sync_before_worker [lf_on="worker"];`},
		"different successes": {` sync_before_worker -> worker_agent [lf_on="current"];`, ` sync_before_worker -> worker_done [lf_on="current"];`},
		"unsafe retry":        {` sync_before_worker -> sync_before_worker [lf_on="retry"];`, ` sync_before_worker -> worker_agent [lf_on="retry"];`},
		"resolver conflict":   {` sync_before_worker -> park_story [lf_on="conflict"];`, ` sync_before_worker -> sync_start_resolve [lf_on="conflict"];`},
		"changed bound":       {`sync_before_worker [lf_action="lf_sync_base", lf_outcomes="synced,current,conflict,retry", lf_limit="8"]`, `sync_before_worker [lf_action="lf_sync_base", lf_outcomes="synced,current,conflict,retry", lf_limit="9"]`},
	} {
		t.Run(name, func(t *testing.T) {
			broken := strings.Replace(dot, mutation[0], mutation[1], 1)
			if broken == dot {
				t.Fatal("mutation changed nothing")
			}
			if err := validateImplementDOT(t, broken); err == nil {
				t.Fatal("unsafe worker gate admitted")
			}
		})
	}
	profile, err := ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	if gate, ok := ImplementWorkerSyncNode(profile); !ok || gate.Name != "sync_before_worker" {
		t.Fatalf("gate = %+v, %v", gate, ok)
	}
	legacy, err := ParseProfile([]byte(implementTestDOT))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ImplementWorkerSyncNode(legacy); ok {
		t.Fatal("legacy graph treated as gated")
	}
}
