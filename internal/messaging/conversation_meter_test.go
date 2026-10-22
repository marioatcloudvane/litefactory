package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationMeterFollowupRetainsTaskAndRechecksAccess(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	if _, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	_, issued, err := run.lookup(t.Context(), Invocation{Command: "meter", Arguments: map[string]string{"project": "acme/app", "task": "42"}})
	if err != nil || issued == nil {
		t.Fatalf("initial lookup reference=%+v err=%v", issued, err)
	}
	refs, err := run.references(t.Context())
	if err != nil || len(refs) != 1 {
		t.Fatalf("retained references=%+v err=%v", refs, err)
	}
	if refs[0].ID != issued.ID || refs[0].Arguments["project"] != "acme/app" || refs[0].Arguments["task"] != "42" {
		t.Fatalf("task target changed in retained reference: %+v", refs[0])
	}
	raw, err := json.Marshal(ConversationEnvelope{Version: 1, Kind: "lookup", Command: "meter", Arguments: []ConversationArgument{{Name: "project", Reference: refs[0].ID}, {Name: "task", Reference: refs[0].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	_, followup, err := DecodeConversationEnvelope(raw, ConversationDecodeRequest{Turns: []DialogueTurn{{SourceID: "102", Text: "And that task?"}}, Commands: run.s.ConversationCommands(), References: refs, OwnerID: run.s.owner, ChannelID: run.c.ChannelRef, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	run.ordinal++
	result, _, err := run.lookup(t.Context(), followup)
	if err != nil {
		t.Fatal(err)
	}
	if result.Arguments["project"] != "acme/app" || result.Arguments["task"] != "42" || result.Result != "No usage is recorded for this filter." || !result.Complete {
		t.Fatalf("follow-up read changed target: %+v", result)
	}
	// A retained target never replaces the current channel authorization check.
	run.s.conversations.Load().check = func(context.Context, string) error { return errors.New("access revoked") }
	if _, _, err = run.lookup(t.Context(), followup); err == nil {
		t.Fatal("retained reference bypassed revoked access")
	}
}

func TestConversationMeterFeatureTotalAcrossSnapshotPages(t *testing.T) {
	provider := &factoryUnitProvider{outputs: []string{
		`{"version":1,"kind":"lookup","command":"meter","arguments":[{"name":"project","value":"acme/app"},{"name":"task","value":"770"}]}`,
		`{"version":1,"kind":"answer","text":"The feature cost about $44.46, including its stories."}`,
	}}
	r, at := factoryUnitRun(t, provider, "What did #770 cost?")
	ctx := t.Context()
	repo, err := r.s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	model := "fixture"
	var rates []store.MeterPriceRate
	for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, store.MeterPriceRate{CodingAgent: "claude", Model: model, Dimension: dimension, USDPerMillion: "1"})
	}
	if _, err := r.s.db.SetMeterPrices(ctx, "claude", model, rates); err != nil {
		t.Fatal(err)
	}
	var parentRunID int64
	for i, issue := range []int{770, 776, 777, 778} {
		session, err := r.s.db.GetOrCreateSession(ctx, repo, issue)
		if err != nil {
			t.Fatal(err)
		}
		segment, err := r.s.db.GetOrCreateSegment(ctx, session.ID, 1, "session", store.AgentClaude)
		if err != nil {
			t.Fatal(err)
		}
		run, err := r.s.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: int64(issue), LogPath: func(n int) string { return fmt.Sprintf("/logs/%d", n) }})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			parentRunID = run.ID
		}
		a, err := r.s.db.StartMeterAttempt(ctx, run.ID, "claude")
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			key := fmt.Sprint(issue)
			if _, err := r.s.db.ReservePlanStory(ctx, "github:acme/app#770", key); err != nil {
				t.Fatal(err)
			}
			if err := r.s.db.BindPlanStoryIssue(ctx, "github:acme/app#770", key, int64(issue), issue); err != nil {
				t.Fatal(err)
			}
		}
		// More than 256 rows forces the real snapshot reader to fetch another page.
		for n := range 70 {
			id := fmt.Sprint(n)
			parent := ""
			if n > 0 {
				parent = "0"
			}
			if err := r.s.db.RecordMeterEvent(ctx, a.ID, "claude", agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: a.ID, ID: id, ParentID: parent, SourceID: "session", SourceInvocationID: id}}); err != nil {
				t.Fatal(err)
			}
			input, zero := int64(0), int64(0)
			if n == 0 {
				input = []int64{7885700, 12000000, 12000000, 12574300}[i]
			}
			if err := r.s.db.RecordMeterEvent(ctx, a.ID, "claude", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: a.ID, SourceID: "session", EventKey: id, InvocationID: id, ActualModel: &model, ModelProvenance: "message", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &input, Output: &zero, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete}}}); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.s.db.FinishMeterAttempt(ctx, a.ID, "success", "complete", "", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	result, _, err := r.lookup(ctx, Invocation{Command: "meter", Arguments: map[string]string{"project": "acme/app", "task": "770", "details": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Result, "44.460000") || strings.Contains(result.Result, "7.8857") || presentation.ConversationDiagnostics(result.Result) {
		t.Fatalf("family amount: %+v", result)
	}
	// Unknown coverage must not erase known dollars or demand technical caveats.
	if _, err := r.s.db.StartMeterAttempt(ctx, parentRunID, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := r.s.runFactoryConversation(ctx, r.c, r.b, r.t, at); err != nil {
		t.Fatal(err)
	}
	if len(provider.prompts) != 2 || !strings.Contains(provider.prompts[1], "44.460000") {
		t.Fatalf("family evidence not supplied: %v", provider.prompts)
	}
	reply, err := r.s.db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ReplyText, "$44.46") || presentation.ConversationDiagnostics(reply.ReplyText) {
		t.Fatalf("cost answer: %q", reply.ReplyText)
	}

}

// meterUsage records one complete attempt with a single priced or unpriced
// observation for issue under the given mode and model.
func meterUsage(t *testing.T, db *store.DB, repo store.Repo, issue int, mode, model string, input int64) {
	t.Helper()
	ctx := t.Context()
	session, err := db.GetOrCreateSession(ctx, repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: mode, Agent: store.AgentClaude, TriggerCommentID: int64(issue), LogPath: func(n int) string { return fmt.Sprintf("/logs/%d-%d", issue, n) }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.StartMeterAttempt(ctx, run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordMeterEvent(ctx, a.ID, "claude", agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: a.ID, ID: "0", SourceID: "session", SourceInvocationID: "0"}}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := db.RecordMeterEvent(ctx, a.ID, "claude", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: a.ID, SourceID: "session", EventKey: "0", InvocationID: "0", ActualModel: &model, ModelProvenance: "message", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &input, Output: &zero, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttempt(ctx, a.ID, "success", "complete", "", "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestConversationMeterBreakdownInOneLookup(t *testing.T) {
	run, _ := factoryUnitRun(t, &factoryUnitProvider{fail: true}, "How did 770 split by mode and model?")
	ctx := t.Context()
	db := run.s.db
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	var rates []store.MeterPriceRate
	for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, store.MeterPriceRate{CodingAgent: "claude", Model: "alpha", Dimension: dimension, USDPerMillion: "1"})
	}
	if _, err := db.SetMeterPrices(ctx, "claude", "alpha", rates); err != nil {
		t.Fatal(err)
	}
	meterUsage(t, db, repo, 770, "plan", "alpha", 2000000)
	if _, err := db.ReservePlanStory(ctx, "github:acme/app#770", "776"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindPlanStoryIssue(ctx, "github:acme/app#770", "776", 776, 776); err != nil {
		t.Fatal(err)
	}
	meterUsage(t, db, repo, 776, "implement", "alpha", 3000000)
	meterUsage(t, db, repo, 777, "implement", "beta", 1000000) // another task, out of scope
	if _, err := db.ReservePlanStory(ctx, "github:acme/app#770", "778"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindPlanStoryIssue(ctx, "github:acme/app#770", "778", 778, 778); err != nil {
		t.Fatal(err)
	}
	meterUsage(t, db, repo, 778, "implement", "beta", 1000000) // unpriced model

	result, _, err := run.lookup(ctx, Invocation{Command: "meter", Arguments: map[string]string{"task": "770", "by": "mode,model"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Arguments["project"] != "acme/app" {
		t.Fatalf("project not resolved from the task number: %+v", result)
	}
	for _, want := range []string{"mode plan, model alpha: USD 2.", "mode implement, model alpha: USD 3.", "Total: at least USD 5.", "Cost unavailable (usage not reported or not priced) for: mode implement, model beta."} {
		if !strings.Contains(result.Result, want) {
			t.Fatalf("breakdown missing %q:\n%s", want, result.Result)
		}
	}
	if strings.Count(result.Result, "unavailable") != 1 {
		t.Fatalf("unavailable rows must be named once:\n%s", result.Result)
	}
	run.ordinal++
	byRun, _, err := run.lookup(ctx, Invocation{Command: "meter", Arguments: map[string]string{"project": "acme/app", "task": "770", "by": "run"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run 1 of issue 770: USD 2.", "run 1 of issue 776: USD 3."} {
		if !strings.Contains(byRun.Result, want) {
			t.Fatalf("run breakdown missing %q:\n%s", want, byRun.Result)
		}
	}
	if strings.Contains(byRun.Result, "issue 777") {
		t.Fatalf("breakdown left the task scope:\n%s", byRun.Result)
	}
	run.ordinal++
	empty, _, err := run.lookup(ctx, Invocation{Command: "meter", Arguments: map[string]string{"project": "acme/app", "task": "770", "mode": "research", "by": "agent"}})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Result != "No usage is recorded for this filter." || !empty.Complete {
		t.Fatalf("no usage is a fact, not a gap: %+v", empty)
	}
}
