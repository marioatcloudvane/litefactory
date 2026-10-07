package turn

import (
	"encoding/json"
	"strings"
	"testing"

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

func TestRenderValidateReport(t *testing.T) {
	s := vsBase(
		ValidateFinding{ID: "s:ac2", State: findingAwaitingCheck, Criterion: true, Blocking: true, Summary: "needs eyes"},
		ValidateFinding{ID: "s:arch", State: findingOpen, Optional: true},
		ValidateFinding{ID: "s:ok", State: findingSatisfied},
	)
	s.Revision = "0123456789ab"
	s.computeReadiness()
	s.Handback = &ValidateHandback{OwnerInstruction: "Fix the thing in Implement."}
	out := renderValidateReport(s, nil, "stale revision given")

	for _, want := range []string{
		"`s:ac2`", "`s:arch`",
		"Fix the thing in Implement.", "stale revision given",
		ValidateCheckCommand + " 0123456789ab s:ac2 pass",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "`s:ok`") {
		t.Error("satisfied finding listed in findings")
	}
	s.Handback = nil
	if got := renderValidateReport(s, nil, ""); strings.Contains(got, "Fix the thing in Implement.") || strings.Contains(got, "stale revision given") {
		t.Error("handback or notice shown without being set")
	}
}
