package messaging

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

func plainFixture(t *testing.T) (*Service, *standardsGateOperations, func(string, time.Time) *factoryConversationRun, *standardsDeliverySender) {
	t.Helper()
	s, ops, start := standardsGateFixture(t)
	s.conversations.Load().factory = true
	sender, ok := s.sender.(*standardsDeliverySender)
	if !ok {
		t.Fatal("fixture sender missing")
	}
	return s, ops, start, sender
}

func proposeAnalysis(t *testing.T, s *Service, start func(string, time.Time) *factoryConversationRun, sender *standardsDeliverySender, project, window, prompt string, at time.Time) owneraction.Proposal {
	t.Helper()
	run := start("analyze "+project+" for "+window, at)
	if err := run.propose(t.Context(), Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "project": project, "window": window}}, ConversationEnvelope{}, nil); err != nil {
		t.Fatal(err)
	}
	deliverAll(t, s, sender, prompt, at)
	page, err := s.ownerActions.History(t.Context(), mustGatePrincipal(t, s), "", 100)
	if err != nil || len(page.Proposals) == 0 {
		t.Fatalf("proposal missing: %+v %v", page, err)
	}
	d := page.Proposals[0]
	if d.State != "awaiting_confirmation" || d.ProposalMessage != prompt {
		t.Fatalf("proposal unavailable: %+v", d)
	}
	return d
}

func deliverAll(t *testing.T, s *Service, sender *standardsDeliverySender, prompt string, at time.Time) {
	t.Helper()
	sender.result.PromptID = prompt
	for {
		more, err := s.ConversationDeliveryStep(t.Context(), at)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			return
		}
	}
}

func TestPlainYesConfirmsTheOneOpenProposal(t *testing.T) {
	for _, yes := range []string{"yes", "Ja", "ok!", "do it", "mach"} {
		t.Run(yes, func(t *testing.T) {
			s, ops, start, sender := plainFixture(t)
			if _, err := s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			p := proposeAnalysis(t, s, start, sender, "litefactory", "last 7 days", "proposal-1", now)
			run := start(yes, now.Add(5*time.Second))
			if handled, err := run.confirm(t.Context()); !handled || err != nil {
				t.Fatalf("plain confirmation: %v %v", handled, err)
			}
			if err := s.ownerActions.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(ops.executed) != 1 || ops.executed[0].OperationKey != "owner-action:"+p.ID || !maps.Equal(ops.executed[0].Arguments, p.Facts.Invocation.Arguments) {
				t.Fatalf("wrong stored action: %+v", ops.executed)
			}
			deliverAll(t, s, sender, "confirmation", now.Add(5*time.Second))
			if !strings.Contains(sender.texts[len(sender.texts)-1], p.ID) {
				t.Fatal("acknowledgment lost history reference")
			}
		})
	}
}

func TestPlainYesNeverConfirmsMoreThanAYesOrAnEarlierProposal(t *testing.T) {
	s, ops, start, sender := plainFixture(t)
	if _, err := s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	first := proposeAnalysis(t, s, start, sender, "litefactory", "last 7 days", "proposal-1", now)
	for _, words := range []string{"yes, but for the last 14 days", "yes please do it tomorrow", "ok what does it cost?"} {
		run := start(words, now.Add(time.Second))
		if handled, err := run.confirm(t.Context()); handled || err != nil {
			t.Fatalf("qualified yes authorized: %v %v", handled, err)
		}
		if err := s.conversationHandled(t.Context(), run.t, "noted", run.at); err != nil {
			t.Fatal(err)
		}
		deliverAll(t, s, sender, "noted", run.at)
	}
	second := proposeAnalysis(t, s, start, sender, "litefactory", "yesterday", "proposal-2", now.Add(2*time.Second))
	run := start("yes", now.Add(7*time.Second))
	run.t.ReplyTo = "proposal-1"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatalf("ambiguous confirmation: %v %v", handled, err)
	}
	if len(ops.executed) != 0 {
		t.Fatal("ambiguous proposals executed")
	}
	deliverAll(t, s, sender, "refusal", run.at)
	if _, err := s.ownerActions.Cancel(t.Context(), mustGatePrincipal(t, s), first.ID); err != nil {
		t.Fatal(err)
	}
	run = start("yes", now.Add(8*time.Second))
	run.t.ReplyTo = "proposal-1"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatal(err)
	}
	deliverAll(t, s, sender, "stale", run.at)
	if len(ops.executed) != 0 {
		t.Fatal("stale reply executed")
	}
	run = start("yes", now.Add(9*time.Second))
	run.t.ReplyTo = "proposal-2"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatal(err)
	}
	if err := s.ownerActions.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(ops.executed) != 1 || !maps.Equal(ops.executed[0].Arguments, second.Facts.Invocation.Arguments) {
		t.Fatalf("wrong selection: %+v", ops.executed)
	}
}

func TestProposalIsPlainWhileTheStoredProposalStaysExact(t *testing.T) {
	s, _, start, sender := plainFixture(t)
	if _, err := s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	p := proposeAnalysis(t, s, start, sender, "litefactory", "last week", "proposal-1", now)
	shown := sender.texts[len(sender.texts)-1]
	if !strings.Contains(shown, "Exact target:") || !strings.Contains(shown, "Arguments:") || !strings.Contains(shown, "Impact: conversational") || !strings.Contains(shown, p.ID) {
		t.Fatalf("review facts missing: %q", shown)
	}
	if p.Facts.Invocation.Arguments["after"] == "" || p.Facts.Invocation.Arguments["before"] == "" || p.Facts.Invocation.Arguments["window"] != "" {
		t.Fatalf("window not frozen: %+v", p.Facts)
	}
	if handled, err := start("details", now.Add(time.Second)).confirm(t.Context()); !handled || err != nil {
		t.Fatal(err)
	}
	deliverAll(t, s, sender, "details", now.Add(time.Second))
	current, err := s.ownerActions.Inspect(t.Context(), mustGatePrincipal(t, s), p.ID)
	if err != nil || current.State != "awaiting_confirmation" || !maps.Equal(current.Facts.Invocation.Arguments, p.Facts.Invocation.Arguments) {
		t.Fatalf("inspection changed action: %+v %v", current, err)
	}
}

// The delivery ordering fence also protects admitted actions retained from the
// earlier policy. Only their persisted link and acknowledgment are needed here.
func announcementStart(t *testing.T, s *Service, start func(string, time.Time) *factoryConversationRun, at time.Time) string {
	t.Helper()
	run := start("confirmation of retained work", at)
	execution := "retained-execution"
	if err := s.db.SaveConversationActionLink(t.Context(), run.t, store.ConversationActionLink{TurnID: run.t.ID, ExecutionID: execution}); err != nil {
		t.Fatal(err)
	}
	if err := run.complete(t.Context(), "The retained action was admitted."); err != nil {
		t.Fatal(err)
	}
	return "discord:3:" + execution
}

func TestBackgroundResultWaitsForItsStartConfirmation(t *testing.T) {
	s, _, start, sender := plainFixture(t)
	now := time.Now().UTC()
	operation := announcementStart(t, s, start, now)
	if err := s.Announce(t.Context(), "3", "log-analysis:a1", operation, "the result", now); err != nil {
		t.Fatal(err)
	}
	posted := len(sender.texts)
	if more, err := s.NoticeStep(t.Context(), now.Add(time.Second)); err != nil || more || len(sender.texts) != posted {
		t.Fatalf("early result: %v %v", more, err)
	}
	deliverAll(t, s, sender, "confirmation", now.Add(2*time.Second))
	if more, err := s.NoticeStep(t.Context(), now.Add(3*time.Second)); err != nil || !more || sender.texts[len(sender.texts)-1] != "the result" {
		t.Fatalf("result ordering: %v %v", more, err)
	}
}

func TestBackgroundResultIsNotHeldForever(t *testing.T) {
	s, _, start, sender := plainFixture(t)
	now := time.Now().UTC()
	operation := announcementStart(t, s, start, now)
	if err := s.Announce(t.Context(), "3", "log-analysis:a1", operation, "the result", now); err != nil {
		t.Fatal(err)
	}
	if more, err := s.NoticeStep(t.Context(), now.Add(2*time.Hour)); err != nil || !more || sender.texts[len(sender.texts)-1] != "the result" {
		t.Fatalf("result held past bound: %v %v", more, err)
	}
}

func TestShortProjectNamesResolveOnlyWhenUnambiguous(t *testing.T) {
	ctx := t.Context()
	s, _, _ := standardsGateFixture(t)
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "cockpit"}); err != nil {
		t.Fatal(err)
	}
	analysis := func(project string) (naturalPayload, string) {
		p := naturalPayload{Invocation: Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "project": project, "window": "yesterday"}}, Channel: "3"}
		refusal, err := s.resolveNatural(ctx, &p, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return p, refusal
	}
	for _, name := range []string{"litefactory", "LiteFactory", "factory"} {
		if p, refusal := analysis(name); refusal != "" || p.Project != "acme/litefactory" {
			t.Fatalf("%q not resolved: %q %+v", name, refusal, p)
		}
	}
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "other", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	p, refusal := analysis("litefactory")
	if p.RepoID != 0 || !strings.Contains(refusal, "acme/litefactory") || !strings.Contains(refusal, "other/litefactory") {
		t.Fatalf("ambiguous short name not asked back: %q %+v", refusal, p)
	}
}

func TestPlainWindowSpellsOutLocalDays(t *testing.T) {
	day := func(d int) string { return time.Date(2026, 10, d, 0, 0, 0, 0, time.Local).Format(time.RFC3339) }
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.Local)
	if got := PlainWindow(day(12), day(19), now); got != "Mon 12 – Sun 18 Oct" {
		t.Fatalf("week: %q", got)
	}
	if got := PlainWindow(day(22), day(23), now); got != "Thu 22 Oct" {
		t.Fatalf("one day: %q", got)
	}
	since := now.Add(-14 * 24 * time.Hour).Format(time.RFC3339)
	if got := PlainWindow(since, now.Format(time.RFC3339), now); !strings.HasPrefix(got, "since Fri 9 Oct") {
		t.Fatalf("window ending now: %q", got)
	}
}
