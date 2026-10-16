package turn

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// containerTickets is the ticket side of a Linear project whose code lives in
// a different repository. It records the container every ticket call names.
type containerTickets struct {
	*ownerForge
	mu    sync.Mutex
	repos map[string][]string
}

func (c *containerTickets) record(call string, repo github.Repo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repos[call] = append(c.repos[call], repo.String())
}

func (c *containerTickets) GetIssue(ctx context.Context, repo github.Repo, n int) (github.Issue, error) {
	c.record("GetIssue", repo)
	return c.ownerForge.GetIssue(ctx, repo, n)
}

func (c *containerTickets) UpdateIssue(ctx context.Context, repo github.Repo, n int, title, body redact.Body) (github.Issue, error) {
	c.record("UpdateIssue", repo)
	return c.ownerForge.UpdateIssue(ctx, repo, n, title, body)
}

func (c *containerTickets) SetStoryState(ctx context.Context, repo github.Repo, pn int, pi int64, n int, id int64, completed bool) error {
	c.record("SetStoryState", repo)
	return c.ownerForge.SetStoryState(ctx, repo, pn, pi, n, id, completed)
}

func (c *containerTickets) CloseDeliveredParent(ctx context.Context, repo github.Repo, n int, id int64) error {
	c.record("CloseDeliveredParent", repo)
	return c.ownerForge.CloseDeliveredParent(ctx, repo, n, id)
}

func (c *containerTickets) ListFollowupIssues(ctx context.Context, repo github.Repo) ([]github.Issue, error) {
	c.record("ListFollowupIssues", repo)
	return c.ownerForge.ListFollowupIssues(ctx, repo)
}

func (c *containerTickets) CreateIssue(ctx context.Context, repo github.Repo, title, body redact.Body) (github.Issue, error) {
	c.record("CreateIssue", repo)
	return c.ownerForge.CreateIssue(ctx, repo, title, body)
}

// linearOwnerFixture is the story owner fixture for a task whose ticket
// container (team/project) differs from the code repository (acme/api).
func linearOwnerFixture(t *testing.T) (*ownerFixture, *containerTickets) {
	t.Helper()
	return linearTickets(storyOwnerFixture(t))
}

func linearTickets(o *ownerFixture) (*ownerFixture, *containerTickets) {
	o.target.Task = naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: ownerParent}
	tickets := &containerTickets{ownerForge: o.forge, repos: map[string][]string{}}
	o.delivery.Tickets = tickets
	o.registry = ActivityRegistry{}
	o.register()
	return o, tickets
}

func onlyContainer(t *testing.T, tickets *containerTickets, call string, want string) {
	t.Helper()
	tickets.mu.Lock()
	defer tickets.mu.Unlock()
	got := tickets.repos[call]
	if len(got) == 0 {
		t.Fatalf("%s was not called", call)
	}
	for _, r := range got {
		if r != want {
			t.Fatalf("%s addressed %v, want only %s", call, got, want)
		}
	}
}

// #932 c1: Validate close addresses every story and the parent at the task's
// own ticket container, never at the code repository.
func TestValidateCloseOfLinearStoriesUsesTheTicketContainer(t *testing.T) {
	o, tickets := linearOwnerFixture(t)
	o.toOwner()
	o.reply(o.owner, o.ship())
	nodes := o.toOwner()
	if o.final() != "delivered" {
		t.Fatalf("final %s path %s notice %q", o.final(), joinNodes(nodes), o.notice())
	}
	st := o.state()
	if st.Effects.Close == nil || st.Effects.Close.State != store.EffectSucceeded {
		t.Fatalf("close effect %+v", st.Effects.Close)
	}
	if got := decodeEffectResult(st.Effects.Close).Closed; len(got) != 2 || got[0] != ownerStoryNum || got[1] != ownerParent {
		t.Fatalf("closed %v", got)
	}
	if strings.Join(o.forge.issueCloses, ",") != "520,497" {
		t.Fatalf("closes %v", o.forge.issueCloses)
	}
	onlyContainer(t, tickets, "SetStoryState", "team/project")
	onlyContainer(t, tickets, "CloseDeliveredParent", "team/project")
}

// #932 c2: the report refresh ticks story criteria at the ticket container.
func TestValidateStoryTickOfLinearStoryUsesTheTicketContainer(t *testing.T) {
	o, tickets := linearOwnerFixture(t)
	o.toOwner()
	want := strings.Replace(storyBody, "- [ ] AC-001", "- [x] AC-001", 1)
	if got := o.storyBody(); got != want {
		t.Fatalf("AC-001 ticked:\n%s", got)
	}
	onlyContainer(t, tickets, "GetIssue", "team/project")
	onlyContainer(t, tickets, "UpdateIssue", "team/project")
}

// #932 c2: follow-up issue listing and creation use the ticket container.
func TestValidateFollowupIssueOfLinearTaskUsesTheTicketContainer(t *testing.T) {
	o, tickets := linearTickets(newOwnerFixture(t))
	o.mixed()
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- ok"))
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "story"))
	if e := o.effect(store.EffectIssue, "story-002:code"); e.State != store.EffectSucceeded || len(o.forge.issueCreates) != 1 {
		t.Fatalf("issue effect %+v created %d", e, len(o.forge.issueCreates))
	}
	onlyContainer(t, tickets, "ListFollowupIssues", "team/project")
	onlyContainer(t, tickets, "CreateIssue", "team/project")
}

// #932: a target without a task ref keeps the code repository.
func TestValidateTicketRepoPrefersTheTaskContainer(t *testing.T) {
	target := ValidateDeliveryTarget{Repo: github.Repo{Owner: "acme", Name: "api"}}
	if got := target.ticketRepo().String(); got != "acme/api" {
		t.Fatalf("no task: %s", got)
	}
	target.Task = naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: 1}
	if got := target.ticketRepo().String(); got != "team/project" {
		t.Fatalf("linear task: %s", got)
	}
}
