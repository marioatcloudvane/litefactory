package messaging_test

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestWorkflowDecisionShortNoticeKeepsCauseWorkAndNextStep(t *testing.T) {
	record := store.NoticeRecord{
		TaskRef: "github:acme/repo#1172", Workflow: "plan", Ready: true,
		Key: "review", Kind: "review", CommentID: 91, State: "waiting", Published: true,
		StopOrigin: "dependency", StopCause: "blocked",
		StopReason: "The dependency review is still awaiting the owner's decision.",
	}
	events, err := messaging.NoticeEvents("owner_waits", record)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	event := events[0]
	if event.Kind != "stopped" {
		t.Fatal("fixture needs a parked decision notice")
	}
	event.Title = strings.Repeat("A long issue title ", 200)
	text := messaging.NoticeText(event)
	for _, needed := range []string{"Plan #1172", record.StopReason, "Open the report below", "reply with the requested decision", "https://github.com/acme/repo/issues/1172#issuecomment-91"} {
		if !strings.Contains(text, needed) {
			t.Fatalf("short notice lost %q: %s", needed, text)
		}
	}
	if len(utf16.Encode([]rune(text))) > 1900 {
		t.Fatal("short notice exceeds chat budget")
	}
	record.Published = false
	events, err = messaging.NoticeEvents("owner_waits", record)
	if err != nil || len(events) != 1 {
		t.Fatalf("unpublished events=%+v err=%v", events, err)
	}
	text = messaging.NoticeText(events[0])
	if !strings.Contains(text, record.StopReason) || !strings.Contains(text, "decision report is not confirmed") || strings.Contains(text, "#issuecomment-") {
		t.Fatal("unpublished decision was offered as a confirmed destination: " + text)
	}

}
