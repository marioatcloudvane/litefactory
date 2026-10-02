package turn

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func fix(defects, files int, kinds []string, test string) *agentresult.SpikeProposedFix {
	f := &agentresult.SpikeProposedFix{ChangeKinds: kinds, RegressionTest: test}
	for range defects {
		f.Defects = append(f.Defects, "defect")
	}
	for i := range files {
		f.Files = append(f.Files, "f"+string(rune('a'+i))+".go")
	}
	return f
}

func TestSpikeGateTable(t *testing.T) {
	forbid := []string{"package", "dependency", "schema", "api"}
	for name, c := range map[string]struct {
		fix   *agentresult.SpikeProposedFix
		max   int
		forbi []string
		want  string
	}{
		"no proposal":                {nil, 3, forbid, "none"},
		"one defect three files":     {fix(1, 3, []string{"code", "test"}, "TestX"), 3, forbid, "small"},
		"one defect one file":        {fix(1, 1, []string{"code"}, "TestX"), 1, forbid, "small"},
		"four files":                 {fix(1, 4, []string{"code", "test"}, "TestX"), 3, forbid, "too_big"},
		"limit is declared":          {fix(1, 3, []string{"code", "test"}, "TestX"), 2, forbid, "too_big"},
		"limit of five":              {fix(1, 5, []string{"code", "test"}, "TestX"), 5, forbid, "small"},
		"two defects":                {fix(2, 1, []string{"code"}, "TestX"), 3, forbid, "too_big"},
		"new package":                {fix(1, 2, []string{"code", "package"}, "TestX"), 3, forbid, "too_big"},
		"new dependency":             {fix(1, 2, []string{"dependency"}, "TestX"), 3, forbid, "too_big"},
		"schema change":              {fix(1, 2, []string{"schema"}, "TestX"), 3, forbid, "too_big"},
		"api change":                 {fix(1, 2, []string{"api"}, "TestX"), 3, forbid, "too_big"},
		"forbid list is declared":    {fix(1, 2, []string{"api"}, "TestX"), 3, []string{"schema"}, "small"},
		"empty forbid allows kinds":  {fix(1, 2, []string{"schema"}, "TestX"), 3, nil, "small"},
		"no defects":                 {fix(0, 2, []string{"code"}, "TestX"), 3, forbid, "blocked"},
		"no files":                   {fix(1, 0, []string{"code"}, "TestX"), 3, forbid, "blocked"},
		"no kinds":                   {fix(1, 1, nil, "TestX"), 3, forbid, "blocked"},
		"no regression test":         {fix(1, 1, []string{"code"}, "  "), 3, forbid, "blocked"},
		"incomplete beats too large": {fix(2, 9, nil, "TestX"), 3, forbid, "blocked"},
	} {
		t.Run(name, func(t *testing.T) {
			got := SpikeGate(c.fix, c.max, c.forbi)
			if got != c.want {
				t.Fatalf("SpikeGate = %q, want %q", got, c.want)
			}
			if !slices.Contains(SpikeGateOutcomes, got) {
				t.Fatalf("outcome %q is not declared", got)
			}
		})
	}
}

func TestSpikeGateLimitsAreReadFromTheNode(t *testing.T) {
	limit, forbid, err := DecodeSpikeGateLimits(map[string]string{"lf_max_files": "2", "lf_forbid": "api, schema"})
	if err != nil || limit != 2 || strings.Join(forbid, ",") != "api,schema" {
		t.Fatalf("limits: %d %v %v", limit, forbid, err)
	}
	for _, attrs := range []map[string]string{
		{"lf_max_files": "0", "lf_forbid": "api"},
		{"lf_max_files": "6", "lf_forbid": "api"},
		{"lf_max_files": "three", "lf_forbid": "api"},
		{"lf_max_files": "3", "lf_forbid": "code"},
		{"lf_max_files": "3", "lf_forbid": ""},
	} {
		if _, _, err := DecodeSpikeGateLimits(attrs); err == nil {
			t.Fatalf("accepted %v", attrs)
		}
	}
}

func TestSpikeNoteRendersEveryStatusFromTheResult(t *testing.T) {
	result := agentresult.Result{Outcome: "completed", Comment: "c", Recommendation: &agentresult.Recommendation{Text: "Take this to Research."}, Payload: agentresult.Payload{Spike: &agentresult.Spike{
		Request: "Why?", WorkDone: []string{"read"}, Findings: []string{"found"}, Decisions: []string{"chose"}, NotDone: []string{"skipped"},
		ProposedFix: &agentresult.SpikeProposedFix{Defects: []string{"d"}, Files: []string{"a.go"}, ChangeKinds: []string{"code"}, RegressionTest: "TestA"},
	}}}
	for _, status := range agentresult.SpikeStatuses {
		note := renderSpikeNote(status, result, nil)
		for _, want := range []string{"Status: `" + status + "`", "## Request", "Why?", "read", "found", "chose", "skipped", "TestA", "Take this to Research."} {
			if !strings.Contains(note, want) {
				t.Fatalf("%s note lacks %q:\n%s", status, want, note)
			}
		}
	}
	empty := renderSpikeNote("answered", agentresult.Result{Payload: agentresult.Payload{Spike: &agentresult.Spike{}}}, nil)
	if !strings.Contains(empty, "_The request was not recorded._") || !strings.Contains(empty, "_None._") {
		t.Fatalf("empty note: %s", empty)
	}
}

func TestSpikeNoteWriterConfinesTheDestination(t *testing.T) {
	root := t.TempDir()
	if err := writeSessionDocument(root, ".aiteam/b/d/s", SpikeNoteName, "one\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionDocument(root, ".aiteam/b/d/s", SpikeNoteName, "two\n"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".aiteam/b/d/s", SpikeNoteName))
	if err != nil || string(raw) != "two\n" {
		t.Fatalf("note not replaced in place: %q %v", raw, err)
	}
	for _, bad := range []string{"../escape", "/abs", ".."} {
		if err := writeSessionDocument(root, bad, SpikeNoteName, "x"); err == nil {
			t.Fatalf("accepted session %q", bad)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionDocument(root, "link/s", SpikeNoteName, "x"); err == nil {
		t.Fatal("wrote through a symlinked ancestor")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("escaped the worktree")
	}
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(root, ".aiteam/b/d/s/LINK.md")); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionDocument(root, ".aiteam/b/d/s", "LINK.md", "x"); err == nil {
		t.Fatal("wrote through a symlinked file")
	}
}

func TestSpikeReportRequiresARecordedNoteForExactlyThisResult(t *testing.T) {
	f, _, out, err := runSpikeOnce(t, spikeDoneNone, nil)
	mustProgress(t, err)
	_ = out
	instance := spikeInstance(t, f)
	source, err := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "probe_agent")
	mustProgress(t, err)
	scope := SpikeWorkflowPolicy().baseContext(true)
	node, err := SpikeWorkflowPolicy().load(t.Context(), f.db, source.ID, scope)
	mustProgress(t, err)
	annotated, err := annotateSpike(t.Context(), f.db, source, "final", node.Result)
	mustProgress(t, err)
	if annotated.Payload.Spike.Status != "answered" {
		t.Fatalf("status = %q", annotated.Payload.Spike.Status)
	}
	if node.Result.Payload.Spike.Status != "" {
		t.Fatal("annotation mutated the retained result")
	}
	changed := node.Result
	spike := *changed.Payload.Spike
	spike.Findings = []string{"a different finding"}
	changed.Payload.Spike = &spike
	if _, err := annotateSpike(t.Context(), f.db, source, "final", changed); err == nil {
		t.Fatal("a report for a result no note recorded was allowed")
	}
	if _, err := annotateSpike(t.Context(), f.db, source, "question", node.Result); err == nil {
		t.Fatal("a question report was allowed over an answered note")
	}
}

func TestSpikeCommentContractRefusesUnrecordedOrAuthoredStatus(t *testing.T) {
	scope := SpikeWorkflowPolicy().baseContext(true)
	source := store.GraphNodeAttempt{Status: store.GraphSucceeded}
	done := agentresult.Result{Outcome: "completed", Comment: "ok", Payload: agentresult.Payload{Spike: &agentresult.Spike{Request: "r", Status: "answered"}}}
	if err := validateSpikeComment(source, "final", done, scope); err != nil {
		t.Fatal(err)
	}
	unrecorded := done
	unrecorded.Payload.Spike = &agentresult.Spike{Request: "r"}
	if err := validateSpikeComment(source, "final", unrecorded, scope); err == nil {
		t.Fatal("a report without a recorded status was allowed")
	}
	asking := done
	asking.Payload.Spike = &agentresult.Spike{Request: "r", Status: "asking"}
	if err := validateSpikeComment(source, "final", asking, scope); err == nil {
		t.Fatal("a final report over an asking note was allowed")
	}
	// An authored result can never carry a status: the shared validator refuses it.
	if d := agentresult.ValidateResult(done, scope); d.Valid {
		t.Fatal("an authored status passed validation")
	}
}

func TestSpikeFailureCandidateClaimsNothing(t *testing.T) {
	c := spikeFailureCandidate(Request{}, context.DeadlineExceeded)
	if c.Outcome == "completed" || strings.TrimSpace(c.Reason) == "" || len(c.Questions) != 0 || c.Payload.Spike == nil {
		t.Fatalf("failure candidate: %+v", c)
	}
	scope := SpikeWorkflowPolicy().baseContext(true)
	if err := validateSpikeComment(store.GraphNodeAttempt{Status: store.GraphFailed}, "failure", c, scope); err != nil {
		t.Fatal(err)
	}
	if text := renderSpikeComment(c, "", naming.Ref{}, ""); !strings.Contains(text, "did not finish") {
		t.Fatalf("failure text: %q", text)
	}
}

func TestNodeAccessMapsTheDeclaredLevelToATightenedProfile(t *testing.T) {
	spike := SpikeWorkflowPolicy()
	research := ResearchWorkflowPolicy()
	claude := Request{Agent: agent.Kind(store.AgentClaude), Tools: []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"}, AllowedTools: []string{"Read", "Edit", "Bash(go test:*)"}}
	codex := Request{Agent: agent.Kind(store.AgentCodex), CodexSandbox: "workspace-write"}
	node := func(access string) graphexec.Node {
		n := graphexec.Node{Name: "n", Attrs: map[string]string{}}
		if access != "" {
			n.Attrs[graphexec.AccessAttribute] = access
		}
		return n
	}
	for _, c := range []struct {
		name   string
		policy WorkflowPolicy
		access string
		req    Request
		tools  string
		allow  string
		sandbx string
	}{
		{"spike absent is read (claude)", spike, "", claude, "Read,Glob,Grep", "Read", ""},
		{"spike read (claude)", spike, "read", claude, "Read,Glob,Grep", "Read", ""},
		{"spike write maps to the fixer tools without a shell", spike, "write", claude, "Read,Glob,Grep,Edit,Write", "Read,Edit", ""},
		{"spike absent is read (codex)", spike, "", codex, "", "", "read-only"},
		{"spike write uses the workspace sandbox", spike, "write", codex, "", "", "workspace-write"},
		{"research absent is unchanged", research, "", claude, "Read,Glob,Grep,Edit,Write,Bash,Task", "Read,Edit,Bash(go test:*)", ""},
		{"research absent is unchanged (codex)", research, "", codex, "", "", "workspace-write"},
		{"research explicit read narrows", research, "read", claude, "Read,Glob,Grep", "Read", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := applyNodeAccess(c.policy, node(c.access), c.req)
			if err != nil {
				t.Fatal(err)
			}
			want := c.sandbx
			if want == "" {
				want = c.req.CodexSandbox
			}
			if strings.Join(got.Tools, ",") != c.tools || strings.Join(got.AllowedTools, ",") != c.allow || got.CodexSandbox != want {
				t.Fatalf("got tools=%v allowed=%v sandbox=%q", got.Tools, got.AllowedTools, got.CodexSandbox)
			}
		})
	}
	if _, err := applyNodeAccess(spike, node("read"), Request{Agent: agent.Kind(store.AgentClaude), Tools: []string{"Bash"}}); err == nil {
		t.Fatal("read access was mapped onto a profile that grants none of it")
	}
	if _, err := applyNodeAccess(spike, node("admin"), claude); err == nil {
		t.Fatal("an undeclared access level was accepted")
	}
	if narrowed, err := applyNodeAccess(spike, node("read"), Request{Agent: agent.Kind(store.AgentClaude)}); err != nil || strings.Join(narrowed.Tools, ",") != "Read,Glob,Grep" {
		t.Fatalf("an unset surface must still narrow: %v %v", narrowed.Tools, err)
	}
	mutated := claude
	if _, err := applyNodeAccess(spike, node("read"), mutated); err != nil || len(claude.Tools) != 7 {
		t.Fatal("the resolved profile was mutated in place")
	}
}
