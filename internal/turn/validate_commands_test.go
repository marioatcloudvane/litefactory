package turn

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestParseValidateCommandValid(t *testing.T) {
	const rev = "0123456789ab"
	sha40 := strings.Repeat("a", 40)
	sha64 := strings.Repeat("b", 64)
	cases := []struct {
		body string
		want ValidateCommand
	}{
		{"/validate-check " + rev + " s1:ac1 pass -- looked fine", ValidateCommand{Name: ValidateCheckCommand, Revision: rev, Criterion: "s1:ac1", Verdict: "pass", Evidence: "looked fine"}},
		{"/validate-check " + rev + " s1:ac1 fail -- saw https://x.test/y", ValidateCommand{Name: ValidateCheckCommand, Revision: rev, Criterion: "s1:ac1", Verdict: "fail", Evidence: "saw https://x.test/y"}},
		{"/validate-finding " + rev + " s1:code repair", ValidateCommand{Name: ValidateFindingCommand, Revision: rev, Finding: "s1:code", Choice: "repair"}},
		{"/validate-finding " + rev + " s1:code defer", ValidateCommand{Name: ValidateFindingCommand, Revision: rev, Finding: "s1:code", Choice: "defer"}},
		{"/validate-finding " + rev + " s1:code issue bug", ValidateCommand{Name: ValidateFindingCommand, Revision: rev, Finding: "s1:code", Choice: "issue", IssueKind: "bug"}},
		{"/validate-finding " + rev + " s1:code issue story", ValidateCommand{Name: ValidateFindingCommand, Revision: rev, Finding: "s1:code", Choice: "issue", IssueKind: "story"}},
		{"/validate-push " + rev, ValidateCommand{Name: ValidatePushCommand, Revision: rev}},
		{"/validate-push", ValidateCommand{Name: ValidatePushCommand}},
		{"/validate-ship -- accepted risk", ValidateCommand{Name: ValidateShipCommand, Evidence: "accepted risk"}},
		{"/validate-ship d-1234567890 -- accepted risk", ValidateCommand{Name: ValidateShipCommand, Reference: "d-1234567890", Evidence: "accepted risk"}},
		{"/return d-1234567890 -- use ISO dates", ValidateCommand{Name: ValidateReturnCommand, Reference: "d-1234567890", Guidance: "use ISO dates"}},
		{"  /validate-push " + rev + "  ", ValidateCommand{Name: ValidatePushCommand, Revision: rev}},
		{"/validate-merge " + rev + " 12 " + sha40, ValidateCommand{Name: ValidateMergeCommand, Revision: rev, PR: 12, Head: sha40}},
		{"/validate-merge " + rev + " 7 " + sha64, ValidateCommand{Name: ValidateMergeCommand, Revision: rev, PR: 7, Head: sha64}},
		{"/validate-close " + rev + " 3 1 2", ValidateCommand{Name: ValidateCloseCommand, Revision: rev, Issues: []int{3, 1, 2}}},
		{"/validate-ship " + rev + " -- clicked through the page", ValidateCommand{Name: ValidateShipCommand, Revision: rev, Evidence: "clicked through the page"}},
		{"/validate-finding " + rev + " s1:architecture accept -- the owner chose tooltips", ValidateCommand{Name: ValidateFindingCommand, Revision: rev, Finding: "s1:architecture", Choice: "accept", Evidence: "the owner chose tooltips"}},
		{"/validate-implement " + rev, ValidateCommand{Name: ValidateImplementCommand, Revision: rev}},
		{"/validate-implement " + rev + " -- keep the tooltips", ValidateCommand{Name: ValidateImplementCommand, Revision: rev, Guidance: "keep the tooltips"}},
	}
	for _, tc := range cases {
		wrapped := "`" + strings.TrimSpace(tc.body) + "`"
		if got, err := ParseValidateCommand(wrapped); err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("wrapped %q: %+v %v", wrapped, got, err)
		}
		if _, err := ParseValidateCommand(wrapped + "\r\nmore"); err == nil {
			t.Errorf("wrapped trailing text accepted: %q", wrapped)
		}
		got, err := ParseValidateCommand(tc.body)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.body, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %+v want %+v", tc.body, got, tc.want)
		}
	}
}

func TestParseValidateCommandMalformed(t *testing.T) {
	const rev = "0123456789ab"
	sha := strings.Repeat("a", 40)
	cases := map[string]string{

		"uppercase revision":      "/validate-push 0123456789AB",
		"11 char revision":        "/validate-push 0123456789a",
		"13 char revision":        "/validate-push 0123456789abc",
		"unknown command":         "/validate-nope " + rev,
		"validate entry":          "/validate " + rev,
		"extra args push":         "/validate-push " + rev + " now",
		"defer with kind":         "/validate-finding " + rev + " s1:code defer bug",
		"repair with kind":        "/validate-finding " + rev + " s1:code repair bug",
		"issue without kind":      "/validate-finding " + rev + " s1:code issue",
		"issue bad kind":          "/validate-finding " + rev + " s1:code issue task",
		"finding bad choice":      "/validate-finding " + rev + " s1:code waive",
		"finding no choice":       "/validate-finding " + rev + " s1:code",
		"finding bad id":          "/validate-finding " + rev + " nocolon repair",
		"check no evidence":       "/validate-check " + rev + " s1:ac1 pass",
		"check empty evidence":    "/validate-check " + rev + " s1:ac1 pass -- ",
		"check bad verdict":       "/validate-check " + rev + " s1:ac1 maybe -- x",
		"evidence on push":        "/validate-push " + rev + " -- why",
		"evidence on finding":     "/validate-finding " + rev + " s1:code repair -- why",
		"evidence on merge":       "/validate-merge " + rev + " 1 " + sha + " -- why",
		"evidence on close":       "/validate-close " + rev + " 1 -- why",
		"multi-line":              "/validate-push " + rev + "\nmore",
		"multi-line check":        "/validate-check " + rev + " s1:ac1 pass -- a\nb",
		"over 2048 bytes":         "/validate-check " + rev + " s1:ac1 pass -- " + strings.Repeat("x", 2048),
		"empty":                   "",
		"duplicate close numbers": "/validate-close " + rev + " 1 2 1",
		"close without numbers":   "/validate-close " + rev,
		"close zero":              "/validate-close " + rev + " 0",
		"close non-number":        "/validate-close " + rev + " x",
		"merge short sha":         "/validate-merge " + rev + " 1 abc1234",
		"merge uppercase sha":     "/validate-merge " + rev + " 1 " + strings.Repeat("A", 40),
		"merge zero PR":           "/validate-merge " + rev + " 0 " + sha,
		"merge negative PR":       "/validate-merge " + rev + " -1 " + sha,
		"merge missing head":      "/validate-merge " + rev + " 1",
		"ship without evidence":   "/validate-ship " + rev,
		"ship empty evidence":     "/validate-ship " + rev + " -- ",
		"ship extra argument":     "/validate-ship " + rev + " now -- checked",

		"accept without reason":    "/validate-finding " + rev + " s1:architecture accept",
		"accept empty reason":      "/validate-finding " + rev + " s1:architecture accept -- ",
		"accept with kind":         "/validate-finding " + rev + " s1:architecture accept bug -- why",
		"implement extra argument": "/validate-implement " + rev + " story-002",
		"implement empty guidance": "/validate-implement " + rev + " -- ",
	}
	for name, body := range cases {
		if got, err := ParseValidateCommand(body); err == nil {
			t.Errorf("%s: %q accepted as %+v", name, body, got)
		}
	}
}

func TestParseCommandClassifiesValidateControls(t *testing.T) {
	if len(ValidateControlCommands) != 9 {
		t.Fatalf("want nine commands, got %v", ValidateControlCommands)
	}
	for _, name := range ValidateControlCommands {
		switch name {
		case "/validate", "/deliver", "/merge", "/followup", "/waive":
			t.Errorf("%s collides with an existing command", name)
		}
		if !IsValidateControlCommand(name) {
			t.Errorf("%s not recognised", name)
		}
		c := ParseCommand(name + " 0123456789ab")
		if c.Kind != KindValidateControl {
			t.Errorf("%s: kind %v", name, c.Kind)
		}
		if mode, ok := c.Mode(); !ok || mode != agent.ModeValidate {
			t.Errorf("%s: mode %q ok=%v", name, mode, ok)
		}
		if !c.Spawns() {
			t.Errorf("%s does not spawn", name)
		}
	}
	if c := ParseCommand("/validate x"); c.Kind != KindSpike {
		t.Errorf("/validate x: kind %v", c.Kind)
	}
	for _, body := range []string{"/merge 1 2", "/deliver x", "/followup x", "/waive x"} {
		if c := ParseCommand(body); c.Kind == KindValidateControl {
			t.Errorf("%s classified as validate control", body)
		}
	}
}

// #573 c-next: the next step names /validate-ship whenever one ship reply can
// deliver, and keeps today's text when something blocks.
func TestNextStepNamesShipWhenNothingBlocks(t *testing.T) {
	ship := "`" + ValidateShipCommand + " 0123456789ab -- <reason>`"
	closeSet := []int{10, 11, 12}
	optional := ValidateFinding{ID: "b:code", Story: "b", Name: "code", Role: agent.ReviewRoleCode, Optional: true, State: findingOpen}
	blocking := ValidateFinding{ID: "b:code", Story: "b", Name: "code", Role: agent.ReviewRoleCode, Blocking: true, State: findingOpen}
	notReviewed := ValidateFinding{ID: "b:code", Story: "b", Name: "code", Role: agent.ReviewRoleCode, State: findingNotReviewed}
	failedSuite := twoStories()
	failedSuite.Suite.Outcome = store.SuiteFailed
	failedSuite.computeReadiness()
	unfinished := twoStories()
	unfinished.Findings[4].State = findingOwnerPassed
	unfinished.Effects.Push = vsEffect(1, store.EffectPush, "", store.EffectSucceeded, nil)
	unfinished.Effects.PR = vsEffect(2, store.EffectPR, "", store.EffectSucceeded, map[string]any{"number": 42, "head": strings.Repeat("e", 40)})
	unfinished.Effects.Merge = vsEffect(3, store.EffectMerge, "", store.EffectFailed, nil)
	unfinished.computeReadiness()
	failedCheck := twoStories()
	failedCheck.Findings[4].State = findingOwnerFailed
	failedCheck.computeReadiness()
	cases := map[string]struct {
		st   ValidateState
		want string
	}{
		"an open check":            {twoStories(), ship},
		"an open optional finding": {twoStories(optional), ship},
		"an unfinished delivery":   {unfinished, ship},
		"a blocking finding":       {twoStories(blocking), ship},
		"a failed criterion":       {failedCheck, ship},
		"a failed suite":           {failedSuite, ship},
		"a missing reviewer":       {twoStories(notReviewed), ship},
	}
	for name, c := range cases {
		got := c.st.NextStep(closeSet)
		if !strings.Contains(got, c.want) || c.want != ship && strings.Contains(got, ValidateShipCommand) {
			t.Errorf("%s: next step %q, want %q", name, got, c.want)
		}
	}
}
