package messaging

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationLegacyEvidenceSurvivesInterruptedRecovery(t *testing.T) {
	provider := &factoryUnitProvider{fail: true}
	run, at := factoryUnitRun(t, provider)
	if err := run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	// Prerequisite rows have no fact_id/status/target. Recovery must derive
	// presentation metadata from their immutable turn and exchange identities.
	legacy := []struct {
		ordinal                   int
		raw, body, target, status string
		complete                  bool
		gaps                      []string
	}{
		{3, `{"command":"status","arguments":{},"source":"factory:status","result":"No tasks are running.","complete":true,"gaps":[]}`, "No tasks are running.", "factory", "complete", true, []string{}},
		{6, `{"command":"question","arguments":{"project":"acme/app","issue":"42"},"source":"factory:question","result":"Question temporarily unavailable.","complete":false,"gaps":["Current question unavailable."]}`, "Question temporarily unavailable.", "acme/app#42", "incomplete", false, []string{"Current question unavailable."}},
	}
	for _, old := range legacy {
		if err := run.s.db.SaveConversationExchange(t.Context(), run.t, store.ConversationExchange{TurnID: run.t.ID, Ordinal: old.ordinal, Kind: "evidence", Result: old.raw, CapturedAt: at, Complete: old.complete, Gaps: strings.Join(old.gaps, "\n")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := run.s.conversationOutcome(t.Context(), run.t, "interrupted", "The prior provider turn was interrupted; its retained evidence remains available.", at); err != nil {
		t.Fatalf("legacy reads prevented interrupted completion: %v", err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.FinishConversationReply(t.Context(), reply, "recovery-message", at); err != nil {
		t.Fatal(err)
	}
	answer, err := run.s.db.ReadConversationAnswer(t.Context(), "discord", "1", "2", "")
	if err != nil {
		t.Fatal(err)
	}
	details := detailsUnitFreeze(t, run, answer, at)
	_, raw, found := strings.Cut(details.ReplyAttachment, "\n\n")
	if !found {
		t.Fatalf("legacy details unavailable: %+v", details)
	}
	var exported struct {
		AnswerID string                   `json:"answer_id"`
		Results  []conversationReadResult `json:"results"`
	}
	if err = json.Unmarshal([]byte(raw), &exported); err != nil {
		t.Fatal(err)
	}
	if exported.AnswerID != answer.ID || len(exported.Results) != len(legacy) {
		t.Fatalf("legacy result association lost: %+v", exported)
	}
	for i, result := range exported.Results {
		old := legacy[i]
		wantID := fmt.Sprintf("%s-fact-%d", run.t.ID, old.ordinal)
		if result.FactID != wantID || result.Target != old.target || result.Status != old.status || result.Result != old.body || result.Complete != old.complete || !result.CapturedAt.Equal(at.Truncate(time.Millisecond)) {
			t.Fatalf("legacy metadata not restored from saved record: %+v, want identity %s capture %s", result, wantID, at.Truncate(time.Millisecond))
		}
	}
	saved, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != len(legacy) {
		t.Fatal("recovery invented evidence")
	}
	for i, exchange := range saved {
		if exchange.Result != legacy[i].raw {
			t.Fatal("legacy normalization rewrote retained source bytes")
		}
	}
	if provider.calls != 0 {
		t.Fatal("recovery invoked provider to replace retained evidence")
	}
}
