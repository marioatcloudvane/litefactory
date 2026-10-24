package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestInboxOrganizerBaselineAndPromotions(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	decision := func(ticket, key, kind string, wait time.Time) store.InboxDecision {
		return store.InboxDecision{Source: "github", Project: "owner/project", Ticket: ticket, Key: key, Kind: kind, WaitingAt: wait}
	}
	page := store.InboxPage{Entries: []store.InboxDecision{
		decision("recovery", "r", "parked", at), decision("mixed", "q", "question", at.Add(time.Hour)), decision("mixed", "v", "review", at.Add(-time.Hour)), decision("known", "k", "review", at), decision("unknown", "u", "review", time.Time{}), decision("question", "z", "question", at),
	}}
	// Identity derives from locator/ref, so give these hand-built facts canonical keys.
	for i := range page.Entries {
		page.Entries[i].Locator.Container = "owner/project"
		page.Entries[i].Locator.Key = page.Entries[i].Ticket
	}
	input := inboxBaseline(page)
	want := []string{"recovery", "known", "mixed", "unknown", "question"}
	for i, ticket := range input.Tickets {
		if ticket.Decisions[0].Ticket != want[i] {
			t.Fatalf("baseline %d: %s want %s", i, ticket.Decisions[0].Ticket, want[i])
		}
	}
	if !input.Tickets[2].WaitingAt.Equal(at.Add(-time.Hour)) {
		t.Fatal("mixed ticket used unrelated question time")
	}
	base := inboxFallback(input, "fixture")
	encode := func(tickets []store.InboxOrganizedTicket) []byte {
		raw, err := json.Marshal(inboxOrganizerOutput{Version: 1, Layout: "action_groups", Tickets: tickets})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if _, err := validateInboxOrganization(encode(base.Tickets), input); err != nil {
		t.Fatal(err)
	}
	promoted := append([]store.InboxOrganizedTicket{}, base.Tickets...)
	promoted = append([]store.InboxOrganizedTicket{promoted[4]}, promoted[:4]...)
	if _, err := validateInboxOrganization(encode(promoted), input); err == nil {
		t.Fatal("accepted unsupported promotion")
	}
	input.Tickets[4].Evidence = []store.InboxEvidence{{ID: "e", Kind: "urgency", DecisionKey: "z", Source: "recorded wait", Revision: "current", Excerpt: "Urgency: blocks release"}}
	promoted[0].PromotionReason = "Recorded release urgency"
	promoted[0].EvidenceIDs = []string{"e"}
	if _, err := validateInboxOrganization(encode(promoted), input); err != nil {
		t.Fatal("rejected evidenced promotion", err)
	}
	// A promoted prefix cannot reorder unrelated tickets behind it.
	promoted[1], promoted[2] = promoted[2], promoted[1]
	if _, err := validateInboxOrganization(encode(promoted), input); err == nil {
		t.Fatal("reordered unpromoted tickets")
	}
	promoted[1], promoted[2] = promoted[2], promoted[1]
	promoted[0].DecisionKeys = []string{"r"}
	if _, err := validateInboxOrganization(encode(promoted), input); err == nil {
		t.Fatal("accepted request moved to another ticket")
	}
	for _, raw := range [][]byte{[]byte(`{"version":1,"version":1,"layout":"action_groups","tickets":[]}`), []byte(`{"version":1,"layout":"action_groups","tickets":null}`), append(encode(base.Tickets), []byte(` {}`)...)} {
		if _, err := validateInboxOrganization(raw, input); err == nil {
			t.Fatal("accepted malformed result")
		}
	}
}

func TestInboxOrganizerFailureFinalizesCompleteFallbackOnce(t *testing.T) {
	for _, scenario := range []string{"invalid", "provider", "oversized", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			run, _ := factoryUnitRun(t, &factoryUnitProvider{})
			ctx := t.Context()
			db := run.s.db
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Owner: "owner", Name: "fallback", Forge: "github", BaseBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), Enabled: true, Provenance: "explicit"}, 0); err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(ctx, repo, 8)
			if err != nil {
				t.Fatal(err)
			}
			seeds := []store.ImplementStorySeed{{Key: "one", Position: 0, Branch: "lf/one"}, {Key: "two", Position: 1, Branch: "lf/two"}}
			if scenario == "oversized" {
				for i := 2; i < 80; i++ {
					seeds = append(seeds, store.ImplementStorySeed{Key: fmt.Sprintf("request-%d", i), Position: i, Branch: fmt.Sprintf("lf/request-%d", i)})
				}
			}
			if err = db.EnsureImplementStories(ctx, session.TaskRef, 1, seeds, run.at); err != nil {
				t.Fatal(err)
			}
			for _, seed := range seeds {
				move := store.ImplementTransition{Parent: session.TaskRef, Generation: 1, Key: seed.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: run.at}
				if _, err = db.TransitionImplementStory(ctx, move); err != nil {
					t.Fatal(err)
				}
				move.From = store.ImplementRunning
				move.To = store.ImplementParked
				move.ExpectedRevision = 2
				move.Reason = "Owner recovery required"
				if scenario == "oversized" {
					move.Reason = strings.Repeat("complete recorded fact ", 170)
				}
				if _, err = db.TransitionImplementStory(ctx, move); err != nil {
					t.Fatal(err)
				}
			}
			scope := run.s.inboxScope(run.c.ChannelRef)
			snapshot, err := db.CreateInboxSnapshot(ctx, scope, store.InboxFilter{}, run.at)
			if err != nil {
				t.Fatal(err)
			}
			page, err := db.ReadCompleteInboxSnapshot(ctx, scope, snapshot.ID, run.at)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			call := func(context.Context, []byte, string, int) ([]byte, error) {
				calls++
				if scenario == "provider" {
					return nil, errors.New("provider unavailable")
				}
				return []byte(`{"version":1,"layout":"action_groups","tickets":[]}`), nil
			}
			if scenario == "unavailable" {
				call = nil
			}
			view, err := run.s.finalizeOwnerInbox(ctx, scope, page, run.at, call)
			if err != nil {
				t.Fatal(err)
			}
			if view.Mode != "fallback" || len(view.Tickets) != 1 || len(view.Tickets[0].DecisionKeys) != len(seeds) || view.FallbackReason == "" {
				t.Fatalf("incomplete fallback: %+v", view)
			}
			want := 0
			if scenario == "invalid" {
				want = 2
			}
			if scenario == "provider" {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls %d want %d", calls, want)
			}
			if _, err = run.s.finalizeOwnerInbox(ctx, scope, page, run.at, call); err != nil {
				t.Fatal(err)
			}
			if calls != want {
				t.Fatal("saved fallback reran organizer")
			}
		})
	}
}
