package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/localprojects"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// DeliveryRequestOptions contains an external owner credential reference, never
// a token argument. The publisher credential cannot stand in for the requester.
type DeliveryRequestOptions = localprojects.DeliveryRequestOptions

// RequestCodeDelivery retains the daemon host entry while factory owns the
// retained code-authority check and durable control admission.
func RequestCodeDelivery(ctx context.Context, options DeliveryRequestOptions) (store.DeliveryControlSource, error) {
	receipt, err := localprojects.RequestCodeDelivery(ctx, options)
	return store.DeliveryControlSource(receipt), err
}

// PollCodeDeliveryControls drains durable CLI controls every tick and, on the
// owning project's listing tick only, reads comments on already bound GitHub
// PRs. Ticket replies never enter here.
func (d *Daemon) PollCodeDeliveryControls(ctx context.Context, now time.Time) error {
	if d.connections == nil {
		return nil
	}
	applier, ok := d.turn.(interface {
		ApplyDelivery(context.Context, turn.DeliveryControl) error
	})
	if !ok {
		return nil
	}
	pending, err := d.db.PendingDeliveryControlSources(ctx)
	if err != nil {
		return err
	}
	var failures []error
	apply := func(source store.DeliveryControlSource) error {
		session, e := d.db.GetSession(ctx, source.SessionID)
		if e != nil {
			return e
		}
		authority, e := d.db.EnsureDeliveryCodeAuthority(ctx, session.ID)
		if e != nil {
			return e
		}
		return applier.ApplyDelivery(ctx, turn.DeliveryControl{Parent: session.TaskRef, SourceID: source.ID, CommentID: source.CommentID, ActorID: source.ActorID, OwnerID: authority.OwnerID, Body: source.Body})
	}
	for _, source := range pending {
		if err := apply(source); err != nil {
			failures = append(failures, err)
		}
	}
	parents, err := d.db.ListDeliveryParents(ctx)
	if err != nil {
		return errors.Join(errors.Join(failures...), err)
	}
	for _, parent := range parents {
		session, e := d.db.GetSessionByTaskRef(ctx, parent)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		// Reading the bound PR is the listing's safety net: it runs only on
		// the project's listing tick. Local CLI controls drain every tick.
		if d.schedule != nil && !d.schedule.Due(session.RepoID, now) {
			continue
		}
		mapping, e := d.db.GetSessionTicket(ctx, session.ID)
		if errors.Is(e, store.ErrNotFound) {
			continue
		}
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if mapping.Locator.Provider != ticket.Linear {
			continue
		}
		proposals, e := d.db.ListDeliveries(ctx, parent)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		for _, q := range proposals {
			if !q.Proposed || q.Superseded || q.Reported || q.PRNumber <= 0 {
				continue
			}
			if e := d.pollBoundDeliveryPR(ctx, q, apply); e != nil {
				failures = append(failures, e)
			}
		}
	}
	return errors.Join(failures...)
}
func (d *Daemon) pollBoundDeliveryPR(ctx context.Context, q store.DeliveryProposal, apply func(store.DeliveryControlSource) error) error {
	binding, err := d.db.GetDeliveryPRBinding(ctx, q.Key)
	if err != nil {
		return err
	}
	authority, err := d.db.EnsureDeliveryCodeAuthority(ctx, binding.SessionID)
	if err != nil {
		return err
	}
	version, err := d.db.GetConnectionVersion(ctx, authority.CodeVersionID)
	if err != nil {
		return err
	}
	clients, err := d.connections.Clients(ctx, version, authority.PublisherID)
	if err != nil {
		return err
	}
	repo, err := deliveryRepository(authority.Repository)
	if err != nil {
		return err
	}
	destination, err := clients.Code.InspectRepository(ctx, repo)
	if err != nil {
		return err
	}
	if strconv.FormatInt(destination.ID, 10) != authority.RepositoryID || !strings.EqualFold(destination.FullName, authority.Repository) {
		return errors.New("bound code repository identity changed")
	}
	pr, err := clients.Code.GetDeliveryPR(ctx, repo, binding.PRNumber)
	if err != nil {
		return err
	}
	if pr.ID != binding.PRID || pr.ID != q.PRID || pr.BaseRepo != authority.Repository || pr.HeadRepo != authority.Repository {
		return errors.New("bound GitHub PR identity changed")
	}
	comments, err := clients.Code.ListCommentsAfter(ctx, repo, binding.PRNumber, 0)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if c.Author.ID != authority.OwnerID || c.Author.IsBot || !c.Author.IsHuman || turn.HasMarker(c.Body) || !c.CreatedAt.After(binding.EnrolledAt) {
			continue
		}
		command, parseErr := turn.ParseDelivery(c.Body)
		if parseErr != nil || !command.Merge {
			continue
		}
		if c.ID <= 0 || c.APIURL != fmt.Sprintf("%s/repos/%s/issues/comments/%d", authority.Origin, authority.Repository, c.ID) || c.IssueAPIURL != fmt.Sprintf("%s/repos/%s/issues/%d", authority.Origin, authority.Repository, binding.PRNumber) {
			return errors.New("GitHub approval comment belongs to another destination")
		}
		source, err := d.db.RetainDeliveryControlSource(ctx, store.DeliveryControlSource{ID: fmt.Sprintf("github-pr:%s:%s:%d:%d", authority.Origin, authority.Repository, binding.PRID, c.ID), SessionID: binding.SessionID, Kind: "github-pr-comment", ProposalKey: q.Key, Generation: q.Generation, ActorID: c.Author.ID, CommentID: c.ID, Body: c.Body, CreatedAt: c.CreatedAt})
		if err != nil {
			return err
		}
		if !source.Completed {
			if err = apply(source); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListDeliveryComments reads an enrolled PR through its saved code connection.
func (d *ProjectCodeDelivery) ListDeliveryComments(ctx context.Context, repo github.Repo, number int, after int64) ([]github.Comment, error) {
	client, err := d.selectClient(ctx, repo, false)
	if err != nil {
		return nil, err
	}
	return client.ListCommentsAfter(ctx, repo, number, after)
}

// PostDeliveryComment sends prepared approval text through the code publisher.
func (d *ProjectCodeDelivery) PostDeliveryComment(ctx context.Context, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	client, err := d.selectClient(ctx, repo, true)
	if err != nil {
		return github.Comment{}, err
	}
	return client.PostComment(ctx, repo, number, body)
}

func deliveryRepository(destination string) (github.Repo, error) {
	owner, name, found := strings.Cut(destination, "/")
	repo := github.Repo{Owner: owner, Name: name}
	if !found {
		return repo, errors.New("code repository must name owner/repository")
	}
	return repo, repo.Validate()
}
