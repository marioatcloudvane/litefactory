package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type optionalParentReader interface {
	ObserveParent(context.Context, ticket.Ref) (*ticket.Ref, error)
}

// effectiveAgent is shared by dispatch and recovery. Only Bugs gain the
// read-only immediate-parent policy; Story label inheritance stays separate.
func (d *Daemon) effectiveAgent(ctx context.Context, t *task, workflow agent.Workflow) (agentDecision, string, error) {
	choice := d.resolveAgent(t.issue.Labels)
	if choice.refused() || choice.Agent != "" {
		return choice, "label", nil
	}
	if workflow == agent.WorkflowBug {
		parent, err := d.bugParent(ctx, t)
		if err != nil {
			return choice, "", err
		}
		if parent != nil {
			choice, err = d.recordedParentAgent(ctx, *parent)
			if err != nil {
				return choice, "", err
			}
			if choice.Agent == "" && !choice.refused() {
				ingress, _, err := d.connectionIngress(ctx, t.forgeRepo, t.issue.Number)
				if err != nil {
					return choice, "", err
				}
				if ingress == nil {
					client, ok := d.forge.(*github.Client)
					if !ok {
						return choice, "", errors.New("bug parent issue reader unavailable")
					}
					adapter, err := github.NewTickets(client)
					if err != nil {
						return choice, "", err
					}
					ingress = adapter
				}
				reader, ok := ingress.(interface {
					Read(context.Context, ticket.Ref) (ticket.Snapshot, error)
				})
				if !ok {
					return choice, "", errors.New("bug parent issue reader unavailable")
				}
				snapshot, err := reader.Read(ctx, *parent)
				if err != nil {
					return choice, "", err
				}
				if snapshot.Ref.Identity != parent.Identity || snapshot.Ref.Locator != parent.Locator {
					return choice, "", errors.New("bug parent identity changed")
				}
				choice = d.resolveAgent(snapshot.Labels)
			}
			if choice.refused() || choice.Agent != "" {
				return choice, "parent", nil
			}
		}
		if d.bugAgent != "" {
			return agentDecision{Agent: string(d.bugAgent)}, "bug_agent", nil
		}
	}
	return agentDecision{Agent: string(d.defaultAgent)}, "default", nil
}

func (d *Daemon) bugParent(ctx context.Context, t *task) (*ticket.Ref, error) {
	ingress, origin, err := d.connectionIngress(ctx, t.forgeRepo, t.issue.Number)
	if err != nil {
		return nil, err
	}
	if ingress == nil {
		client, ok := d.forge.(*github.Client)
		if !ok {
			return nil, errors.New("bug optional parent reader unavailable")
		}
		adapter, err := github.NewTickets(client)
		if err != nil {
			return nil, err
		}
		ingress, origin = adapter, adapter.Origin()
	}
	reader, ok := ingress.(optionalParentReader)
	if !ok {
		// A provider without parent relationships (such as Linear) has no
		// observable parent; that is not a lookup failure.
		return nil, nil
	}
	child, err := ingress.Resolve(ctx, locatorAt(origin, t.forgeRepo, t.issue.Number))
	if err != nil {
		return nil, err
	}
	expected := locatorAt(origin, t.forgeRepo, t.issue.Number)
	if child.Ref.Locator != expected || (t.issue.ID > 0 && child.Ref.Identity.RemoteID != fmt.Sprint(t.issue.ID)) {
		return nil, errors.New("bug child identity changed")
	}
	parent, err := reader.ObserveParent(ctx, child.Ref)
	if err != nil || parent == nil {
		return parent, err
	}
	if err := parent.Validate(); err != nil {
		return nil, err
	}
	if parent.Identity.Provider != child.Ref.Identity.Provider || parent.Identity.Origin != child.Ref.Identity.Origin || parent.Identity == child.Ref.Identity {
		return nil, errors.New("bug parent identity differs from child provider or instance")
	}
	return parent, nil
}

func (d *Daemon) recordedParentAgent(ctx context.Context, parent ticket.Ref) (agentDecision, error) {
	session, err := d.db.GetSessionByTicket(ctx, parent.Identity)
	if errors.Is(err, store.ErrNotFound) {
		session, err = d.db.GetSessionByTicketLocator(ctx, parent.Locator)
		if err == nil {
			binding, e := d.db.GetSessionTicket(ctx, session.ID)
			if e != nil {
				return agentDecision{}, e
			}
			if binding.RemoteID != "" && binding.RemoteID != parent.Identity.RemoteID {
				return agentDecision{}, store.ErrTicketIdentityConflict
			}
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		return agentDecision{}, nil
	}
	if err != nil {
		return agentDecision{}, err
	}
	runs, err := d.db.ListRuns(ctx, session.ID)
	if err != nil {
		return agentDecision{}, err
	}
	if len(runs) == 0 || runs[len(runs)-1].Agent == "" {
		return agentDecision{}, nil
	}
	selected := strings.ToLower(runs[len(runs)-1].Agent)
	if _, ok := d.agents[agent.Kind(selected)]; !ok {
		return agentDecision{}, fmt.Errorf("bug parent recorded agent %q is no longer configured", selected)
	}
	return agentDecision{Agent: selected}, nil
}
