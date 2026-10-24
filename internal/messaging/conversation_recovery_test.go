package messaging

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationRecoveryTargetAndCurrentGrant(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	first, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "one", Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "two", Name: "second"})
	if err != nil {
		t.Fatal(err)
	}
	run.focus = &conversationFocus{Project: first.Slug(), Issue: "7"}
	run.t.Source = "For second issue 42, what did we decide?"
	project, issue, err := run.recoveryTarget(t.Context())
	if err != nil || project != second.Slug() || issue != "42" {
		t.Fatalf("explicit target %q %q: %v", project, issue, err)
	}
	run.t.Source = "For two/second, continue the discussion"
	project, issue, err = run.recoveryTarget(t.Context())
	if err != nil || project != second.Slug() || issue != "" {
		t.Fatalf("repository switch inherited issue %q %q: %v", project, issue, err)
	}
	prompt := factoryConversationPrompt{RetainedEvidence: []store.ConversationEvidence{{Repository: first.Slug(), Entry: retainedevidence.Entry{Channel: "wrong-repository-id"}}}}
	if err = run.authorizeRestoredPrompt(t.Context(), &prompt); err == nil {
		t.Fatal("saved metadata granted changed repository access")
	}
	allowed, err := run.evidenceAuthorized(t.Context(), store.ConversationEvidence{Repository: first.Slug(), Entry: retainedevidence.Entry{Kind: "ask", OwnerID: "discord:missing", Revision: strings.Repeat("a", 64), Channel: strconv.FormatInt(first.ID, 10)}})
	if err != nil || allowed {
		t.Fatalf("missing owning Ask row remained available: %v %v", allowed, err)
	}
	run.at = at.Add(time.Second)
	if err = run.retainView(t.Context(), 2, "owner", "source", run.t.ID, first.Slug(), "7", "abc", "abc", at, retainedevidence.Unknown); err != nil {
		t.Fatal(err)
	}
	if err = run.retainView(t.Context(), 2, "owner", "source", run.t.ID, first.Slug(), "7", "changed", "changed", at, retainedevidence.Unknown); err == nil {
		t.Fatal("changed retry silently reused captured evidence")
	}
}

func TestConversationRecoveryReplyExcerptCoverage(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "one", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	original := strings.Repeat("x", 5000)
	if err = run.retainView(t.Context(), 15, "reply", "reply1", run.t.ID, repo.Slug(), "", original, original[:4096], at, retainedevidence.Unknown); err != nil {
		t.Fatal(err)
	}
	rows, err := run.s.db.ListConversationTurnEvidence(t.Context(), run.t.ID, at)
	if err != nil || len(rows) != 1 || rows[0].Entry.Bytes != 5000 || rows[0].RangeEnd != 4096 || len(rows[0].Entry.Omissions) == 0 {
		t.Fatalf("lost source excerpt coverage: %+v %v", rows, err)
	}
}

func TestConversationReasonerSummaryKeepsCoverageWithinBudget(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	lookups := make([]conversationReadResult, 6)
	for i := range lookups {
		lookups[i] = conversationReadResult{Command: "ask", FactID: fmt.Sprintf("source-%d", i), Target: "one/app#42", CapturedAt: at, Result: strings.Repeat("x", 16384), Gaps: []string{strings.Repeat("missing ", 4096)}}
	}
	text := reasonerEvidenceText(strings.Repeat("finding ", 4096), lookups)
	if len(text) > 16384 {
		t.Fatalf("summary exceeded existing limit: %d", len(text))
	}
	for _, lookup := range lookups {
		if !strings.Contains(text, lookup.FactID) || !strings.Contains(text, "of 16384") || !strings.Contains(text, "2026-10-24T12:00:00Z") {
			t.Fatalf("source coverage lost: %s", lookup.FactID)
		}
	}
	if !strings.Contains(text, "findings excerpt") || !strings.Contains(text, "additional gap details retained") {
		t.Fatal("summary omissions disappeared")
	}
}
