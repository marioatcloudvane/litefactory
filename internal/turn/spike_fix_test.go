package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const spikeRecipe = `version: 1
phases:
  regression:
    commands:
      - argv: ["check-regression"]
        cwd: "."
        timeout_seconds: 5
  focused:
    commands:
      - argv: ["check-focused"]
        cwd: "."
        timeout_seconds: 5
  full:
    commands:
      - argv: ["check-full", "./..."]
        cwd: "."
        timeout_seconds: 5
`

const spikeFixerDone = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"The fix and its test are written.","payload":{"spike":{"request":"Why does staging fail?","work_done":["Edited the flag"],"findings":["Wrong flag"],"not_done":[],"fix_report":{"defect":"Wrong flag","files":["a.go","a_test.go"],"regression_test":"a_test.go","change_kinds":[],"summary":"Corrected the flag and tested it."}}}}}`

// spikeIssueDouble is a forge that records every issue it is asked to create.
type spikeIssueDouble struct {
	mu        sync.Mutex
	issues    []github.Issue
	creates   int
	attaches  int
	createErr error
	// lose makes the forge keep a created issue but answer the call with an error.
	lose bool
	// hide keeps created issues out of listings.
	hide     bool
	onCreate func(body string)
}

func (d *spikeIssueDouble) CreateIssue(_ context.Context, _ github.Repo, title, body redact.Body) (github.Issue, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.creates++
	if d.onCreate != nil {
		d.onCreate(body.String())
	}
	if d.createErr != nil {
		return github.Issue{}, d.createErr
	}
	n := 500 + len(d.issues)
	issue := github.Issue{ID: int64(9000 + n), Number: n, Title: title.String(), Body: body.String(), State: github.StateOpen, Author: github.User{ID: 9999}, HTMLURL: fmt.Sprintf("https://forge.invalid/acme/acme-api/issues/%d", n)}
	d.issues = append(d.issues, issue)
	if d.lose {
		return github.Issue{}, errors.New("connection reset after the request was sent")
	}
	return issue, nil
}

func (d *spikeIssueDouble) ListIssuesPage(_ context.Context, _ github.Repo, state string, page, _ int) (github.IssuePage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := github.IssuePage{Items: []github.Issue{}}
	if state == github.StateOpen && page == 1 && !d.hide {
		out.Items = append(out.Items, d.issues...)
	}
	return out, nil
}

func (d *spikeIssueDouble) AddSubIssue(context.Context, github.Repo, int, int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attaches++
	return nil
}
func (d *spikeIssueDouble) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{}, github.ErrNotFound
}
func (d *spikeIssueDouble) GetParentIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{}, github.ErrNotFound
}

// spikeFix drives the whole fix path: investigator, gate, fixer, check, notes,
// issue and report, over a real repository with a recording command runner.
type spikeFix struct {
	t        *testing.T
	f        *fixture
	forge    *fakeForge
	issues   *spikeIssueDouble
	req      Request
	mu       sync.Mutex
	stages   []string
	requests map[string]agent.Request
	commands [][]string
	// edit runs in the worktree when the fixer runs.
	edit func(worktree string)
	// fixerResult is what the fixer submits.
	fixerResult string
	// investigator is what the investigator submits.
	investigator string
	// command decides each recipe command's outcome; nil passes everything.
	command func(argv []string) bugchecks.CommandResult
	// failFixer makes the fixer's process fail.
	failFixer bool
	// graph edits the shipped graph file before it is captured.
	graph func(string) string
	// refused is submitted first by the fixer and must be refused by the route.
	refused string
	// noRecipe leaves the recipe out of the repository.
	noRecipe bool
}

func (s *spikeFix) start(configure func(*spikeFix)) Result {
	t := s.t
	t.Helper()
	if s.edit == nil {
		s.edit = func(worktree string) {
			mustProgress(t, os.WriteFile(filepath.Join(worktree, "a.go"), []byte("package a\n\nconst flag = \"fixed\"\n"), 0o600))
			mustProgress(t, os.WriteFile(filepath.Join(worktree, "a_test.go"), []byte("package a\n"), 0o600))
		}
	}
	if s.fixerResult == "" {
		s.fixerResult = spikeFixerDone
	}
	if s.investigator == "" {
		s.investigator = spikeDoneWithFix(`"one defect"`, `"a.go","a_test.go"`, `"code","test"`)
	}
	s.requests = map[string]agent.Request{}
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		s.mu.Lock()
		s.stages = append(s.stages, req.Graph.Node)
		s.requests[req.Graph.Node] = req
		s.mu.Unlock()
		switch {
		case req.Graph.Node == "probe_agent":
			submitLiveResult(t, req, s.investigator)
		case s.failFixer:
			return &agent.RunResult{Outcome: agent.OutcomeTimedOut, Reason: "timeout", FinishedAt: time.Now()}, nil
		default:
			s.edit(req.WorktreePath)
			if s.refused != "" {
				var node struct {
					Result json.RawMessage `json:"result"`
				}
				mustProgress(t, json.Unmarshal([]byte(s.refused), &node))
				data := append(append([]byte(`{"result":`), node.Result...), '}')
				got := mcpDecodeSubmission(t, mcpToolCall(t, submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, "communication_submit", data))
				if got.Validation.Valid {
					t.Fatalf("the shared route accepted an invalid fixer result")
				}
			}
			submitLiveResult(t, req, s.fixerResult)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner, "execution: graph\n")
	s.f, s.forge, s.req = f, forge, req
	s.issues = &spikeIssueDouble{}
	authorizer, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: mustCreationStore(t, f.db), Enabled: false, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	f.opts.MCP.Creation, f.opts.MCP.CreationAuthorizer = s.issues, authorizer
	f.opts.spikeRunCommand = func(_ context.Context, _ string, c config.BugCheckCommand) bugchecks.CommandResult {
		s.mu.Lock()
		s.commands = append(s.commands, c.Argv)
		s.mu.Unlock()
		if s.command != nil {
			return s.command(c.Argv)
		}
		zero := 0
		return bugchecks.CommandResult{Outcome: "passed", Exit: &zero, Output: "ok"}
	}
	if configure != nil {
		configure(s)
	}
	if s.graph != nil {
		f.opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, _ graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
			return fixtureInstalled(captureSpikeVariant(ctx, t, SpikeWorkflowPolicy(), s.graph))
		}
	}
	if !s.noRecipe {
		f.repo.WriteFile(config.BugRecipePath, spikeRecipe)
		f.repo.Commit("add the check recipe")
		f.repo.Git("push", "origin", "main")
	}
	out, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	return out
}

// continueOwed runs every stage the graph owes, as the daemon's chain does.
func (s *spikeFix) continueOwed() Result {
	t := s.t
	t.Helper()
	var out Result
	for range 8 {
		due, err := s.f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Minute), 32)
		mustProgress(t, err)
		if len(due) == 0 {
			return out
		}
		reserved, ok, err := s.f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
		mustProgress(t, err)
		if !ok {
			t.Fatal("owed stage could not be reserved")
		}
		instruction := fmt.Sprintf("Continue the workflow at its %s stage.", reserved.Node)
		req := s.req
		req.Trigger = Trigger{CommentID: reserved.TriggerID(), Body: instruction, Author: s.req.Trigger.Author}
		req.Command = Command{Kind: KindFreeText, Text: instruction}
		out, err = Execute(t.Context(), s.f.opts, req)
		mustProgress(t, err)
		mustProgress(t, s.f.db.FinishGraphDispatch(t.Context(), reserved.ID, time.Now()))
	}
	t.Fatal("the graph kept owing stages")
	return out
}

func (s *spikeFix) run(configure func(*spikeFix)) Result {
	s.start(configure)
	return s.continueOwed()
}

func (s *spikeFix) worktree() string {
	s.t.Helper()
	session, err := s.f.db.GetSession(s.t.Context(), s.f.session.ID)
	mustProgress(s.t, err)
	return session.WorktreePath
}

func (s *spikeFix) note() string { return spikeNote(s.t, s.worktree()) }

func (s *spikeFix) reports() []string {
	var out []string
	for _, post := range s.forge.all() {
		out = append(out, post.body)
	}
	return out
}

func (s *spikeFix) ran(node string) int {
	n := 0
	for _, stage := range s.stages {
		if stage == node {
			n++
		}
	}
	return n
}

func mustCreationStore(t *testing.T, db *store.DB) *store.CreationStore {
	t.Helper()
	cs, err := store.NewCreationStore(db, time.Now)
	mustProgress(t, err)
	return cs
}

func TestSpikeFixOnlySmallStartsTheWriteFixer(t *testing.T) {
	for name, result := range map[string]string{
		"none":      spikeDoneNone,
		"too big":   spikeDoneWithFix(`"one defect"`, `"a.go","b.go","c.go","d_test.go"`, `"code","test"`),
		"forbidden": spikeDoneWithFix(`"one defect"`, `"a.go","a_test.go"`, `"code","dependency"`),
		"blocked":   spikeDoneWithFix(`"one defect"`, `"a.go"`, ``),
	} {
		t.Run(name, func(t *testing.T) {
			s := &spikeFix{t: t}
			s.run(func(s *spikeFix) { s.investigator = result })
			if s.ran("fix_agent") != 0 || len(s.commands) != 0 || len(s.reports()) != 1 {
				t.Fatalf("the fixer ran for a %s outcome: stages=%v commands=%v", name, s.stages, s.commands)
			}
		})
	}
	t.Run("small", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.start(nil)
		instance := spikeInstance(t, s.f)
		if instance.CurrentNode != "fix_agent" || len(s.reports()) != 0 || s.note() != "" {
			t.Fatalf("a small proposal must park at the fixer with nothing recorded yet: %+v", instance)
		}
		baseline, err := s.f.db.LatestSpikeRecord(t.Context(), instance.ID, store.SpikeRecordBaseline)
		mustProgress(t, err)
		if !strings.Contains(string(baseline.Payload), s.f.repo.Head()) {
			t.Fatalf("the gate did not save the worktree HEAD: %s vs %s", baseline.Payload, s.f.repo.Head())
		}
		s.continueOwed()
		if s.ran("fix_agent") != 1 {
			t.Fatalf("stages=%v", s.stages)
		}
		surface, ok := s.requests["fix_agent"].Permissions.Payload.(harness.ToolSurface)
		if !ok || strings.Join(surface.Tools, ",") != "Read,Glob,Grep,Edit,Write" {
			t.Fatalf("fixer tools: %+v", s.requests["fix_agent"].Permissions)
		}
		if s.requests["fix_agent"].MayAsk || !s.requests["probe_agent"].MayAsk {
			t.Fatalf("question permission must follow ask reachability: fixer=%v investigator=%v", s.requests["fix_agent"].MayAsk, s.requests["probe_agent"].MayAsk)
		}
		if !strings.Contains(s.requests["fix_agent"].Prompt, "Wrong flag") {
			t.Fatal("the fixer did not receive what the investigation found")
		}
	})
}

func TestSpikeFixPassedRecordsFixedThenReportsOnce(t *testing.T) {
	s := &spikeFix{t: t}
	s.run(nil)
	note, reports := s.note(), s.reports()
	for _, want := range []string{"Status: `fixed`", "Change made", "Regression test: a_test.go", "Measured change and checks", "Files changed (2): a.go, a_test.go", "focused, full"} {
		if !strings.Contains(note, want) {
			t.Fatalf("SPIKE.md lacks %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, ".aiteam/") {
		t.Fatalf("the session artifact directory was counted: %s", note)
	}
	if len(reports) != 1 || !strings.Contains(reports[0], "**Spike status:** `fixed`") || !strings.Contains(reports[0], "a_test.go") {
		t.Fatalf("want exactly one fixed report: %v", reports)
	}
	if got := fmt.Sprint(s.commands); got != "[[check-focused] [check-full ./...]]" {
		t.Fatalf("phases run: %s", got)
	}
	if s.issues.creates != 0 || spikeInstance(t, s.f).Status != store.GraphSucceeded {
		t.Fatalf("a passing fix filed an issue or did not finish: %d", s.issues.creates)
	}
	if _, err := os.Stat(filepath.Join(s.worktree(), "a.go")); err != nil {
		t.Fatalf("the fix is not in the worktree: %v", err)
	}
	record, err := s.f.db.LatestSpikeRecord(t.Context(), spikeInstance(t, s.f).ID, store.SpikeRecordCheck)
	mustProgress(t, err)
	if !strings.Contains(string(record.Payload), `"outcome":"passed"`) || !strings.Contains(string(record.Payload), "check-focused") {
		t.Fatalf("check record: %s", record.Payload)
	}
}

func TestSpikeFixFailedCheckIsUnverifiedWithoutIssueOrRetry(t *testing.T) {
	s := &spikeFix{t: t}
	s.run(func(s *spikeFix) {
		s.command = func(argv []string) bugchecks.CommandResult {
			one := 1
			return bugchecks.CommandResult{Outcome: "failed", Exit: &one, Output: "FAIL TestFlag", Reason: "project check reported test failure"}
		}
	})
	reports := s.reports()
	if !strings.Contains(s.note(), "Status: `fix_unverified`") || len(reports) != 1 || !strings.Contains(reports[0], "not ready") {
		t.Fatalf("note=%q reports=%v", s.note(), reports)
	}
	if s.ran("fix_agent") != 1 || len(s.commands) != 1 || s.issues.creates != 0 {
		t.Fatalf("a failed check retried or filed: stages=%v commands=%v creates=%d", s.stages, s.commands, s.issues.creates)
	}
	if _, err := os.Stat(filepath.Join(s.worktree(), "a.go")); err != nil {
		t.Fatal("the unverified patch was discarded")
	}
}

func TestSpikeFixTooBigNotesThenFilesOneIssueThenReports(t *testing.T) {
	s := &spikeFix{t: t}
	var noteAtCreate string
	s.run(func(s *spikeFix) {
		s.edit = func(w string) {
			for _, name := range []string{"a.go", "b.go", "c.go", "a_test.go"} {
				mustProgress(t, os.WriteFile(filepath.Join(w, name), []byte("package a\n"), 0o600))
			}
		}
		s.fixerResult = strings.Replace(spikeFixerDone, `"files":["a.go","a_test.go"]`, `"files":["a.go","b.go","c.go","a_test.go"]`, 1)
		s.issues.onCreate = func(string) { noteAtCreate = spikeNote(t, s.worktree()) }
	})
	if !strings.Contains(noteAtCreate, "Status: `fix_too_big`") {
		t.Fatalf("SPIKE.md was not written before the issue was filed: %q", noteAtCreate)
	}
	if len(s.commands) != 0 || s.issues.creates != 1 || s.issues.attaches != 0 {
		t.Fatalf("commands=%v creates=%d attaches=%d", s.commands, s.issues.creates, s.issues.attaches)
	}
	issue := s.issues.issues[0]
	for _, want := range []string{"lf:spike-issue session=", "Original question", "Why does staging fail?", "Wrong flag", "4 files changed, at most 3 allowed", "acme/acme-api#123"} {
		if !strings.Contains(issue.Body, want) {
			t.Fatalf("issue body lacks %q:\n%s", want, issue.Body)
		}
	}
	reports := s.reports()
	if len(reports) != 1 || !strings.Contains(reports[0], "fix_too_big") || !strings.Contains(reports[0], issue.HTMLURL) {
		t.Fatalf("report must link the filed issue: %v", reports)
	}
}

func TestSpikeFixIssueFailureStillReportsUnconfirmed(t *testing.T) {
	for name, configure := range map[string]func(*spikeIssueDouble){
		"forge error":      func(d *spikeIssueDouble) { d.createErr = errors.New("forge unavailable") },
		"lost response":    func(d *spikeIssueDouble) { d.lose = true },
		"lost and hidden":  func(d *spikeIssueDouble) { d.lose, d.hide = true, true },
		"created and lost": func(d *spikeIssueDouble) { d.lose = true },
	} {
		t.Run(name, func(t *testing.T) {
			s := &spikeFix{t: t}
			s.run(func(s *spikeFix) {
				s.edit = func(w string) {
					for _, n := range []string{"a.go", "b.go", "c.go", "a_test.go"} {
						mustProgress(t, os.WriteFile(filepath.Join(w, n), []byte("package a\n"), 0o600))
					}
				}
				configure(s.issues)
			})
			reports := s.reports()
			if len(reports) != 1 || !strings.Contains(reports[0], "could not be confirmed") || s.issues.creates != 1 {
				t.Fatalf("reports=%v creates=%d", reports, s.issues.creates)
			}
			if spikeInstance(t, s.f).Status != store.GraphSucceeded {
				t.Fatal("a filing problem hid the fix outcome")
			}
		})
	}
}

func TestSpikeFixBlockedWhenTheRecipeIsMissingOrNothingChanged(t *testing.T) {
	cases := map[string]func(*spikeFix){
		"no recipe": func(s *spikeFix) { s.noRecipe = true },
		"no change": func(s *spikeFix) { s.edit = func(string) {} },
		"claim only": func(s *spikeFix) {
			s.edit = func(w string) { mustProgress(t, os.WriteFile(filepath.Join(w, "a.go"), []byte("package a\n"), 0o600)) }
		},
	}
	for name, configure := range cases {
		t.Run(name, func(t *testing.T) {
			s := &spikeFix{t: t}
			s.run(configure)
			reports := s.reports()
			if !strings.Contains(s.note(), "Status: `blocked`") || len(reports) != 1 || strings.Contains(reports[0], "`fixed`") {
				t.Fatalf("note=%q reports=%v", s.note(), reports)
			}
			if len(s.commands) != 0 || s.issues.creates != 0 {
				t.Fatalf("a blocked check ran commands or filed: %v %d", s.commands, s.issues.creates)
			}
		})
	}
}

func TestSpikeFixFixerFailureReportsOnceAndStops(t *testing.T) {
	s := &spikeFix{t: t}
	s.start(func(s *spikeFix) { s.failFixer = true })
	due, err := s.f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Minute), 8)
	mustProgress(t, err)
	reserved, ok, err := s.f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
	mustProgress(t, err)
	if !ok {
		t.Fatal("dispatch not reserved")
	}
	req := s.req
	req.Trigger = Trigger{CommentID: reserved.TriggerID(), Body: "Continue the workflow at its fix_agent stage.", Author: s.req.Trigger.Author}
	req.Command = Command{Kind: KindFreeText, Text: req.Trigger.Body}
	out, err := Execute(t.Context(), s.f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("a failed fixer did not fail the run: %+v %v", out.Run, err)
	}
	reports := s.reports()
	if len(reports) != 1 || !strings.Contains(reports[0], "did not finish") || strings.Contains(reports[0], "Spike status") {
		t.Fatalf("want one failure comment: %v", reports)
	}
	if spikeInstance(t, s.f).Status != store.GraphFailed || s.issues.creates != 0 || len(s.commands) != 0 || s.note() != "" {
		t.Fatalf("a failed fixer left a note, check or issue: creates=%d commands=%v", s.issues.creates, s.commands)
	}
}

// tooBigEdit changes four files, one more than the shipped graph allows.
func tooBigEdit(t *testing.T) func(string) {
	return func(w string) {
		for _, name := range []string{"a.go", "b.go", "c.go", "a_test.go"} {
			mustProgress(t, os.WriteFile(filepath.Join(w, name), []byte("package a\n"), 0o600))
		}
	}
}

const spikeFixerTooBig = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Written.","payload":{"spike":{"request":"Why does staging fail?","work_done":["Edited"],"findings":["Wrong flag"],"fix_report":{"defect":"Wrong flag","files":["a.go","b.go","c.go","a_test.go"],"regression_test":"a_test.go","summary":"Corrected the flag."}}}}}`

// dropNodes removes nodes, their declarations and every edge touching them.
func dropNodes(dot string, names ...string) string {
	var kept []string
	for _, line := range strings.Split(dot, "\n") {
		trimmed := strings.TrimSpace(line)
		drop := false
		for _, name := range names {
			if strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"\t") || strings.Contains(trimmed, "-> "+name+" ") || strings.HasPrefix(trimmed, name+" ->") {
				drop = true
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

var fixNodes = []string{"fix_agent", "fix_wants_owner", "fix_check", "note_fixed", "note_fix_unverified", "note_fix_too_big", "note_fix_blocked", "issue_too_big", "report_fix"}

func TestSpikeFixDotSwapsChangeThePathWithoutGoChange(t *testing.T) {
	t.Run("small routed to the proposal note never runs the fixer", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.graph = func(dot string) string {
				dot = dropNodes(dot, fixNodes...)
				add := ` note_fix_proposed [lf_action="lf_spike_note", lf_status="fix_proposed", lf_outcomes="success,failed", lf_source="probe_agent", lf_limit="1"];
 size_gate -> note_fix_proposed [lf_on="small"];
 note_fix_proposed -> report_final [lf_on="success"];
 note_fix_proposed -> report_failure [lf_on="failed"];
}`
				return strings.TrimSuffix(strings.TrimSpace(dot), "}") + add
			}
		})
		reports := s.reports()
		if s.ran("fix_agent") != 0 || !strings.Contains(s.note(), "Status: `fix_proposed`") || len(reports) != 1 || len(s.commands) != 0 {
			t.Fatalf("stages=%v note=%q reports=%v", s.stages, s.note(), reports)
		}
	})
	t.Run("naming only the full phase runs only it", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.graph = func(dot string) string {
				return strings.Replace(dot, `lf_phases="focused,full"`, `lf_phases="full"`, 1)
			}
		})
		if fmt.Sprint(s.commands) != "[[check-full ./...]]" || !strings.Contains(s.note(), "Status: `fixed`") {
			t.Fatalf("commands=%v", s.commands)
		}
	})
	t.Run("routing the oversized note to the report files no issue", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.edit, s.fixerResult = tooBigEdit(t), spikeFixerTooBig
			s.graph = func(dot string) string {
				dot = strings.Replace(dot, "note_fix_too_big    -> issue_too_big       [lf_on=\"success\"];", "note_fix_too_big    -> report_fix          [lf_on=\"success\"];", 1)
				return dropNodes(dot, "issue_too_big")
			}
		})
		reports := s.reports()
		if s.issues.creates != 0 || len(reports) != 1 || !strings.Contains(s.note(), "Status: `fix_too_big`") || strings.Contains(reports[0], "Follow-up issue") {
			t.Fatalf("creates=%d note=%q reports=%v", s.issues.creates, s.note(), reports)
		}
	})
	t.Run("a note that claims a fix the check did not pass is refused", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.start(func(s *spikeFix) {
			s.command = func([]string) bugchecks.CommandResult {
				one := 1
				return bugchecks.CommandResult{Outcome: "failed", Exit: &one}
			}
			s.graph = func(dot string) string {
				dot = strings.Replace(dot, "fix_check           -> note_fix_unverified [lf_on=\"failed\"];", "fix_check           -> note_fixed          [lf_on=\"failed\"];", 1)
				return dropNodes(dot, "note_fix_unverified")
			}
		})
		out, err := s.dispatchOwed()
		if err == nil || out.Run.Status != store.RunFailed {
			t.Fatalf("a fixed note over a failed check was written: %v", err)
		}
		reports := s.reports()
		if len(reports) != 1 || strings.Contains(reports[0], "`fixed`") || strings.Contains(s.note(), "Status: `fixed`") {
			t.Fatalf("success was claimed: note=%q reports=%v", s.note(), reports)
		}
	})
}

// dispatchOwed runs the next owed stage once and returns its outcome and error.
func (s *spikeFix) dispatchOwed() (Result, error) {
	t := s.t
	t.Helper()
	due, err := s.f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Minute), 8)
	mustProgress(t, err)
	if len(due) == 0 {
		t.Fatal("no stage is owed")
	}
	reserved, ok, err := s.f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
	mustProgress(t, err)
	if !ok {
		t.Fatal("dispatch not reserved")
	}
	req := s.req
	req.Trigger = Trigger{CommentID: reserved.TriggerID(), Body: "Continue the workflow at its " + reserved.Node + " stage.", Author: s.req.Trigger.Author}
	req.Command = Command{Kind: KindFreeText, Text: req.Trigger.Body}
	return Execute(t.Context(), s.f.opts, req)
}

func TestSpikeFixCheckBlockedByRunnerSetupTimeoutAndCancellation(t *testing.T) {
	for name, outcome := range map[string]bugchecks.CommandResult{
		"sandbox setup failure": {Outcome: "blocked", Reason: "sandbox binary is missing"},
		"timeout":               {Outcome: "blocked", Reason: "check timed out"},
		"infrastructure":        {Outcome: "blocked", Reason: "check infrastructure failure or signal"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &spikeFix{t: t}
			s.run(func(s *spikeFix) { s.command = func([]string) bugchecks.CommandResult { return outcome } })
			reports := s.reports()
			if !strings.Contains(s.note(), "Status: `blocked`") || len(reports) != 1 || strings.Contains(reports[0], "`fixed`") || !strings.Contains(reports[0], outcome.Reason) {
				t.Fatalf("note=%q reports=%v", s.note(), reports)
			}
			if len(s.commands) != 1 {
				t.Fatalf("a blocked command did not stop the phases: %v", s.commands)
			}
		})
	}
	t.Run("cancellation is not an outcome", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.start(func(s *spikeFix) {
			s.command = func([]string) bugchecks.CommandResult {
				return bugchecks.CommandResult{Outcome: "cancelled", Reason: "context canceled"}
			}
		})
		out, err := s.dispatchOwed()
		if err == nil || out.Run.Status != store.RunFailed || strings.Contains(s.note(), "Status:") {
			t.Fatalf("cancellation was recorded as a check outcome: %v", err)
		}
	})
}

func TestSpikeFixRouteRefusesAFinishedFixWithoutAValidReport(t *testing.T) {
	for name, refused := range map[string]string{
		"missing report": strings.Replace(spikeFixerDone, `,"fix_report":{"defect":"Wrong flag","files":["a.go","a_test.go"],"regression_test":"a_test.go","change_kinds":[],"summary":"Corrected the flag and tested it."}`, "", 1),
		"test outside":   strings.Replace(spikeFixerDone, `"regression_test":"a_test.go"`, `"regression_test":"b_test.go"`, 1),
		"a question":     spikeAskJSON,
	} {
		t.Run(name, func(t *testing.T) {
			s := &spikeFix{t: t}
			s.run(func(s *spikeFix) { s.refused = refused })
			if !strings.Contains(s.note(), "Status: `fixed`") {
				t.Fatalf("the corrected result did not complete the fix path: %q", s.note())
			}
		})
	}
}

func seedIssueRecord(s *spikeFix, state string, mutate func(*spikeIssueRecord)) func(string) {
	base := tooBigEdit(s.t)
	return func(w string) {
		base(w)
		t := s.t
		instance := spikeInstance(t, s.f)
		attempt, err := s.f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "fix_agent")
		mustProgress(t, err)
		record := spikeIssueRecord{State: state, SourceAttemptID: attempt.ID, Marker: spikeIssueMarker(instance.SessionID, "issue_too_big"), Detail: "seeded"}
		if mutate != nil {
			mutate(&record)
		}
		raw, err := json.Marshal(record)
		mustProgress(t, err)
		mustProgress(t, s.f.db.PutSpikeRecord(t.Context(), store.SpikeRecord{InstanceID: instance.ID, Node: "issue_too_big", Kind: store.SpikeRecordIssue, Payload: raw, UpdatedAt: time.Now()}))
	}
}

func TestSpikeFixIssueReplayRecoversByMarkerAndNeverRecreates(t *testing.T) {
	remote := func(s *spikeFix, count int) {
		for i := range count {
			instance := spikeInstance(t, s.f)
			s.issues.issues = append(s.issues.issues, github.Issue{ID: int64(70 + i), Number: 70 + i, Body: spikeIssueMarker(instance.SessionID, "issue_too_big") + "\nbody", State: github.StateOpen, Author: github.User{ID: 9999}, HTMLURL: fmt.Sprintf("https://forge.invalid/i/%d", 70+i)})
		}
	}
	t.Run("interrupted after the forge created it", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.fixerResult = spikeFixerTooBig
			inner := seedIssueRecord(s, spikeIssueIntent, nil)
			s.edit = func(w string) { inner(w); remote(s, 1) }
		})
		reports := s.reports()
		if s.issues.creates != 0 || len(reports) != 1 || !strings.Contains(reports[0], "https://forge.invalid/i/70") {
			t.Fatalf("creates=%d reports=%v", s.issues.creates, reports)
		}
	})
	t.Run("intent with no marker match is unconfirmed", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.fixerResult, s.edit = spikeFixerTooBig, seedIssueRecord(s, spikeIssueUncertain, nil)
		})
		reports := s.reports()
		if s.issues.creates != 0 || len(reports) != 1 || !strings.Contains(reports[0], "could not be confirmed") {
			t.Fatalf("creates=%d reports=%v", s.issues.creates, reports)
		}
		record, err := s.f.db.GetSpikeRecord(t.Context(), spikeInstance(t, s.f).ID, "issue_too_big", store.SpikeRecordIssue)
		mustProgress(t, err)
		if !strings.Contains(string(record.Payload), `"state":"uncertain"`) {
			t.Fatalf("the uncertain state was lost: %s", record.Payload)
		}
	})
	t.Run("two marker matches are not guessed between", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.fixerResult = spikeFixerTooBig
			inner := seedIssueRecord(s, spikeIssueIntent, nil)
			s.edit = func(w string) { inner(w); remote(s, 2) }
		})
		if s.issues.creates != 0 || !strings.Contains(strings.Join(s.reports(), "\n"), "could not be confirmed") {
			t.Fatalf("creates=%d reports=%v", s.issues.creates, s.reports())
		}
	})
	t.Run("a confirmed filing is never redone", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.fixerResult = spikeFixerTooBig
			s.edit = seedIssueRecord(s, spikeIssueFiled, func(r *spikeIssueRecord) { r.Number, r.URL = 42, "https://forge.invalid/i/42" })
		})
		if s.issues.creates != 0 || !strings.Contains(strings.Join(s.reports(), "\n"), "https://forge.invalid/i/42") {
			t.Fatalf("creates=%d reports=%v", s.issues.creates, s.reports())
		}
	})
	t.Run("a terminal failure is kept", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) { s.fixerResult, s.edit = spikeFixerTooBig, seedIssueRecord(s, spikeIssueFailed, nil) })
		if s.issues.creates != 0 || !strings.Contains(strings.Join(s.reports(), "\n"), "could not be confirmed") {
			t.Fatalf("creates=%d reports=%v", s.issues.creates, s.reports())
		}
	})
	t.Run("a record of another result is not evidence", func(t *testing.T) {
		s := &spikeFix{t: t}
		s.run(func(s *spikeFix) {
			s.fixerResult = spikeFixerTooBig
			s.edit = seedIssueRecord(s, spikeIssueFiled, func(r *spikeIssueRecord) { r.SourceAttemptID, r.URL, r.Number = 999999, "https://forge.invalid/i/1", 1 })
		})
		if s.issues.creates != 1 || strings.Contains(strings.Join(s.reports(), "\n"), "https://forge.invalid/i/1)") {
			t.Fatalf("creates=%d reports=%v", s.issues.creates, s.reports())
		}
	})
	t.Run("intent is durable before the forge is called", func(t *testing.T) {
		s := &spikeFix{t: t}
		var seen string
		s.run(func(s *spikeFix) {
			s.fixerResult, s.edit = spikeFixerTooBig, tooBigEdit(t)
			s.issues.onCreate = func(string) {
				raw, err := s.f.db.LatestSpikeRecord(t.Context(), spikeInstance(t, s.f).ID, store.SpikeRecordIssue)
				mustProgress(t, err)
				seen = string(raw.Payload)
			}
		})
		if !strings.Contains(seen, `"state":"intent"`) || !strings.Contains(seen, "lf:spike-issue session=") {
			t.Fatalf("no intent with a marker was retained before the call: %s", seen)
		}
	})
}

func TestSpikeFixCheckReplayIsSafeAndDoesNotRestartTheFixer(t *testing.T) {
	s := &spikeFix{t: t}
	var replayed ActivityResult
	var replayErr error
	nested := false
	s.start(func(s *spikeFix) {
		inner := func(argv []string) bugchecks.CommandResult {
			zero := 0
			return bugchecks.CommandResult{Outcome: "passed", Exit: &zero, Output: "ok"}
		}
		s.command = func(argv []string) bugchecks.CommandResult {
			if !nested {
				nested = true
				// The same running attempt is executed a second time, as a restart
				// would: the check measures and runs again from its saved baseline.
				replayed, replayErr = s.replayCheck()
			}
			return inner(argv)
		}
	})
	s.continueOwed()
	if replayErr != nil || replayed.Outcome != "passed" {
		t.Fatalf("the replayed check: %+v %v", replayed, replayErr)
	}
	if s.ran("fix_agent") != 1 || s.ran("probe_agent") != 1 {
		t.Fatalf("a replayed check restarted an agent: %v", s.stages)
	}
	records := 0
	instance := spikeInstance(t, s.f)
	if _, err := s.f.db.GetSpikeRecord(t.Context(), instance.ID, "fix_check", store.SpikeRecordCheck); err == nil {
		records++
	}
	if records != 1 || !strings.Contains(s.note(), "Status: `fixed`") || len(s.reports()) != 1 {
		t.Fatalf("records=%d note=%q reports=%v", records, s.note(), s.reports())
	}
}

// replayCheck executes the running check node's activity again.
func (s *spikeFix) replayCheck() (ActivityResult, error) {
	t := s.t
	instance := spikeInstance(t, s.f)
	attempt, err := s.f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "fix_check")
	mustProgress(t, err)
	policy := SpikeWorkflowPolicy()
	definition, err := captureSpike(t.Context(), policy)
	mustProgress(t, err)
	session, err := s.f.db.GetSession(t.Context(), s.f.session.ID)
	mustProgress(t, err)
	clone, err := s.f.opts.Git.Open(t.Context(), s.f.repo.Dir)
	mustProgress(t, err)
	ensured, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: session.WorktreePath, Branch: session.Branch, BaseBranch: gittest.DefaultBranch})
	mustProgress(t, err)
	activity := SpikeCheckActivity{
		Profile: definition.Profile,
		Context: func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error) {
			c := policy.baseContext(true)
			c.WriteAccess = true
			return c, nil
		},
		Destinations: func() (string, string, error) { return session.WorktreePath, ".aiteam/replay", nil },
		Worktree:     func() (*git.Worktree, error) { return ensured.Worktree, nil },
	}
	return activity.Execute(t.Context(), s.f.opts, ActivityRequest{Mode: agent.ModeSpike, Node: "fix_check", InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: attempt.RunID})
}
