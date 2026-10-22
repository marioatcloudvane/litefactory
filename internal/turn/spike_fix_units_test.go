package turn

import (
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestWriteAccessMapsToTheSpikeFixerToolsUnderTheModeCeiling(t *testing.T) {
	policy := SpikeWorkflowPolicy()
	write := graphexec.Node{Name: "n", Attrs: map[string]string{graphexec.AccessAttribute: "write"}}
	fixerTools := config.BugRoleClaudeTools(config.SpikeFixerProfile)
	claude := func(tools, allowed []string) Request {
		return Request{Agent: agent.Kind(store.AgentClaude), Tools: tools, AllowedTools: allowed}
	}
	t.Run("claude grants the role mapping with a contained shell and no delegation", func(t *testing.T) {
		got, err := applyNodeAccess(policy, write, claude([]string{"Read", "Glob", "Grep", "Edit", "MultiEdit", "Write", "Bash", "Task"}, []string{"Read", "Edit", "Bash(go test:*)", "Task"}))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Tools, fixerTools) || strings.Join(got.AllowedTools, ",") != "Read,Edit,Bash(go test:*)" || !got.nodeWrite {
			t.Fatalf("tools=%v allowed=%v write=%v", got.Tools, got.AllowedTools, got.nodeWrite)
		}
		if !slices.Contains(got.Tools, "Bash") || slices.Contains(got.Tools, "Task") {
			t.Fatal("the fixer cannot run its checks, or was given delegation")
		}
	})
	t.Run("claude is capped at a narrower ceiling", func(t *testing.T) {
		got, err := applyNodeAccess(policy, write, claude([]string{"Read", "Edit"}, nil))
		if err != nil || strings.Join(got.Tools, ",") != "Read,Edit" {
			t.Fatalf("tools=%v err=%v", got.Tools, err)
		}
		if _, err := applyNodeAccess(policy, write, claude([]string{"Read", "Glob", "Grep"}, nil)); err == nil {
			t.Fatal("write access was granted over a read-only ceiling")
		}
		if _, err := applyNodeAccess(policy, write, claude([]string{"Bash"}, nil)); err == nil {
			t.Fatal("write access was granted over a ceiling with none of its tools")
		}
	})
	t.Run("claude with no explicit surface gets the role mapping", func(t *testing.T) {
		got, err := applyNodeAccess(policy, write, claude(nil, nil))
		if err != nil || !slices.Equal(got.Tools, fixerTools) {
			t.Fatalf("tools=%v err=%v", got.Tools, err)
		}
	})
	t.Run("codex uses the workspace sandbox and never widens read-only", func(t *testing.T) {
		got, err := applyNodeAccess(policy, write, Request{Agent: agent.Kind(store.AgentCodex), CodexSandbox: "workspace-write"})
		if err != nil || got.CodexSandbox != "workspace-write" || !got.nodeWrite {
			t.Fatalf("sandbox=%q err=%v", got.CodexSandbox, err)
		}
		if _, err := applyNodeAccess(policy, write, Request{Agent: agent.Kind(store.AgentCodex), CodexSandbox: "read-only"}); err == nil {
			t.Fatal("write access widened a read-only sandbox")
		}
	})
	t.Run("absent access stays read and never write", func(t *testing.T) {
		got, err := applyNodeAccess(policy, graphexec.Node{Name: "n", Attrs: map[string]string{}}, claude([]string{"Read", "Edit", "Bash"}, nil))
		if err != nil || got.nodeWrite || strings.Join(got.Tools, ",") != "Read" {
			t.Fatalf("tools=%v write=%v err=%v", got.Tools, got.nodeWrite, err)
		}
	})
	t.Run("an agent without a mapping is refused", func(t *testing.T) {
		if _, err := applyNodeAccess(policy, write, Request{Agent: agent.Kind("other")}); err == nil {
			t.Fatal("unknown agent was granted write access")
		}
	})
}

func TestSpikePathKindsAndMeasurement(t *testing.T) {
	forbid := []string{"package", "dependency", "schema", "api"}
	for path, want := range map[string]string{
		"go.mod": "dependency", "sub/go.sum": "dependency", "package.json": "dependency", "web/yarn.lock": "dependency",
		"requirements-dev.txt": "dependency", "db/migrations/001_a.sql": "schema", "internal/store/migrations/0086.sql": "schema",
		"internal/turn/a.go": "", "docs/migrations.md": "",
	} {
		got := strings.Join(spikePathKinds(path), ",")
		if got != want {
			t.Errorf("%s: kinds %q, want %q", path, got, want)
		}
	}
	session := ".aiteam/lf-1/2026/run"
	files, kinds, forbidden := measureSpikeChange([]string{"a.go", session + "/SPIKE.md", session, "go.mod", "b_test.go", ".aiteam/other/notes.md"}, session, []string{"api"}, forbid)
	if strings.Join(files, ",") != ".aiteam/other/notes.md,a.go,b_test.go,go.mod" {
		t.Fatalf("only the session directory is excluded: %v", files)
	}
	if strings.Join(kinds, ",") != "api,dependency" || strings.Join(forbidden, ",") != "api,dependency" {
		t.Fatalf("kinds=%v forbidden=%v", kinds, forbidden)
	}
	if _, _, none := measureSpikeChange([]string{"a.go"}, session, []string{"api"}, []string{"schema"}); len(none) != 0 {
		t.Fatalf("a kind the graph does not forbid was forbidden: %v", none)
	}
}

func TestSpikePhasesAreAClosedOrderedList(t *testing.T) {
	got, err := DecodeSpikePhases(map[string]string{"lf_phases": "focused, full"})
	if err != nil || strings.Join(got, ",") != "focused,full" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"", "unit", "full,full", "focused,", "Full"} {
		if _, err := DecodeSpikePhases(map[string]string{"lf_phases": bad}); err == nil {
			t.Errorf("phases %q accepted", bad)
		}
	}
	spec := SpikeCheckSpec()
	if !spec.Attributes["lf_phases"].List || !spec.Attributes["lf_phases"].Required || strings.Join(spec.Attributes["lf_phases"].Values, ",") != "regression,focused,full" {
		t.Fatalf("phase attribute: %+v", spec.Attributes["lf_phases"])
	}
	if strings.Join(spec.Outcomes, ",") != "passed,failed,too_big,blocked" || strings.Join(SpikeIssueSpec().Outcomes, ",") != "filed,failed" {
		t.Fatal("check and issue outcomes are not the closed lists")
	}
	control := SpikeControlPolicy()
	for _, action := range []string{SpikeCheckAction, SpikeIssueAction} {
		if !slices.Contains(control.MeasureActions, action) || !slices.Contains(control.FindingActions, action) {
			t.Errorf("%s is not a declared measure finding", action)
		}
	}
	if slices.Contains(control.FindingActions, SpikeNoteAction) || slices.Contains(control.FindingActions, SpikeGateAction) {
		t.Error("a note or gate failure became a finding")
	}
}

func TestSpikeStatusesCoverTheFixOutcomes(t *testing.T) {
	for _, status := range []string{"fixed", "fix_unverified", "fix_too_big", "fix_proposed"} {
		if !slices.Contains(agentresult.SpikeStatuses, status) || spikeStatusLine[status] == "" {
			t.Errorf("status %q is not declared with a report line", status)
		}
	}
	values := SpikeNoteSpec().Attributes["lf_status"].Values
	if !slices.Contains(values, "fix_too_big") {
		t.Fatalf("note statuses: %v", values)
	}
}

func TestAnAskNobodyCanAnswerIsReportedAsBlockedWithoutQuestions(t *testing.T) {
	asked := agentresult.Result{Outcome: "incomplete", Reason: "stuck", Comment: "c", Questions: []agentresult.Question{{ID: "q1", Audience: "technical", Text: "Which flag?"}}, Payload: agentresult.Payload{Spike: &agentresult.Spike{Request: "r", NotDone: []string{"finish"}}}}
	settled := settleUnaskedQuestions(asked, "final", "blocked")
	if len(settled.Questions) != 0 || len(settled.Payload.Spike.NotDone) != 2 || !strings.Contains(settled.Payload.Spike.NotDone[1], "Which flag?") {
		t.Fatalf("settled: %+v", settled)
	}
	if len(asked.Questions) != 1 || len(asked.Payload.Spike.NotDone) != 1 {
		t.Fatal("the retained candidate was mutated")
	}
	if kept := settleUnaskedQuestions(asked, "question", "asking"); len(kept.Questions) != 1 {
		t.Fatal("a real question was dropped")
	}
	scope := SpikeWorkflowPolicy().baseContext(true)
	settled.Payload.Spike.Status = "blocked"
	if err := validateSpikeComment(store.GraphNodeAttempt{Status: store.GraphSucceeded}, "final", settled, scope); err != nil {
		t.Fatalf("a blocked report of an unasked question was refused: %v", err)
	}
	settled.Payload.Spike.Status = "fixed"
	if err := validateSpikeComment(store.GraphNodeAttempt{Status: store.GraphSucceeded}, "final", settled, scope); err == nil {
		t.Fatal("an unfinished step was reported as fixed")
	}
	body := renderSpikeComment(settled, "", naming.Ref{}, "")
	if strings.Contains(body, "Which flag?") && !strings.Contains(body, "Not asked") {
		t.Fatalf("the report puts the question to the owner: %s", body)
	}
}

func TestSpikeReportStatesTheMeasuredCheckAndTheFilingFromRecords(t *testing.T) {
	candidate := agentresult.Result{Outcome: "completed", Comment: "c", Payload: agentresult.Payload{Spike: &agentresult.Spike{
		Request: "r", Status: "fix_too_big",
		FixReport: &agentresult.SpikeFixReport{Defect: "d", Files: []string{"a.go"}, RegressionTest: "a.go", Summary: "s"},
		Check:     &agentresult.SpikeCheckView{Outcome: "too_big", Files: []string{"a.go", "b.go", "c.go", "d.go"}, Forbidden: []string{"dependency"}, Reason: "4 files"},
		Issue:     &agentresult.SpikeIssueView{State: "filed", Number: 501, URL: "https://forge.invalid/i/501"},
	}}}
	body := renderSpikeComment(candidate, "", naming.Ref{}, "")
	for _, want := range []string{"`too_big`", "Files changed (4)", "dependency", "No repository checks were run", "not ready", "https://forge.invalid/i/501"} {
		if !strings.Contains(body, want) {
			t.Errorf("report lacks %q:\n%s", want, body)
		}
	}
	candidate.Payload.Spike.Issue = &agentresult.SpikeIssueView{State: "uncertain"}
	if body := renderSpikeComment(candidate, "", naming.Ref{}, ""); !strings.Contains(body, "could not be confirmed") || strings.Contains(body, "forge.invalid") {
		t.Fatalf("an unconfirmed filing was reported as filed:\n%s", body)
	}
	candidate.Payload.Spike.Issue = nil
	if body := renderSpikeComment(candidate, "", naming.Ref{}, ""); strings.Contains(body, "Follow-up issue") {
		t.Fatal("a skipped filing was reported")
	}
	// The report contract strips factory records before judging the authored result.
	scope := SpikeWorkflowPolicy().baseContext(true)
	scope.WriteAccess = true
	if err := validateSpikeComment(store.GraphNodeAttempt{Status: store.GraphSucceeded}, "final", candidate, scope); err != nil {
		t.Fatalf("contract: %v", err)
	}
}

func TestShippedSpikeFixPathHasNoReturnEdgeAndNoOwnerWaitForTheFixer(t *testing.T) {
	specs, err := SpikeSpecs()
	mustProgress(t, err)
	definition, err := teamsource.CaptureSpike(t.Context(), specs)
	mustProgress(t, err)
	profile := definition.Profile
	node := func(action string, attr, value string) []string {
		var out []string
		for _, n := range profile.Nodes {
			if n.Action == action && (attr == "" || n.Attrs[attr] == value) {
				out = append(out, n.Name)
			}
		}
		return out
	}
	fixers := node("lf_agent", "lf_access", "write")
	checks := node(SpikeCheckAction, "", "")
	if len(fixers) != 2 || len(checks) != 2 {
		t.Fatalf("want one fixer and one repair pass, each checked: fixers=%v checks=%v", fixers, checks)
	}
	into := func(target string) []graphexec.Edge {
		var from []graphexec.Edge
		for _, e := range profile.Edges {
			if e.To == target {
				from = append(from, e)
			}
		}
		return from
	}
	checkOf := map[string]string{}
	for _, check := range checks {
		c, _ := profile.Node(check)
		checkOf[c.Attrs["lf_source"]] = check
	}
	for _, fixer := range fixers {
		check, ok := checkOf[fixer]
		if !ok {
			t.Fatalf("fixer %s is never checked", fixer)
		}
		// Entry: the first fixer only by the gate's small outcome, the repair
		// only by the first check's failed outcome.
		entries := into(fixer)
		if len(entries) != 1 {
			t.Fatalf("edges into %s: %v", fixer, entries)
		}
		from, _ := profile.Node(entries[0].From)
		first := from.Action == SpikeGateAction && entries[0].On == "small"
		repair := from.Action == SpikeCheckAction && entries[0].On == "failed" && from.Attrs["lf_source"] != fixer
		if !first && !repair {
			t.Fatalf("%s is entered other than by the gate's small outcome or a failed check: %+v", fixer, entries[0])
		}
		for _, e := range into(check) {
			if n, _ := profile.Node(e.From); n.Action != "lf_if" || n.Attrs["lf_source"] != fixer {
				t.Fatalf("the check is entered other than from the fixer's own response test: %s", e.From)
			}
		}
		// No path leaving a fixer or its check ever returns to either.
		for _, start := range []string{fixer, check} {
			seen := map[string]bool{}
			var walk func(string)
			walk = func(name string) {
				for _, e := range profile.Edges {
					if e.From != name || seen[e.To] {
						continue
					}
					if e.To == fixer || (start == check && e.To == check) {
						t.Fatalf("a path from %s returns to %s", start, e.To)
					}
					seen[e.To] = true
					walk(e.To)
				}
			}
			walk(start)
			for name := range seen {
				if n, _ := profile.Node(name); n.Action == "lf_ask" {
					t.Fatalf("the owner-question node %q is reachable from %s", name, start)
				}
			}
			terminals := 0
			for name := range seen {
				if n, _ := profile.Node(name); n.Terminal {
					terminals++
					if name == "waiting" {
						t.Fatalf("%s can end at an owner wait", start)
					}
				}
			}
			if terminals == 0 {
				t.Fatalf("%s never reaches a terminal", start)
			}
		}
		if graphexec.ResponseReachesOwnerWait(profile, fixer, SpikeControlPolicy()) {
			t.Fatalf("the fixer %s's ask reaches an owner question", fixer)
		}
		// A fixer or note failure reports and stops.
		for _, name := range append(node(SpikeNoteAction, "lf_source", fixer), fixer) {
			to, ok := graphexec.Decide(profile, name, "failed")
			if !ok {
				t.Fatalf("%s has no failure edge", name)
			}
			if n, _ := profile.Node(to); n.Attrs["lf_kind"] != "failure" {
				t.Fatalf("%s failure goes to %s", name, to)
			}
		}
	}
	investigators := node("lf_agent", "lf_access", "read")
	if len(investigators) != 1 || !graphexec.ResponseReachesOwnerWait(profile, investigators[0], SpikeControlPolicy()) {
		t.Fatal("the investigator's ask no longer reaches an owner question")
	}
}
