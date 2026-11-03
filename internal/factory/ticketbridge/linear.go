package ticketbridge

import (
	"context"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Service preserves existing local slots and qualified payloads without changing persisted identities.
type Service struct{ db *store.DB }

// New composes the private slot mapping adapter.
func New(db *store.DB) *Service { return &Service{db: db} }

// Snapshot translates a verified observation without reassigning saved identities.
func (b *Service) Snapshot(ctx context.Context, s ticket.Snapshot) (github.Issue, error) {
	if s.Ref.Identity.Provider == ticket.GitHub {
		return GitHubSnapshot(s)
	}
	if s.Ref.Identity.Provider != ticket.Linear || s.Ref.Validate() != nil {
		return github.Issue{}, store.ErrTicketIdentityConflict
	}
	slot, err := b.db.LocalTicketSlot(ctx, "issue", s.Ref.Identity)
	if err != nil {
		return github.Issue{}, err
	}
	author, err := b.Actor(ctx, s.Author)
	if err != nil {
		return github.Issue{}, err
	}
	copySnapshot := s
	out := github.Issue{QualifiedSnapshot: &copySnapshot, ID: slot, Number: int(slot), Title: s.Title, Body: s.Body, State: s.State, StateReason: s.StateReason, Author: author, Labels: s.Labels, HTMLURL: s.BrowserURL, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	for _, a := range s.Assignees {
		u, e := b.Actor(ctx, a)
		if e != nil {
			return github.Issue{}, e
		}
		out.Assignees = append(out.Assignees, u)
	}
	return out, nil
}

// Actor retains the existing local alias for a qualified actor.
func (b *Service) Actor(ctx context.Context, a ticket.Actor) (github.User, error) {
	if a.Identity.RemoteID == "" {
		return github.User{}, nil
	}
	slot, err := b.db.LocalTicketSlot(ctx, "actor", a.Identity)
	return github.User{ID: slot, Login: a.Handle, IsBot: a.IsBot, IsHuman: !a.IsBot}, err
}

// Reply retains the existing reply and parent aliases with qualified provenance.
func (b *Service) Reply(ctx context.Context, r ticket.Reply) (github.Comment, error) {
	slot, err := b.db.LocalTicketSlot(ctx, "comment", r.Identity)
	if err != nil {
		return github.Comment{}, err
	}
	author, err := b.Actor(ctx, r.Author)
	if err != nil {
		return github.Comment{}, err
	}
	var replyTo int64
	if r.ReplyTo.RemoteID != "" {
		if r.ReplyTo.Provider != r.Identity.Provider || r.ReplyTo.Origin != r.Identity.Origin {
			return github.Comment{}, store.ErrTicketIdentityConflict
		}
		replyTo, err = b.db.LocalTicketSlot(ctx, "comment", r.ReplyTo)
		if err != nil {
			return github.Comment{}, err
		}
	}
	copyReply := r
	return github.Comment{ReplyToCommentID: replyTo, ID: slot, Author: author, Body: r.Body, HTMLURL: r.BrowserURL, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, QualifiedReply: &copyReply}, nil
}
