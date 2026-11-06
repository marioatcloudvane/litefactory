package store

import (
	"strconv"
	"strings"
	"testing"
)

func TestBugStartTriggersArePrivate(t *testing.T) {
	if !IsPrivateTrigger(BugStartTrigger(1)) || IsPrivateTrigger(6664969403) {
		t.Fatal("bug start triggers must be private and forge comment ids must not")
	}
	if IsBugStartTrigger(bugStartTriggerEnd) || IsBugStartTrigger(bugStartTriggerBase) {
		t.Fatal("the bug start band must exclude its bounds")
	}
}

func TestStrandedRecoveryExclusionUsesBugStartBand(t *testing.T) {
	want := "l.trigger_id>" + strconv.FormatInt(bugStartTriggerBase, 10) + " AND l.trigger_id<" + strconv.FormatInt(bugStartTriggerEnd, 10)
	if !strings.Contains(graphInstanceBugStartReplyOnly, want) || !strings.Contains(strandedGraphInstancePredicate, graphInstanceBugStartReplyOnly) {
		t.Fatal("the stranded-recovery exclusion must use the bug start band bounds")
	}
}
