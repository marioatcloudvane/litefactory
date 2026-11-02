package turn

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func vsEffect(id int64, kind, target, state string, result any) *store.ValidateEffect {
	e := &store.ValidateEffect{ID: id, Kind: kind, Target: target, State: state}
	if result != nil {
		e.Result, _ = json.Marshal(result)
	}
	return e
}

func vsBase(findings ...ValidateFinding) ValidateState {
	return ValidateState{
		InstanceID: 1,
		Commit:     strings.Repeat("c", 40),
		Suite:      validateSuiteOutput{Outcome: store.SuitePassed, Commit: strings.Repeat("c", 40)},
		Assessed:   true,
		Findings:   findings,
		Effects:    ValidateDeliveryRows{Issues: map[string]store.ValidateEffect{}},
	}
}

func vsIssueChoice(f ValidateFinding) ValidateFinding {
	f.Choice = &store.ValidateChoice{Choice: store.ChoiceIssue, IssueKind: "bug", CommentID: 5}
	return f
}

func TestValidateReadiness(t *testing.T) {
	sat := ValidateFinding{ID: "s:ac1", State: findingSatisfied, Criterion: true, Blocking: true}
	awaiting := ValidateFinding{ID: "s:ac2", State: findingAwaitingCheck, Criterion: true, Blocking: true}
	passed := ValidateFinding{ID: "s:ac3", State: findingOwnerPassed, Criterion: true, Blocking: true}
	blocking := ValidateFinding{ID: "s:code", State: findingOpen, Blocking: true}
	optional := ValidateFinding{ID: "s:arch", State: findingOpen, Optional: true}
	issueEffect := func(state string) map[string]store.ValidateEffect {
		return map[string]store.ValidateEffect{"s:arch": {Kind: store.EffectIssue, Target: "s:arch", State: state}}
	}
	cases := []struct {
		name   string
		mutate func(*ValidateState)
		ready  bool
	}{
		{"suite passed, all satisfied", func(s *ValidateState) { s.Findings = []ValidateFinding{sat, passed} }, true},
		{"no findings", func(s *ValidateState) {}, true},
		{"suite failed", func(s *ValidateState) { s.Suite.Outcome = store.SuiteFailed }, false},
		{"not assessed", func(s *ValidateState) { s.Assessed = false }, false},
		{"criterion awaiting check", func(s *ValidateState) { s.Findings = []ValidateFinding{sat, awaiting} }, false},
		{"blocking open no choice", func(s *ValidateState) { s.Findings = []ValidateFinding{blocking} }, false},
		{"blocking open with repair", func(s *ValidateState) {
			f := blocking
			f.Choice = &store.ValidateChoice{Choice: store.ChoiceRepair}
			s.Findings = []ValidateFinding{f}
		}, false},
		{"blocking open with succeeded issue", func(s *ValidateState) {
			f := vsIssueChoice(blocking)
			s.Findings = []ValidateFinding{f}
			s.Effects.Issues = map[string]store.ValidateEffect{"s:code": {State: store.EffectSucceeded}}
		}, false},
		{"optional without choice", func(s *ValidateState) { s.Findings = []ValidateFinding{optional} }, false},
		{"optional deferred", func(s *ValidateState) {
			f := optional
			f.Choice = &store.ValidateChoice{Choice: store.ChoiceDefer}
			s.Findings = []ValidateFinding{f}
		}, true},
		{"optional repair", func(s *ValidateState) {
			f := optional
			f.Choice = &store.ValidateChoice{Choice: store.ChoiceRepair}
			s.Findings = []ValidateFinding{f}
		}, true},
		{"issue effect missing", func(s *ValidateState) { s.Findings = []ValidateFinding{vsIssueChoice(optional)} }, false},
		{"issue effect intent", func(s *ValidateState) {
			s.Findings = []ValidateFinding{vsIssueChoice(optional)}
			s.Effects.Issues = issueEffect(store.EffectIntent)
		}, false},
		{"issue effect failed", func(s *ValidateState) {
			s.Findings = []ValidateFinding{vsIssueChoice(optional)}
			s.Effects.Issues = issueEffect(store.EffectFailed)
		}, false},
		{"issue effect uncertain", func(s *ValidateState) {
			s.Findings = []ValidateFinding{vsIssueChoice(optional)}
			s.Effects.Issues = issueEffect(store.EffectUncertain)
		}, false},
		{"issue effect succeeded", func(s *ValidateState) {
			s.Findings = []ValidateFinding{vsIssueChoice(optional)}
			s.Effects.Issues = issueEffect(store.EffectSucceeded)
		}, true},
	}
	for _, tc := range cases {
		s := vsBase()
		tc.mutate(&s)
		s.computeReadiness()
		if s.Ready != tc.ready {
			t.Errorf("%s: Ready=%v want %v (open=%v)", tc.name, s.Ready, tc.ready, s.Open)
		}
		if s.Ready != (len(s.Open) == 0) {
			t.Errorf("%s: Ready disagrees with Open %v", tc.name, s.Open)
		}
	}
}

func TestValidateOpenReasonsNameFinding(t *testing.T) {
	s := vsBase(
		ValidateFinding{ID: "s:ac2", State: findingAwaitingCheck, Blocking: true},
		ValidateFinding{ID: "s:code", State: findingOpen, Blocking: true},
		ValidateFinding{ID: "s:arch", State: findingOpen, Optional: true},
	)
	s.computeReadiness()
	joined := strings.Join(s.Open, "\n")
	for _, id := range []string{"s:ac2", "s:code", "s:arch"} {
		if !strings.Contains(joined, id) {
			t.Errorf("open reasons do not mention %s: %v", id, s.Open)
		}
	}
}

func TestValidateRevision(t *testing.T) {
	mk := func() ValidateState {
		f := ValidateFinding{ID: "s:arch", Status: "open", State: findingOpen, Optional: true}
		return vsBase(f)
	}
	base := mk().revision()
	if len(base) != 12 || !validateRevisionPattern.MatchString(base) {
		t.Fatalf("revision %q is not 12 hex", base)
	}
	if mk().revision() != base {
		t.Error("revision is not stable")
	}
	changes := map[string]func(*ValidateState){
		"verdict": func(s *ValidateState) {
			s.Findings[0].Verdict = &store.ValidateVerdict{Verdict: store.VerdictPass, CommentID: 9}
		},
		"choice": func(s *ValidateState) {
			s.Findings[0].Choice = &store.ValidateChoice{Choice: store.ChoiceDefer, CommentID: 9}
		},
		"finding state": func(s *ValidateState) { s.Findings[0].State = findingOwnerPassed },
		"suite":         func(s *ValidateState) { s.Suite.Outcome = store.SuiteFailed },
		"commit":        func(s *ValidateState) { s.Commit = strings.Repeat("d", 40) },
		"issue effect": func(s *ValidateState) {
			s.Effects.Issues["s:arch"] = store.ValidateEffect{ID: 1, Kind: store.EffectIssue, Target: "s:arch", State: store.EffectIntent}
		},
		"push effect": func(s *ValidateState) { s.Effects.Push = vsEffect(2, store.EffectPush, "", store.EffectIntent, nil) },
	}
	for name, mutate := range changes {
		s := mk()
		mutate(&s)
		if s.revision() == base {
			t.Errorf("revision did not change on %s", name)
		}
	}
	a, b := mk(), mk()
	a.Effects.Push = vsEffect(2, store.EffectPush, "", store.EffectIntent, nil)
	b.Effects.Push = vsEffect(2, store.EffectPush, "", store.EffectSucceeded, nil)
	if a.revision() == b.revision() {
		t.Error("revision did not change on effect state")
	}
}

func readyState() ValidateState {
	s := vsBase(ValidateFinding{ID: "s:ac1", State: findingSatisfied, Criterion: true, Blocking: true})
	s.Revision = "0123456789ab"
	s.computeReadiness()
	return s
}

func joinCmds(c []string) string { return strings.Join(c, "\n") }

func TestNextCommandsDelivery(t *testing.T) {
	head := strings.Repeat("e", 40)

	s := vsBase(ValidateFinding{ID: "s:ac2", State: findingAwaitingCheck, Blocking: true})
	s.Revision = "0123456789ab"
	s.computeReadiness()
	if got := joinCmds(s.NextCommands(nil)); strings.Contains(got, ValidatePushCommand) {
		t.Errorf("push offered while not ready: %s", got)
	}

	s = readyState()
	if got := joinCmds(s.NextCommands(nil)); !strings.Contains(got, ValidatePushCommand+" 0123456789ab") {
		t.Errorf("push not offered when ready: %s", got)
	}

	s.Effects.Push = vsEffect(1, store.EffectPush, "", store.EffectSucceeded, nil)
	s.Effects.PR = vsEffect(2, store.EffectPR, "", store.EffectSucceeded, map[string]any{"number": 42, "head": head})
	got := joinCmds(s.NextCommands(nil))
	want := ValidateMergeCommand + " 0123456789ab 42 " + head
	if !strings.Contains(got, want) || strings.Contains(got, ValidatePushCommand) {
		t.Errorf("want merge %q only, got %s", want, got)
	}

	s.Effects.Merge = vsEffect(3, store.EffectMerge, "", store.EffectSucceeded, nil)
	got = joinCmds(s.NextCommands([]int{9, 3, 5}))
	if !strings.Contains(got, ValidateCloseCommand+" 0123456789ab 3 5 9") || strings.Contains(got, ValidateMergeCommand) {
		t.Errorf("want sorted close, got %s", got)
	}

	s.Effects.Close = vsEffect(4, store.EffectClose, "", store.EffectSucceeded, nil)
	if got := s.NextCommands([]int{1}); len(got) != 0 {
		t.Errorf("commands after close: %v", got)
	}

	// push succeeded but PR not: push is offered again.
	s = readyState()
	s.Effects.Push = vsEffect(1, store.EffectPush, "", store.EffectSucceeded, nil)
	if got := joinCmds(s.NextCommands(nil)); !strings.Contains(got, ValidatePushCommand) {
		t.Errorf("push not offered with PR pending: %s", got)
	}
}

func TestNextCommandsDeferOnlyForOptional(t *testing.T) {
	s := vsBase(
		ValidateFinding{ID: "s:code", State: findingOpen, Blocking: true},
		ValidateFinding{ID: "s:arch", State: findingOpen, Optional: true},
	)
	s.Revision = "0123456789ab"
	s.computeReadiness()
	for _, line := range s.NextCommands(nil) {
		has := strings.Contains(line, "s:code")
		isArch := strings.Contains(line, "s:arch")
		deferred := strings.Contains(line, " defer")
		switch {
		case has && deferred:
			t.Errorf("defer offered for blocking finding: %s", line)
		case isArch && !deferred:
			t.Errorf("defer missing for optional finding: %s", line)
		case !has && !isArch:
			t.Errorf("unexpected line: %s", line)
		}
	}
	s = vsBase(ValidateFinding{ID: "s:ac2", State: findingAwaitingCheck, Blocking: true})
	s.Revision = "0123456789ab"
	got := joinCmds(s.NextCommands(nil))
	if !strings.Contains(got, ValidateCheckCommand+" 0123456789ab s:ac2 pass") || !strings.Contains(got, "s:ac2 fail") {
		t.Errorf("check commands missing: %s", got)
	}
}

// twoStories is a run with story a (all satisfied) and story b (one criterion
// awaiting the owner's check), each with a code and an architecture review.
func twoStories(extra ...ValidateFinding) ValidateState {
	fs := []ValidateFinding{
		{ID: "a:AC-001", Story: "a", Name: "AC-001", Criterion: true, Role: agent.ReviewRoleAcceptance, Blocking: true, State: findingSatisfied},
		{ID: "a:code", Story: "a", Name: "code", Role: agent.ReviewRoleCode, Optional: true, State: findingSatisfied},
		{ID: "a:architecture", Story: "a", Name: "architecture", Role: agent.ReviewRoleArchitecture, Optional: true, State: findingSatisfied},
		{ID: "b:AC-001", Story: "b", Name: "AC-001", Criterion: true, Role: agent.ReviewRoleAcceptance, Blocking: true, State: findingSatisfied},
		{ID: "b:AC-002", Story: "b", Name: "AC-002", Criterion: true, Role: agent.ReviewRoleAcceptance, Blocking: true, State: findingAwaitingCheck, Summary: "Open the page and look."},
		{ID: "b:architecture", Story: "b", Name: "architecture", Role: agent.ReviewRoleArchitecture, Optional: true, State: findingSatisfied},
	}
	st := vsBase(append(fs, extra...)...)
	st.Stories = []ValidateJoinedStory{{Story: "a"}, {Story: "b"}}
	st.Revision = "0123456789ab"
	st.computeReadiness()
	return st
}

var twoTargets = &ValidateDeliveryTarget{Parent: ValidateIssueRef{Number: 10}, Stories: []ValidateIssueRef{{Number: 11, Key: "a", Title: "Diagnosis"}, {Number: 12, Key: "b", Title: "Repair tools"}}}

// rowOf returns the table row of a story.
func rowOf(t *testing.T, out, key string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, " "+key+" ") {
			return line
		}
	}
	t.Fatalf("no row for %s:\n%s", key, out)
	return ""
}

func nextLines(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "**Next:** ") || strings.HasPrefix(line, "Next: ") {
			n++
		}
	}
	return n
}

// summaryOf is the report before its collapsed details.
func summaryOf(out string) string {
	summary, _, _ := strings.Cut(out, "<details>")
	return summary
}

// summaryContentLines counts the non-blank lines of the summary.
func summaryContentLines(out string) int {
	n := 0
	for _, line := range strings.Split(summaryOf(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// #1314: one header light, one light per story row (in the details), one Next
// line, the item's one command in the summary and each command once.
func TestRenderValidateReportOverview(t *testing.T) {
	st := twoStories()
	out := renderValidateReport(st, twoTargets, "stale revision given")
	if !strings.HasPrefix(out, lightYellow+" **Validate report**") {
		t.Errorf("yellow header:\n%s", out)
	}
	a, b := rowOf(t, out, "a"), rowOf(t, out, "b")
	if a != "| "+lightGreen+" a Diagnosis (#11) | 1 of 1 satisfied | accepted | accepted | — |" {
		t.Errorf("row a: %s", a)
	}
	if b != "| "+lightYellow+" b Repair tools (#12) | 1 of 2 satisfied · 1 awaiting owner check | — | accepted | 1 check on #12 |" {
		t.Errorf("row b: %s", b)
	}
	for _, row := range []string{a, b} {
		cells := strings.Split(row, "|")[2:]
		for _, light := range []string{lightGreen, lightYellow, lightRed} {
			if strings.Contains(strings.Join(cells, "|"), light) {
				t.Errorf("a cell carries a light: %s", row)
			}
		}
	}
	if strings.Count(out, lightGreen)+strings.Count(out, lightYellow)+strings.Count(out, lightRed) != 3 {
		t.Errorf("one header light and one per row:\n%s", out)
	}
	pass := "`" + ValidateCheckCommand + " 0123456789ab b:AC-002 pass -- <what you checked>`"
	fail := "`" + ValidateCheckCommand + " 0123456789ab b:AC-002 fail -- <what you saw>`"
	if nextLines(out) != 1 || !strings.Contains(summaryOf(out), pass) || strings.Count(out, pass) != 1 || strings.Count(out, fail) != 1 || strings.Contains(summaryOf(out), fail) {
		t.Errorf("the summary names the check once; its other command is in the details once:\n%s", out)
	}
	if n := summaryContentLines(out); n > validateSummaryLines {
		t.Errorf("summary has %d lines:\n%s", n, summaryOf(out))
	}
	if strings.Contains(summaryOf(out), "authorizes the existing ordered sequence") || strings.Count(out, validateShipConsequence) != 1 {
		t.Errorf("the authority wording is in the details, once:\n%s", out)
	}
	if !strings.Contains(summaryOf(out), "stale revision given") {
		t.Error("the notice is shown in the summary")
	}
	q := validateQuestionCandidate(st, nil, "")
	wait := renderValidateComment(q, "", naming.Ref{}, "")
	if nextLines(wait) != 1 || strings.Count(wait, pass) != 1 || strings.Contains(wait, "authorizes the existing ordered sequence") || strings.Contains(wait, "Nothing is pushed, merged or closed without owner authority") {
		t.Errorf("owner wait with one Next line and no authority boilerplate:\n%s", wait)
	}

	// A blocking finding turns row b and the header red.
	st = twoStories(ValidateFinding{ID: "b:code", Story: "b", Name: "code", Role: agent.ReviewRoleCode, Blocking: true, State: findingOpen})
	out = renderValidateReport(st, twoTargets, "")
	if !strings.HasPrefix(out, lightRed) || !strings.HasPrefix(rowOf(t, out, "b"), "| "+lightRed+" b") || !strings.HasPrefix(rowOf(t, out, "a"), "| "+lightGreen) {
		t.Errorf("blocking finding is red:\n%s", out)
	}
	if !strings.Contains(rowOf(t, out, "b"), "| 1 finding (blocking) |") {
		t.Errorf("code cell: %s", rowOf(t, out, "b"))
	}
	repair := "`" + ValidateFindingCommand + " 0123456789ab b:code repair`"
	if !strings.Contains(summaryOf(out), "`b:code`") || strings.Count(out, repair) != 1 {
		t.Errorf("the blocking finding has its own summary line and one command:\n%s", out)
	}
}

// #571 BC-003: a reviewer without a usable result: the rerun is the next step.
func TestRenderValidateReportNotReviewed(t *testing.T) {
	st := twoStories(ValidateFinding{ID: "b:code", Story: "b", Name: "code", Role: agent.ReviewRoleCode, State: findingNotReviewed})
	st.Reviews = []store.ValidateReview{{Role: agent.ReviewRoleCode, State: "failed", Reason: "read denied"}}
	st.computeReadiness()
	out := renderValidateReport(st, twoTargets, "")
	if !strings.Contains(rowOf(t, out, "b"), "| not reviewed: failed: read denied |") || !strings.HasPrefix(rowOf(t, out, "b"), "| "+lightRed) || !strings.HasPrefix(out, lightRed) {
		t.Errorf("not reviewed cell and red row:\n%s", out)
	}
	if nextLines(out) != 1 || !strings.Contains(out, "**Next:** Run the missing reviews again; the passed suite is reused. `"+ValidateStartCommand+"`") || strings.Count(out, "`"+ValidateStartCommand+"`") != 1 {
		t.Errorf("the retry is the Next line:\n%s", out)
	}
}

// #573, #1314: while delivery is unfinished and nothing blocks, the one next
// step is /validate-ship with its decision reference; its consequence is in
// the details once.
func TestRenderValidateReportDeliveryNextStep(t *testing.T) {
	head := strings.Repeat("e", 40)
	st := twoStories()
	st.Findings[4].State = findingOwnerPassed
	st.computeReadiness()
	if !st.Ready {
		t.Fatalf("ready: %v", st.Open)
	}
	ship := "`" + ValidateShipCommand + " " + st.shipReference() + " -- <reason>`"
	for i := range 4 {
		want := ship
		switch i {
		case 1:
			st.Effects.Push = vsEffect(1, store.EffectPush, "", store.EffectSucceeded, nil)
			st.Effects.PR = vsEffect(2, store.EffectPR, "", store.EffectSucceeded, map[string]any{"number": 42, "head": head})
		case 2:
			st.Effects.Merge = vsEffect(3, store.EffectMerge, "", store.EffectSucceeded, nil)
		case 3:
			st.Effects.Close = vsEffect(4, store.EffectClose, "", store.EffectSucceeded, nil)
			want = "Nothing. Delivery is complete."
		}
		out := renderValidateReport(st, twoTargets, "")
		_, next, _ := strings.Cut(out, "\n**Next:** ")
		next, _, _ = strings.Cut(next, "\n")
		if !strings.HasPrefix(out, lightGreen) || nextLines(out) != 1 || !strings.Contains(next, want) {
			t.Errorf("step %d wants %s:\n%s", i, want, out)
		}
		if i < 3 && (strings.Count(out, want) != 1 || strings.Count(out, validateShipConsequence) != 1 || strings.Contains(summaryOf(out), "ordered sequence")) {
			t.Errorf("step %d: ship once, its consequence once and only in the details:\n%s", i, out)
		}
		if i == 3 && strings.Contains(out, ValidateShipCommand) {
			t.Errorf("complete delivery offered a new decision:\n%s", out)
		}
	}
}

// #1314: a failed suite names what failed in the summary; the details keep
// the suite reason, the repair rounds and each failure's facts.
func TestRenderValidateReportFailedSuite(t *testing.T) {
	st := twoStories()
	st.Suite.Outcome, st.Suite.Reason = store.SuiteFailed, "go test ./... exited 1"
	st.Failures = []HandbackFailure{{Command: "go test ./...", Reason: "exited 1", OutputTail: "--- FAIL: TestTotal\n3 failed, 269 passed in 427.3s", SuiteRecord: "suite run 7, command 0"}}
	st.computeReadiness()
	out := renderValidateReport(st, twoTargets, "")
	if !strings.HasPrefix(out, lightRed) || strings.Contains(out, "| Story |") || !strings.Contains(out, "go test ./... exited 1") || nextLines(out) != 1 {
		t.Errorf("failed suite:\n%s", out)
	}
	summary := summaryOf(out)
	if !strings.Contains(summary, "**What failed:**") || !strings.Contains(summary, "`go test ./...`: TestTotal failed") || !strings.Contains(summary, "**Why:**") {
		t.Errorf("the summary names the failing command and why:\n%s", summary)
	}
	if !strings.Contains(out, "**Failures:**") || !strings.Contains(out, "269 passed in 427.3s") || !strings.Contains(out, "suite run 7, command 0") {
		t.Errorf("the details keep each failure's facts:\n%s", out)
	}
	// #704: the Next line names only steps the factory has.
	if !strings.Contains(out, "**Next:** Push a fix to the feature branch, then run Validate again") || strings.Contains(out, "hand it back") {
		t.Errorf("failed suite Next line:\n%s", out)
	}
}

// #704 c-next-blocking: an open blocking finding with nothing for the owner
// names a fix on the feature branch and the Validate start command.
func TestValidateNextStepBlockingFindingNamesRealSteps(t *testing.T) {
	f := ValidateFinding{ID: "s:code", State: findingOpen, Blocking: true, Choice: &store.ValidateChoice{Choice: store.ChoiceRepair}}
	s := vsBase(f)
	s.computeReadiness()
	got := s.NextStep(nil)
	if !strings.Contains(got, ValidateShipCommand) || s.Ready || len(s.Open) == 0 || strings.Contains(got, "hand it back") || strings.Contains(got, "Implement") {
		t.Errorf("blocking Next line: %q", got)
	}
}

func TestValidateOwnerPassedReviewPreservesOtherGates(t *testing.T) {
	f := ValidateFinding{ID: "s:architecture", Status: "unverified", Blocking: true, State: findingOwnerPassed}
	for _, gate := range []string{"suite", "missing review", "issue pending", "issue failed", "issue uncertain"} {
		t.Run(gate, func(t *testing.T) {
			s := vsBase(f)
			switch gate {
			case "suite":
				s.Suite.Outcome = store.SuiteFailed
			case "missing review":
				s.Findings = append(s.Findings, ValidateFinding{ID: "s:code", Role: agent.ReviewRoleCode, State: findingNotReviewed})
			default:
				s.Findings[0].Choice = &store.ValidateChoice{Choice: store.ChoiceIssue, IssueKind: "bug"}
				if gate != "issue pending" {
					s.Effects.Issues[f.ID] = store.ValidateEffect{State: strings.TrimPrefix(gate, "issue ")}
				}
			}
			s.computeReadiness()
			if s.Ready || len(s.Open) == 0 {
				t.Fatal("owner verdict bypassed another gate")
			}
		})
	}
}

// longFindingText is n bytes or more of whole sentences.
func longFindingText(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString("The organized inbox refuses the whole view when one source loses access, sentence ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(". ")
	}
	return strings.TrimSpace(b.String())
}

// #1123 c4: finding text within the bound is kept whole; longer text is cut at
// a sentence end and says it was shortened, never a bare "...".
func TestShortenFindingTextKeepsWholeSentences(t *testing.T) {
	short := longFindingText(1500)
	if got := shortenFindingText(short, validateFindingTextLimit); got != short {
		t.Fatalf("text within the bound changed: %q", got)
	}
	long := longFindingText(10000)
	got := shortenFindingText(long, validateFindingTextLimit)
	kept, ok := strings.CutSuffix(got, findingTextShortened)
	if !ok || len(kept) > validateFindingTextLimit || !strings.HasSuffix(kept, ".") || !strings.HasPrefix(long, kept) || strings.HasSuffix(got, "...") {
		t.Fatalf("not cut at a sentence end with the note (%d bytes): %q", len(kept), got[max(0, len(got)-120):])
	}
	if got := shortenFindingText(strings.Repeat("é", 3000), 4097); !strings.HasSuffix(got, findingTextShortened) || !utf8ValidPrefix(got) {
		t.Fatalf("an unbroken run is cut at the limit as valid UTF-8: %q", got[max(0, len(got)-40):])
	}
	text, _ := findingEvidence([]agent.EvidenceExplanation{{Summary: long}})
	if text != got && !strings.HasSuffix(text, findingTextShortened) {
		t.Fatalf("findingEvidence keeps up to the bound: %d bytes", len(text))
	}
	if text, _ := findingEvidence([]agent.EvidenceExplanation{{Summary: short}}); text != short {
		t.Fatalf("findingEvidence cut 1,500 bytes: %d", len(text))
	}
}

func utf8ValidPrefix(s string) bool { return strings.ToValidUTF8(s, "�") == s }

// #1127: a Validate question that carries options shows them under the question
// and marks the recommended one only; the question line is unchanged.
func TestValidateQuestionShowsItsOptionsAndRecommendation(t *testing.T) {
	candidate := agentresult.Result{Outcome: "incomplete", Comment: "Intro.", Reason: "Next step.", Questions: []agentresult.Question{{ID: "q1", Audience: "technical", Text: " Ship this commit? ", RecommendedOption: "b", Options: []agentresult.Option{{ID: "a", Text: "Ship now"}, {ID: "b", Text: "Check again"}}}}}
	text := renderValidateComment(candidate, "", naming.Ref{}, "")
	want := "- Ship this commit?\n  - `a` Ship now\n  - `b` Check again (recommended)\n"
	if !strings.Contains(text, want) || strings.Count(text, "(recommended)") != 1 {
		t.Fatalf("options or marker missing:\n%s", text)
	}
}

// #1182 C2: a stored handback's output tail reaches the owner short, as the
// end of the stored tail, through the state-loading path.
func TestValidateReportShowsAShortHandbackTail(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("the sum is wrong"), ActivityResult{}
	}
	f.stepTo("outcome")
	stored := strings.Repeat("x", 500) + "END-OF-TAIL"
	h := ValidateHandback{Commit: f.base, Failures: []HandbackFailure{{Command: "full-one", Argv: []string{"full-one"}, OutputTail: stored, Cause: "the sum is wrong", Locations: []agentresult.RepairLocation{{Path: "app/app.go", Line: 3}}, Objective: "Change Total so it returns 10."}}}
	h.OwnerInstruction = ownerHandbackInstruction(h)
	raw, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.PutValidateOutcome(t.Context(), store.ValidateOutcome{InstanceID: f.instance.ID, Commit: f.base, Outcome: store.ValidateHandedBack, Assessment: []byte("[]"), Handback: raw, CreatedAt: f.clock}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadValidateState(t.Context(), f.opts, f.profile, f.instance.ID)
	if err != nil || st.Handback == nil {
		t.Fatalf("state %+v %v", st.Handback, err)
	}
	out := renderValidateReport(st, nil, "")
	_, fixing, ok := strings.Cut(out, "**Failures:**")
	if !ok {
		t.Fatalf("no failure section:\n%s", out)
	}
	fixing, _, _ = strings.Cut(fixing, "\n\n**")
	if !strings.Contains(fixing, "Cause: the sum is wrong") || !strings.Contains(fixing, "app/app.go:3") || !strings.Contains(fixing, "Change Total so it returns 10.") {
		t.Fatalf("the failure lacks cause, objective or location:\n%s", fixing)
	}
	_, tail, ok := strings.Cut(fixing, "Output tail: `")
	if !ok {
		t.Fatalf("no output tail:\n%s", fixing)
	}
	tail, _, _ = strings.Cut(tail, "`")
	if n := len([]rune(tail)); n > handbackInstructionTail || !strings.HasSuffix(stored, tail) || !strings.HasSuffix(tail, "END-OF-TAIL") {
		t.Fatalf("tail of %d characters %q", n, tail)
	}
}

// #1182 C3/C4: with a failed suite the suite blocker and its bounded cause come
// first, and the items awaiting a check share one neutral line. Readiness is
// unchanged, and with a passed suite each item keeps its own line.
func TestValidateFailedSuiteGroupsItemsAwaitingCheck(t *testing.T) {
	findings := []ValidateFinding{
		{ID: "a", State: findingAwaitingCheck, Criterion: true, Blocking: true},
		{ID: "b", State: findingAwaitingCheck, Criterion: true, Blocking: true},
		{ID: "c", State: findingAwaitingCheck, Blocking: true},
		{ID: "d", State: findingOpen, Optional: true},
	}
	passed := vsBase(findings...)
	passed.computeReadiness()
	want := []string{"Item `a` awaits your check.", "Item `b` awaits your check.", "Item `c` awaits your check.", "Optional finding `d` needs a choice."}
	if strings.Join(passed.Open, "\n") != strings.Join(want, "\n") || passed.Ready {
		t.Fatalf("passed suite open %q ready %v", passed.Open, passed.Ready)
	}

	failed := vsBase(findings...)
	failed.Suite.Outcome = store.SuiteFailed
	cause := strings.Repeat("the path is wrong ", 30)
	failed.Handback = &ValidateHandback{Failures: []HandbackFailure{{Command: "go test ./...", Cause: cause}}}
	failed.computeReadiness()
	if failed.Ready {
		t.Fatal("a failed suite became ready")
	}
	head := failed.Open[0]
	prefix := "The suite did not pass on commit `" + short(failed.Commit) + "` (failed): `go test ./...`: "
	if !strings.HasPrefix(head, prefix) || len([]rune(strings.TrimPrefix(head, prefix))) > suiteCauseLimit {
		t.Fatalf("suite blocker %q", head)
	}
	grouped := 0
	for _, line := range failed.Open {
		if line == "3 items await your check (a, b, c); they cannot be settled while the suite fails." {
			grouped++
		}
		if strings.HasSuffix(line, "awaits your check.") {
			t.Fatalf("per-item line with a failed suite: %q", failed.Open)
		}
	}
	if grouped != 1 || failed.Open[1] != "3 items await your check (a, b, c); they cannot be settled while the suite fails." || !slices.Contains(failed.Open, "Optional finding `d` needs a choice.") {
		t.Fatalf("failed suite open %q", failed.Open)
	}

	// Without a handback the blocker carries the suite's own reason.
	bare := vsBase()
	bare.Suite.Outcome, bare.Suite.Reason = store.SuiteFailed, "a suite command reported test failure"
	bare.computeReadiness()
	if bare.Ready || bare.Open[0] != "The suite did not pass on commit `"+short(bare.Commit)+"` (failed): a suite command reported test failure" {
		t.Fatalf("no handback open %q", bare.Open)
	}
}
