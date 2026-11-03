package daemon_test

import (
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/store"
	"testing"
	"time"
)

func TestChatCreatedFeatureDiscoveryStaysIdle(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	p := store.ChatIssuePreview{ID: "preview", Body: feature.Heading + "\n\n## Summary\nExport reports\n\n## Problem\nManual copying\n\n## Wanted outcome\nDownload\n\n## Origin", CreatedAt: time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)}
	f.forge.addIssue(fakeIssue{Number: testIssue, Title: "Export report", Body: feature.IssueBody(p), User: bot()})
	f.settle()
	if len(f.turns.calls()) != 0 || len(f.runs()) != 0 {
		t.Fatal("chat-created issue started work")
	}
	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 0 || len(reacted) != 0 {
		t.Fatal("chat-created issue entered admission")
	}
}
