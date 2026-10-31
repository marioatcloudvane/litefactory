package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
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
	s.conversations.Load().factory = true
	proposal := start("analyze app for the last 7 days", now)
	if err = proposal.propose(ctx, p.Invocation, ConversationEnvelope{}, nil); err != nil {
		t.Fatal(err)
	}
	sender, ok := s.sender.(*standardsDeliverySender)
	if !ok {
		t.Fatal("fixture sender missing")
	}
	deliverAll(t, s, sender, "analysis-prompt", now)

	if len(ops.executed) != 0 {
		t.Fatal("a proposal started an analysis before confirmation")
	}
	run := start("yes", now.Add(5*time.Second))
	run.t.ReplyTo = "analysis-prompt"
	if handled, err := run.confirm(ctx); !handled || err != nil {
		t.Fatalf("confirmation not handled: %v %v", handled, err)
	}
	if err = s.ownerActions.Tick(ctx); err != nil {
		t.Fatal(err)
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
	sender, ok := s.sender.(*standardsDeliverySender)
	if !ok {
		t.Fatalf("sender is %T", s.sender)
	}
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

// TestLogAnalysisClarifiedProjectReusesTheEarlierWindow replays the reported
// exchange: an unscoped window request, Nico's clarifying question and a short
// project answer give one proposal without the owner repeating the window. The
// window is still spanned only from owner text, and an owner turn retained under
// another project stays out of the follow-up's sources.
func TestLogAnalysisClarifiedProjectReusesTheEarlierWindow(t *testing.T) {
	ctx := t.Context()
	local := time.Now().In(time.Local)
	phrase, day := "today since 2 pm", local
	if local.Before(time.Date(local.Year(), local.Month(), local.Day(), 14, 1, 0, 0, time.Local)) {
		phrase, day = "since 2 pm yesterday", local.AddDate(0, 0, -1)
	}
	window := "please analyze the factory runs for " + phrase
	answer := "litefactory only"
	span := func(id, text, part string) string {
		start := strings.Index(text, part)
		return fmt.Sprintf(`{"source_id":%q,"start":%d,"end":%d}`, id, start, start+len(part))
	}
	propose := func(windowSource string) string {
		return `{"version":1,"kind":"propose","command":"standards-analysis","arguments":[{"name":"action","value":"start"},` +
			strings.Replace(span("103", answer, "litefactory"), "{", `{"name":"project",`, 1) + `,` +
			strings.Replace(span(windowSource, window, phrase), "{", `{"name":"window",`, 1) + `]}`
	}
	p := &factoryUnitProvider{outputs: []string{
		`{"version":1,"kind":"answer","text":"acme/app is quiet."}`,
		`{"version":1,"kind":"clarify","text":"Which project should I analyze?"}`,
		propose("delivered"),
		propose("102"),
	}}
	r, at := factoryUnitRun(t, p, "How is acme/app doing?")
	for _, repo := range []store.Repo{{Forge: "github", Owner: "acme", Name: "app"}, {Forge: "github", Owner: "marioatcloudvane", Name: "litefactory"}} {
		repo.Path = t.TempDir()
		if _, err := r.s.db.UpsertRepo(ctx, repo); err != nil {
			t.Fatal(err)
		}
	}
	r.s.EnableStandards(&standardsGateOperations{db: r.s.db})
	if _, err := r.s.db.UpsertIdentity(ctx, store.Identity{Forge: "github", Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := r.s.db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "2", DiscordOwnerID: "1"}
	actions := owneraction.New(r.s.db, OwnerActionDomain{Service: r.s}, func() time.Time { return at })
	actions.SetDiscord("1", []string{"2"})
	actions.SetDelivery(gateApproval{})
	r.s.EnableOwnerActions(actions, func(context.Context, string) (owneraction.Principal, error) { return principal, nil })

	for i, input := range []string{"How is acme/app doing?", window, answer} {
		if i > 0 {
			scan, err := r.s.db.ReadNaturalScan(ctx, "2")
			if err != nil {
				t.Fatal(err)
			}
			source := fmt.Sprint(101 + i)
			if err := r.s.db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: source}, []store.NaturalMessage{{ChannelRef: "2", SourceID: source, OwnerID: "1", Text: input, CreatedAt: at}}, true); err != nil {
				t.Fatal(err)
			}
			if _, err := r.s.db.AdmitConversationTurn(ctx, r.c.ID, source, "{}", at); err != nil {
				t.Fatal(err)
			}
			if r.t, err = r.s.db.ClaimConversationTurn(ctx, r.c.ID, "followup", at, time.Minute); err != nil {
				t.Fatal(err)
			}
			if r.c, err = r.s.db.ReadConversation(ctx, "discord", "2"); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.s.runFactoryConversation(ctx, r.c, r.b, r.t, at); err != nil {
			t.Fatal(err)
		}
		reply, err := r.s.db.ClaimConversationReply(ctx, "discord", at)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.s.db.FinishConversationReply(ctx, reply, "delivered", at); err != nil {
			t.Fatal(err)
		}
	}
	var followup struct {
		OwnerSources []DialogueTurn `json:"owner_sources"`
	}
	if err := json.Unmarshal([]byte(p.prompts[2]), &followup); err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, source := range followup.OwnerSources {
		sources[source.SourceID] = true
	}
	if !sources["103"] || !sources["102"] {
		t.Fatalf("follow-up owner sources %v lack the current answer or the unscoped window request", followup.OwnerSources)
	}
	if sources["101"] || sources["delivered"] {
		t.Fatalf("follow-up owner sources %v include a turn scoped to another project or agent text", followup.OwnerSources)
	}
	if !strings.Contains(p.prompts[3], "the span names no supplied owner message") {
		t.Fatal("a window span citing the factory reply was not rejected")
	}
	if p.calls != 4 {
		t.Fatalf("provider calls=%d, want answer, clarify, rejected and corrected proposal", p.calls)
	}
	page, err := actions.History(ctx, principal, "", 10)
	if err != nil || len(page.Proposals) != 1 {
		t.Fatalf("proposals: %+v %v", page, err)
	}
	args := page.Proposals[0].Facts.Invocation.Arguments
	start := time.Date(day.Year(), day.Month(), day.Day(), 14, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
	if args["project"] != "marioatcloudvane/litefactory" || args["after"] != start || args["before"] != at.UTC().Format(time.RFC3339) {
		t.Fatalf("proposal arguments %v, want litefactory from %s until %s", args, start, at.UTC().Format(time.RFC3339))
	}
}
