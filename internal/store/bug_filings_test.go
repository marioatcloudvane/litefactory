package store

import "testing"

func TestBugStartBandMatchesRecoveryExclusion(t *testing.T) {
	if bugStartTriggerBase != 36028797018963968 || int64(1<<56) != 72057594037927936 {
		t.Fatal("the stranded-recovery exclusion literals no longer match the bug start band")
	}
	if !IsPrivateTrigger(BugStartTrigger(1)) || IsPrivateTrigger(6664969403) {
		t.Fatal("bug start triggers must be private and forge comment ids must not")
	}
}
