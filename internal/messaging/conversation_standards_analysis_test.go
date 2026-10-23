package messaging

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestLogAnalysisStartIsAConfirmedActionWithResolvedScope(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	repo, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	p := naturalPayload{Invocation: Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "project": "acme/app", "window": "last 7 days", "dry_run": "true"}}, Channel: "3"}
	if refusal, err := s.resolveNatural(ctx, &p, now); err != nil || refusal != "" {
		t.Fatalf("analysis proposal refused: %q %v", refusal, err)
	}
	payload, digest, err := naturalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: digest, Code: "analysis-code", ExpiresAt: now.Add(time.Minute), UpdatedAt: now}
	if err = s.db.SaveNaturalDialogue(ctx, dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.db.BindNaturalPrompt(ctx, dialogue, "analysis-prompt"); err != nil {
		t.Fatal(err)
	}
	if len(ops.executed) != 0 {
		t.Fatal("a proposal started an analysis before confirmation")
	}
	run := start("yes", now.Add(time.Second))
	run.t.ReplyTo = "analysis-prompt"
	if handled, err := run.confirm(ctx); !handled || err != nil {
		t.Fatalf("confirmation not handled: %v %v", handled, err)
	}
	if len(ops.executed) != 1 {
		t.Fatalf("confirmation ran %d operations", len(ops.executed))
	}
	got := ops.executed[0]
	want := map[string]string{"repository_id": strconv.FormatInt(repo.ID, 10), "after": now.Add(-7 * 24 * time.Hour).Format(time.RFC3339), "before": now.Format(time.RFC3339), "dry_run": "true"}
	if got.Operation != "analysis.start" || got.ChannelID != "3" {
		t.Fatalf("unexpected operation: %+v", got)
	}
	for name, value := range want {
		if got.Arguments[name] != value {
			t.Errorf("%s = %q, want %q", name, got.Arguments[name], value)
		}
	}
	if got.Arguments["project"] == "" || got.Arguments["window"] != "" {
		t.Fatalf("scope was not resolved to an exact project and window: %+v", got.Arguments)
	}
}

func TestLogAnalysisStartAsksForAnUnclearScope(t *testing.T) {
	ctx := t.Context()
	s, ops, _ := standardsGateFixture(t)
	if _, err := s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string]map[string]string{
		"no window":      {"action": "start", "project": "acme/app"},
		"unknown window": {"action": "start", "project": "acme/app", "window": "whenever"},
		"no project":     {"action": "start", "window": "yesterday"},
	} {
		p := naturalPayload{Invocation: Invocation{Command: "standards-analysis", Arguments: args}, Channel: "3"}
		refusal, err := s.resolveNatural(ctx, &p, time.Now())
		if err != nil || refusal == "" {
			t.Errorf("%s: proposed without a clear scope: %q %v", name, refusal, err)
		}
	}
	if len(ops.executed) != 0 {
		t.Fatal("an unclear request ran")
	}
}

func TestAnnouncePostsALongMessageInOrderedParts(t *testing.T) {
	ctx := t.Context()
	s, _, _ := standardsGateFixture(t)
	sender := s.sender.(*standardsDeliverySender)
	before := len(sender.texts)
	text := strings.Repeat("A finding about the test setup. ", 200)
	now := time.Now().UTC()
	if err := s.Announce(ctx, "3", "log-analysis:a1", "", text, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Announce(ctx, "3", "log-analysis:a1", "", text, now); err != nil {
		t.Fatal(err)
	}
	for {
		more, err := s.NoticeStep(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	posted := sender.texts[before:]
	if len(posted) < 2 || strings.Join(posted, "") != text {
		t.Fatalf("message was not posted once in ordered parts: %d parts", len(posted))
	}
	for i, target := range sender.targets[before:] {
		if target.Channel != "3" || replySize(posted[i]) > 2000 {
			t.Fatalf("part %d: wrong channel or over the message limit", i)
		}
	}
}
