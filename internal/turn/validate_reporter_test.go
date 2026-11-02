package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// flowReporter is the flow fixture's reporter: f.reporterRun answers it.
type flowReporter struct{ f *flowFixture }

func (r flowReporter) RunReporter(_ context.Context, req ValidateReporterRequest) (*agentresult.Result, error) {
	r.f.reporterReqs = append(r.f.reporterReqs, req)
	if r.f.reporterRun == nil {
		return nil, errors.New("no reporter in this fixture")
	}
	return r.f.reporterRun(req)
}

func (r flowReporter) Recover(context.Context, string) (*agentresult.Result, bool, error) {
	return nil, false, nil
}

// redState is a red run with one failure, a check, an optional and a blocking
// finding and a pending repair candidate, as the #1268 report had.
func redState() ValidateState {
	st := twoStories(
		ValidateFinding{ID: "b:plan", Story: "b", Name: "plan", Role: agent.ReviewRoleAcceptance, Optional: true, State: findingOpen, Summary: "The staged verification is not shown."},
		ValidateFinding{ID: "b:code", Story: "b", Name: "code", Role: agent.ReviewRoleCode, Blocking: true, State: findingOpen, Summary: "The resolver never records the owner wait."},
	)
	st.Suite.Outcome, st.Suite.RunID, st.Suite.Reason = store.SuiteFailed, 95, "project check reported test failure"
	st.Failures = []HandbackFailure{{Command: "make test", Reason: "exited 2", OutputTail: "--- FAIL: TestImplementGraphRun (0.2s)\nFAIL\n3 failed, 269 passed in 427.3s", Cause: "No owner wait is recorded after sync_start:conflict.", Objective: "Trace the first resolver dispatch after the conflict.", SuiteRecord: "suite run 95, command 1"}}
	st.Candidates = []store.RepairCandidate{{ID: strings.Repeat("1", 64), ReviewDigest: strings.Repeat("2", 64), Resolution: "pending"}}
	st.Reimplement, st.FactoryBugFiling, st.DecisionTask, st.DeliveryCommit = true, true, "github:acme/api#497", strings.Repeat("c", 40)
	st.computeReadiness()
	return st
}

// #1314: every action renders an exact command that parses, with the current
// revision or the displayed decision reference.
func TestReporterActionsRenderValidCommands(t *testing.T) {
	st := redState()
	items := st.reportItems()
	if len(items) < 5 {
		t.Fatalf("items %+v", items)
	}
	check := func(command string) {
		t.Helper()
		switch {
		case strings.HasPrefix(command, "/validate-"), strings.HasPrefix(command, ValidateReturnCommand):
			c, err := ParseValidateCommand(command)
			if err != nil {
				t.Fatalf("%q does not parse: %v", command, err)
			}
			if c.Revision != st.Revision && c.Reference != st.shipReference() {
				t.Fatalf("%q names neither the revision nor the decision reference", command)
			}
		case command == ValidateStartCommand:
			if c := ParseCommand(command); c.Name != "/validate" {
				t.Fatalf("%q is not the Validate start", command)
			}
		case command == ValidateBringMainCommand:
			if c := ParseCommand(command); c.Name != "/implement" || c.Kind != KindSpike {
				t.Fatalf("%q is not an Implement instruction", command)
			}
		default:
			t.Fatalf("unexpected command %q", command)
		}
	}
	seen := map[string]bool{}
	for i := range items {
		for _, action := range items[i].Actions {
			command, ok := st.reportCommand(&items[i], action)
			if !ok {
				t.Fatalf("%s %s admitted but renders nothing", items[i].Ref, action)
			}
			check(command)
			seen[action] = true
		}
	}
	for _, action := range st.reportGlobalActions() {
		command, ok := st.reportCommand(nil, action)
		if !ok {
			t.Fatalf("global %s renders nothing", action)
		}
		check(command)
		seen[action] = true
	}
	for _, action := range agentresult.ReportActions {
		if !seen[action] {
			t.Errorf("action %s never rendered for this state", action)
		}
	}
	// An action an item does not admit renders nothing.
	if _, ok := st.reportCommand(&items[0], agentresult.ReportActionCheckPass); ok {
		t.Fatal("a failure rendered a check")
	}
	ship, _ := st.reportCommand(nil, agentresult.ReportActionShip)
	if ship != ValidateShipCommand+" "+st.shipReference()+" -- <reason>" || !strings.HasPrefix(st.shipReference(), "d-") {
		t.Fatalf("ship %q", ship)
	}
	factoryBug, _ := st.reportCommand(&items[0], agentresult.ReportActionFactoryBug)
	if c, err := ParseValidateCommand(factoryBug); err != nil || c.Item != "failure-1" || c.Name != ValidateFactoryBugCommand {
		t.Fatalf("factory Bug %q: %+v %v", factoryBug, c, err)
	}
}

// #1314: every command the old report offered still appears with the shown
// revision, and the details keep every recorded fact.
func TestReportKeepsEveryCommandAndFact(t *testing.T) {
	st := redState()
	st.Suite.Outcome, st.Failures = store.SuitePassed, nil
	st.Suite.AssessmentID, st.Suite.AssessmentDigest = "", ""
	st.Waiver = nil
	st.computeReadiness()
	out := renderValidateReport(st, twoTargets, "")
	for _, line := range st.NextCommands(twoTargets.CloseSet()) {
		for _, piece := range strings.Split(line, "`") {
			if strings.HasPrefix(piece, "/validate-") && !strings.Contains(out, "`"+piece+"`") {
				t.Errorf("command %q is no longer offered:\n%s", piece, out)
			}
		}
	}
	for _, fact := range []string{st.Commit, st.Candidates[0].ID, st.Candidates[0].ReviewDigest, "| " + lightRed + " b Repair tools (#12)", "Optional finding `b:plan` needs a choice.", "Delivery: nothing is pushed", "The resolver never records the owner wait."} {
		if !strings.Contains(out, fact) {
			t.Errorf("fact %q is missing:\n%s", fact, out)
		}
	}
	for _, command := range []string{"`" + ValidateFindingCommand + " " + st.Revision + " b:code repair`", "`" + ValidateImplementCommand + " " + st.Revision + "`"} {
		if n := strings.Count(out, command); n != 1 {
			t.Errorf("%s appears %d times:\n%s", command, n, out)
		}
	}
}

// AC (#1314): a red report's summary shows each command at most once, names
// the failing test, and carries no shipping authorization boilerplate.
func TestRedReportSummaryIsShortAndPlain(t *testing.T) {
	st := redState()
	out := renderValidateReport(st, twoTargets, "")
	summary := summaryOf(out)
	if !strings.Contains(summary, "TestImplementGraphRun") {
		t.Errorf("the failing test is not named:\n%s", summary)
	}
	for _, boiler := range []string{"authorizes the existing ordered sequence", "Displayed delivery commit", "Complete decision guidance", "Legacy repair option", "Give each command as"} {
		if strings.Contains(summary, boiler) {
			t.Errorf("summary carries %q:\n%s", boiler, summary)
		}
	}
	seen := map[string]bool{}
	for _, piece := range strings.Split(summary, "`") {
		if strings.HasPrefix(piece, "/") {
			if seen[piece] {
				t.Errorf("command %q twice in the summary", piece)
			}
			seen[piece] = true
		}
	}
	if n := summaryContentLines(out); n > validateSummaryLines {
		t.Errorf("summary has %d lines:\n%s", n, summary)
	}
	if !strings.Contains(out, "269 passed in 427.3s") {
		t.Error("the output tail lost its plain numbers")
	}
}

func reporterSummary() *agentresult.ValidateReport {
	return &agentresult.ValidateReport{
		Mode: agentresult.ReportModeReport, Headline: "One test fails in internal/turn; *it fails on main too*.",
		WhatFailed: []string{"TestImplementGraphRun: no owner wait after a sync conflict"},
		Why:        "The same test fails on the base commit | so this story did not cause it.", Certainty: "likely",
		Next: &agentresult.ReportItem{Ref: "failure-1", Action: agentresult.ReportActionWaitForMain, Recommendation: "Wait until main is fixed, then bring main in.", Diagnosis: agentresult.DiagnosisAlreadyFailing, Evidence: "rerun-1"},
		Items: []agentresult.ReportItem{
			{Ref: "b:AC-002", Action: agentresult.ReportActionCheckPass, Recommendation: "Open the page and confirm the result list shows.", Diagnosis: agentresult.DiagnosisNone},
			{Ref: "b:plan", Action: agentresult.ReportActionDefer, Recommendation: "Optional; defer it.", Diagnosis: agentresult.DiagnosisNone},
			{Ref: "b:code", Action: agentresult.ReportActionShip, Recommendation: "A stale advice line the factory must not render.", Diagnosis: agentresult.DiagnosisStoryDefect},
		},
		Reruns: []agentresult.ReportRerun{{Command: "go test ./internal/turn -run TestImplementGraphRun", Commit: "base", Result: "failed"}},
	}
}

// The reporter's stored summary renders with factory commands; an item whose
// action no longer applies gets the factory's own line; GitHub and Linear
// escape the same text for their destination.
func TestReporterSummaryRendersFromStorageForEachForge(t *testing.T) {
	st := redState()
	st.Summary, st.Advice = reporterSummary(), 1
	linear := *twoTargets
	linear.Task = naming.Ref{Forge: "linear", Owner: "acme", Repo: "eng", Issue: 7}
	gh := renderValidateReport(st, twoTargets, "")
	ln := renderValidateReport(st, &linear, "")
	if gh != renderValidateReport(st, twoTargets, "") || ln != renderValidateReport(st, &linear, "") {
		t.Fatal("rendering is not deterministic")
	}
	for name, out := range map[string]string{"github": gh, "linear": ln} {
		summary := summaryOf(out)
		if name == "linear" {
			summary, _, _ = strings.Cut(out, "**Details**")
		}
		if !strings.Contains(summary, "**Next:** Wait until main is fixed, then bring main in") || !strings.Contains(summary, "`"+ValidateBringMainCommand+"`") {
			t.Errorf("%s: next step:\n%s", name, summary)
		}
		if !strings.Contains(summary, "`"+ValidateCheckCommand+" "+st.Revision+" b:AC-002 pass -- <what you checked>`") || !strings.Contains(summary, "`"+ValidateFindingCommand+" "+st.Revision+" b:plan defer`") {
			t.Errorf("%s: agent items lost their commands:\n%s", name, summary)
		}
		if strings.Contains(out, "A stale advice line") || !strings.Contains(summary, "`"+ValidateFindingCommand+" "+st.Revision+" b:code repair`") {
			t.Errorf("%s: an inapplicable action was rendered:\n%s", name, summary)
		}
		if !strings.Contains(out, "already-failing") || !strings.Contains(out, "rerun-1") || !strings.Contains(out, "Summary by the Validate reporter") {
			t.Errorf("%s: the details lost the diagnosis or re-runs:\n%s", name, out)
		}
	}
	if !strings.Contains(gh, "<details>") || strings.Contains(ln, "<details>") || !strings.Contains(ln, "**Details**") {
		t.Error("only GitHub collapses the details; Linear keeps one collapsible")
	}
	if strings.Contains(ln, "&#") || !strings.Contains(ln, `\*it fails on main too\*`) || !strings.Contains(ln, `\|`) {
		t.Errorf("Linear escaping:\n%s", ln)
	}
	if strings.Contains(gh, "*it fails on main too*") {
		t.Errorf("GitHub left agent Markdown active:\n%s", gh)
	}
}

// The reporter step: a failed reporter falls back and Validate goes on; a
// valid summary is stored once and the replayed step starts nothing.
func TestReporterStepFallsBackAndStoresOnce(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("the sum is wrong"), ActivityResult{}
	}
	nodes := f.runToEnd()
	if !strings.HasSuffix(joinNodes(nodes), "outcome>reporter") || len(f.reporterReqs) != 1 {
		t.Fatalf("path %s, reporter runs %d", joinNodes(nodes), len(f.reporterReqs))
	}
	rows, err := f.db.ListValidateReporter(t.Context(), f.instance.ID)
	if err != nil || len(rows) != 1 || rows[0].State != store.ReporterFallback || !strings.Contains(rows[0].Reason, "no reporter in this fixture") {
		t.Fatalf("fallback row %+v %v", rows, err)
	}
	req := f.reporterReqs[0]
	if req.Scope.Report == nil || len(req.Scope.Report.Items["failure-1"]) == 0 || !strings.Contains(req.Prompt, "<validate-reporter>") || !strings.Contains(req.Prompt, "TestTotal") {
		t.Fatalf("the reporter got no scope or facts: %+v", req.Scope.Report)
	}
	st, err := LoadValidateState(t.Context(), f.opts, f.profile, f.instance.ID)
	if err != nil || st.Summary != nil || st.ReporterFallback == "" {
		t.Fatalf("fallback state %+v %v", st.Summary, err)
	}
	if out := renderValidateReport(st, nil, ""); !strings.Contains(out, "Summary by the factory from the records") || !strings.Contains(summaryOf(out), "TestTotal") {
		t.Fatalf("fallback report:\n%s", out)
	}
}

// The stored summary is accepted only when valid for the current scope, and
// renders the same bytes from storage on every load.
func TestReporterStepStoresAValidSummary(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("the sum is wrong"), ActivityResult{}
	}
	f.reporterRun = func(req ValidateReporterRequest) (*agentresult.Result, error) {
		next := agentresult.ReportItem{Ref: "failure-1", Action: req.Scope.Report.Items["failure-1"][0], Recommendation: "Send it back so the total is rounded right.", Diagnosis: agentresult.DiagnosisStoryDefect}
		return &agentresult.Result{Outcome: "completed", Comment: "Summary written.", Payload: agentresult.Payload{Validate: &agentresult.Validate{Report: &agentresult.ValidateReport{Mode: agentresult.ReportModeReport, Headline: "TestTotal fails: Total returns 9.", WhatFailed: []string{"TestTotal in app"}, Next: &next}}}}, nil
	}
	f.runToEnd()
	rows, err := f.db.ListValidateReporter(t.Context(), f.instance.ID)
	if err != nil || len(rows) != 1 || rows[0].State != store.ReporterAccepted {
		t.Fatalf("rows %+v %v", rows, err)
	}
	var stored agentresult.ValidateReport
	if err := json.Unmarshal([]byte(rows[0].Result), &stored); err != nil || stored.Headline != "TestTotal fails: Total returns 9." {
		t.Fatalf("stored %+v %v", stored, err)
	}
	a, err := LoadValidateState(t.Context(), f.opts, f.profile, f.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadValidateState(t.Context(), f.opts, f.profile, f.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	ra, rb := renderValidateReport(a, nil, ""), renderValidateReport(b, nil, "")
	if ra != rb || !strings.Contains(summaryOf(ra), "TestTotal fails") || !strings.Contains(summaryOf(ra), "`"+ValidateImplementCommand+" "+a.Revision+"`") || a.Revision != b.Revision {
		t.Fatalf("render from storage is not byte-stable:\n%s", ra)
	}

	// An invalid summary (an embedded command) falls back.
	g := newFlowFixture(t)
	g.suiteExit = f.suiteExit
	g.agent = f.agent
	g.reporterRun = func(req ValidateReporterRequest) (*agentresult.Result, error) {
		res, _ := f.reporterRun(req)
		res.Payload.Validate.Report.Headline = "Comment /validate-ship now"
		return res, nil
	}
	g.runToEnd()
	rows, err = g.db.ListValidateReporter(t.Context(), g.instance.ID)
	if err != nil || len(rows) != 1 || rows[0].State != store.ReporterFallback || !strings.Contains(rows[0].Reason, "not valid") {
		t.Fatalf("invalid summary row %+v %v", rows, err)
	}
}

// The follow-up loop is bounded per run; plain comments are told apart from
// commands, and a graph without discuss refuses them.
func TestReporterDiscussionIsBounded(t *testing.T) {
	for body, want := range map[string]bool{"Why does it fail on main?": true, "/validate-check 0123456789ab a:AC-001 pass -- ok": false, "/validate Validate the finished work.": false, "  ": false} {
		if got := isValidateDiscussion(body); got != want {
			t.Errorf("%q: discussion %v", body, got)
		}
	}
	st := redState()
	st.Discussion, st.DiscussionRounds = true, 5
	step := validateStep{node: graphexec.Node{Name: "owner_resolve", Action: graphexec.ValidateResolveAction, Outcomes: graphexec.ValidateResolveOutcomes}}
	var refused []string
	refuse := func(outcome, why string) (ActivityResult, error) {
		refused = append(refused, why)
		return validateResolveResult(outcome, why, st)
	}
	h := ValidateResolveActivity{}
	wait := store.GraphOwnerWait{ReplyID: 40, ReplyBody: "Why?"}
	for i := range 5 {
		out, err := h.discuss(t.Context(), Options{}, ActivityRequest{}, step, st, wait, refuse)
		if err != nil || out.Outcome != "discuss" {
			t.Fatalf("round %d: %+v %v", i+1, out, err)
		}
		st.Answers = append(st.Answers, store.ValidateReporterResult{CommentID: int64(100 + i), Mode: store.ReporterModeFollowUp})
	}
	out, _ := h.discuss(t.Context(), Options{}, ActivityRequest{}, step, st, wait, refuse)
	if out.Outcome != "wait" || len(refused) != 1 || !strings.Contains(refused[0], "limit") {
		t.Fatalf("a sixth comment was not refused: %+v %v", out, refused)
	}
	// A replayed comment the reporter already holds goes on, within the bound.
	wait.ReplyID = 104
	if out, _ := h.discuss(t.Context(), Options{}, ActivityRequest{}, step, st, wait, refuse); out.Outcome != "discuss" {
		t.Fatalf("replay refused: %+v", out)
	}
	step.node.Outcomes = []string{"wait"}
	st.Answers = nil
	wait.ReplyID = 41
	if out, _ := h.discuss(t.Context(), Options{}, ActivityRequest{}, step, st, wait, refuse); out.Outcome != "wait" {
		t.Fatalf("a graph without discuss answered: %+v", out)
	}
}

// The owner wait shows the answer once, says it decides nothing, and keeps a
// single Next line without authorization boilerplate.
func TestReporterAnswerOnTheOwnerWait(t *testing.T) {
	st := redState()
	raw, _ := json.Marshal(agentresult.ValidateReport{Mode: agentresult.ReportModeFollowUp, Answer: "It fails on main too, so waiting is safe."})
	answer := store.ValidateReporterResult{Mode: store.ReporterModeFollowUp, CommentID: 40, Question: "Is it safe to wait?", State: store.ReporterAccepted, Result: string(raw)}
	wait := renderValidateComment(validateQuestionCandidateFor(false, st, nil, "", &answer), "", naming.Ref{}, "")
	if !strings.Contains(wait, "**Answer:** It fails on main too") || !strings.Contains(wait, "decides nothing and permits nothing") || nextLines(wait) != 1 || strings.Contains(wait, "authorizes the existing ordered sequence") {
		t.Fatalf("wait:\n%s", wait)
	}
	failed := store.ValidateReporterResult{Mode: store.ReporterModeFollowUp, CommentID: 41, Question: "And now?", State: store.ReporterFallback, Reason: "timeout"}
	if wait := renderValidateComment(validateQuestionCandidateFor(false, st, nil, "", &failed), "", naming.Ref{}, ""); !strings.Contains(wait, "could not answer") || !strings.Contains(wait, "Nothing was recorded") {
		t.Fatalf("failed answer:\n%s", wait)
	}
}

// The reporter's scope admits exactly the current items and the summary it
// validates is the one the factory renders.
func TestReporterScopeMatchesTheState(t *testing.T) {
	st := redState()
	scope := validateReportScope(st, false, []string{"https://github.com/acme/api/blob/" + st.Commit + "/"})
	for _, ref := range []string{"failure-1", "b:AC-002", "b:plan", "b:code", st.Candidates[0].ID} {
		if len(scope.Items[ref]) == 0 {
			t.Errorf("scope lacks %s: %+v", ref, scope.Items)
		}
	}
	if _, ok := scope.Items["a:AC-001"]; ok {
		t.Error("a satisfied criterion is an item")
	}
	report := reporterSummary()
	report.Items = report.Items[:2]
	ctx := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "validate", WorkflowVersion: store.WorkflowSeparated, Report: scope}
	result := agentresult.Result{Outcome: "completed", Comment: "Summary.", Payload: agentresult.Payload{Validate: &agentresult.Validate{Report: report}}}
	if d := agentresult.ValidateResult(result, ctx); !d.Valid {
		t.Fatalf("a summary of the current items was refused: %+v", d.Diagnostics)
	}
	report.Items = append(report.Items, agentresult.ReportItem{Ref: "b:code", Action: agentresult.ReportActionShip, Recommendation: "Ship it.", Diagnosis: agentresult.DiagnosisNone})
	if d := agentresult.ValidateResult(result, ctx); d.Valid {
		t.Fatal("an action the item does not admit was accepted")
	}
}

// The factory Bug filing text holds the facts and the stable marker.
func TestFactoryBugTextCarriesFactsAndMarker(t *testing.T) {
	st := redState()
	marker := validateFactoryBugMarker(st.InstanceID, st.Commit, "failure-1")
	if marker != validateFactoryBugMarker(st.InstanceID, st.Commit, "failure-1") || !strings.HasPrefix(marker, "<!-- lf:validate-factory-bug v=1 key=") {
		t.Fatalf("marker %q", marker)
	}
	title, body := validateFactoryBugText(st, "github:acme/api#497", store.ValidateFactoryBug{Item: "failure-1", CommentID: 9, Marker: marker})
	if !strings.Contains(title, "make test") || !strings.Contains(body, "269 passed in 427.3s") || !strings.Contains(body, "No owner wait is recorded") || !strings.HasSuffix(strings.TrimSpace(body), marker) {
		t.Fatalf("title %q body:\n%s", title, body)
	}
}
