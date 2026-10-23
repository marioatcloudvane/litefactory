package messaging

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// plainFixture is the standards gate fixture with factory delivery, so a
// delivered proposal is bound for confirmation as in production.
func plainFixture(t *testing.T) (*Service, *standardsGateOperations, func(string, time.Time) *factoryConversationRun, *standardsDeliverySender) {
	t.Helper()
	s, ops, start := standardsGateFixture(t)
	s.conversations.Load().factory = true
	return s, ops, start, s.sender.(*standardsDeliverySender)
}

// proposeAnalysis runs the propose path for one owner message and delivers
// the proposal as Discord message prompt.
func proposeAnalysis(t *testing.T, s *Service, start func(string, time.Time) *factoryConversationRun, sender *standardsDeliverySender, project, window, prompt string, at time.Time) store.NaturalDialogue {
	t.Helper()
	ctx := t.Context()
	run := start("analyze "+project+" for "+window, at)
	inv := Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "project": project, "window": window}}
	if err := run.propose(ctx, inv, ConversationEnvelope{}, nil); err != nil {
		t.Fatal(err)
	}
	deliverAll(t, s, sender, prompt, at)
	d, err := s.db.ReadNaturalDialogue(ctx, "discord", "7", "3")
	if err != nil || d.State != "awaiting_confirmation" || d.PromptID != prompt {
		t.Fatalf("proposal not awaiting confirmation: %+v %v", d, err)
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
			ctx := t.Context()
			s, ops, start, sender := plainFixture(t)
			if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			d := proposeAnalysis(t, s, start, sender, "litefactory", "last 7 days", "proposal-1", now)
			// Not a Discord reply: the plain yes alone confirms.
			run := start(yes, now.Add(time.Second))
			if handled, err := run.confirm(ctx); !handled || err != nil {
				t.Fatalf("plain yes not handled: %v %v", handled, err)
			}
			if len(ops.executed) != 1 || ops.executed[0].MessageID == "" || !strings.HasSuffix(ops.executed[0].OperationKey, "nl-exec:"+d.Code) {
				t.Fatalf("plain yes did not run the stored proposal once: %+v", ops.executed)
			}
			deliverAll(t, s, sender, "confirmation", now.Add(time.Second))
			// One short message: no saved outcome block or operation references.
			confirmation := sender.texts[len(sender.texts)-1]
			for _, internal := range []string{"Saved action outcome", "Saved standards operation", "Exact retained references", "nl-exec", "{"} {
				if strings.Contains(confirmation, internal) {
					t.Fatalf("confirmation shows %q: %q", internal, confirmation)
				}
			}
		})
	}
}

func TestPlainYesNeverConfirmsMoreThanAYesOrAnEarlierProposal(t *testing.T) {
	ctx := t.Context()
	s, ops, start, sender := plainFixture(t)
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	proposeAnalysis(t, s, start, sender, "litefactory", "last 7 days", "proposal-1", now)
	for _, text := range []string{"yes, but for the last 14 days", "yes please do it tomorrow", "ok what does it cost?"} {
		run := start(text, now.Add(time.Second))
		if handled, err := run.confirm(ctx); handled || err != nil {
			t.Fatalf("%q was taken as a confirmation: %v %v", text, handled, err)
		}
		// Such a message is an ordinary turn for the conversation model.
		if err := s.conversationHandled(ctx, run.t, "noted", now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		deliverAll(t, s, sender, "proposal-1", now.Add(time.Second))
	}
	// A second proposal replaces the first: the store holds one per
	// conversation, so a plain yes can never pick between two.
	first, err := s.db.ReadNaturalDialogue(ctx, "discord", "7", "3")
	if err != nil {
		t.Fatal(err)
	}
	second := proposeAnalysis(t, s, start, sender, "litefactory", "yesterday", "proposal-2", now.Add(2*time.Second))
	if second.Revision != first.Revision+1 || second.Code == first.Code {
		t.Fatalf("second proposal did not replace the first: %+v", second)
	}
	// Yes in reply to the earlier proposal message, and the earlier code, do nothing.
	stale := start("yes", now.Add(3*time.Second))
	stale.t.ReplyTo = "proposal-1"
	if handled, err := stale.confirm(ctx); !handled || err != nil {
		t.Fatalf("stale yes not answered: %v %v", handled, err)
	}
	deliverAll(t, s, sender, "refusal-1", now.Add(3*time.Second))
	if handled, err := start(first.Code, now.Add(3*time.Second)).confirm(ctx); !handled || err != nil {
		t.Fatalf("earlier code not answered: %v %v", handled, err)
	}
	deliverAll(t, s, sender, "refusal-2", now.Add(3*time.Second))
	if len(ops.executed) != 0 {
		t.Fatalf("a stale confirmation ran: %+v", ops.executed)
	}
	// Other messages came after the latest proposal, so a bare yes is the
	// agent's to judge; a yes in reply to the proposal still confirms it.
	bare := start("yes", now.Add(4*time.Second))
	if handled, err := bare.confirm(ctx); handled || err != nil {
		t.Fatalf("bare yes after other messages bypassed the agent: %v %v", handled, err)
	}
	if err := s.conversationHandled(ctx, bare.t, "noted", now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	deliverAll(t, s, sender, "noted", now.Add(4*time.Second))
	replied := start("yes", now.Add(4*time.Second))
	replied.t.ReplyTo = "proposal-2"
	if handled, err := replied.confirm(ctx); !handled || err != nil {
		t.Fatalf("yes in reply not handled: %v %v", handled, err)
	}
	var p naturalPayload
	if err = json.Unmarshal([]byte(second.Payload), &p); err != nil {
		t.Fatal(err)
	}
	if len(ops.executed) != 1 || !maps.Equal(ops.executed[0].Arguments, p.Invocation.Arguments) {
		t.Fatalf("yes did not run exactly the latest proposal: %+v", ops.executed)
	}
}

func TestProposalIsPlainWhileTheStoredProposalStaysExact(t *testing.T) {
	ctx := t.Context()
	s, _, start, sender := plainFixture(t)
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	d := proposeAnalysis(t, s, start, sender, "litefactory", "last week", "proposal-1", now)
	shown := sender.texts[len(sender.texts)-1]
	for _, internal := range []string{"{", "Proposed action", "Exact protected arguments", "repository_id", d.Code, "T00:00:00"} {
		if strings.Contains(shown, internal) {
			t.Fatalf("proposal shows %q: %q", internal, shown)
		}
	}
	if !strings.Contains(shown, "litefactory") || !strings.Contains(shown, "last week (") || !strings.Contains(shown, d.Code[:4]) {
		t.Fatalf("proposal does not name the project, window and code: %q", shown)
	}
	// The digest binds the stored payload, whose invocation is exactly what
	// resolution produces for the same request.
	var stored naturalPayload
	if err := json.Unmarshal([]byte(d.Payload), &stored); err != nil {
		t.Fatal(err)
	}
	if _, digest, err := naturalDigest(stored); err != nil || digest != d.Digest {
		t.Fatalf("stored digest does not bind the payload: %v", err)
	}
	want := naturalPayload{Invocation: Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "project": "litefactory", "window": "last week"}}, Channel: "3"}
	if refusal, err := s.resolveNatural(ctx, &want, now); err != nil || refusal != "" {
		t.Fatalf("resolution refused: %q %v", refusal, err)
	}
	if !maps.Equal(stored.Invocation.Arguments, want.Invocation.Arguments) || stored.Project != "acme/litefactory" {
		t.Fatalf("stored invocation differs: %+v want %+v", stored.Invocation.Arguments, want.Invocation.Arguments)
	}
	// Details show the exact request without cancelling it.
	if handled, err := start("details", now.Add(time.Second)).confirm(ctx); !handled || err != nil {
		t.Fatalf("details not answered: %v %v", handled, err)
	}
	deliverAll(t, s, sender, "details", now.Add(time.Second))
	details := sender.texts[len(sender.texts)-1]
	if !strings.Contains(details, d.Code) || !strings.Contains(details, `"action":"start"`) {
		t.Fatalf("details miss the exact request: %q", details)
	}
	if after, err := s.db.ReadNaturalDialogue(ctx, "discord", "7", "3"); err != nil || after.State != "awaiting_confirmation" {
		t.Fatalf("details ended the proposal: %+v %v", after, err)
	}
}

func TestBackgroundResultWaitsForItsStartConfirmation(t *testing.T) {
	ctx := t.Context()
	s, ops, start, sender := plainFixture(t)
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	proposeAnalysis(t, s, start, sender, "litefactory", "yesterday", "proposal-1", now)
	if handled, err := start("yes", now.Add(time.Second)).confirm(ctx); !handled || err != nil {
		t.Fatalf("yes not handled: %v %v", handled, err)
	}
	operation := ops.executed[0].OperationKey
	// The analysis ends before the start confirmation is posted.
	if err := s.Announce(ctx, "3", "log-analysis:a1", operation, "the result", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	posted := len(sender.texts)
	if more, err := s.NoticeStep(ctx, now.Add(2*time.Second)); err != nil || more || len(sender.texts) != posted {
		t.Fatalf("result posted before its start confirmation: %v %v", more, err)
	}
	deliverAll(t, s, sender, "confirmation", now.Add(3*time.Second))
	if more, err := s.NoticeStep(ctx, now.Add(4*time.Second)); err != nil || !more {
		t.Fatalf("result not posted after its confirmation: %v %v", more, err)
	}
	if n := len(sender.texts); n != posted+2 || sender.texts[n-1] != "the result" || sender.texts[n-2] == "the result" {
		t.Fatalf("confirmation and result out of order: %q", sender.texts[posted:])
	}
}

func TestBackgroundResultIsNotHeldForever(t *testing.T) {
	ctx := t.Context()
	s, ops, start, sender := plainFixture(t)
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	proposeAnalysis(t, s, start, sender, "litefactory", "yesterday", "proposal-1", now)
	if handled, err := start("yes", now.Add(time.Second)).confirm(ctx); !handled || err != nil {
		t.Fatalf("yes not handled: %v %v", handled, err)
	}
	if err := s.Announce(ctx, "3", "log-analysis:a1", ops.executed[0].OperationKey, "the result", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// The confirmation is never delivered (a paused conversation).
	if more, err := s.NoticeStep(ctx, now.Add(2*time.Hour)); err != nil || !more || sender.texts[len(sender.texts)-1] != "the result" {
		t.Fatalf("result held past its bound: %v %v", more, err)
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
