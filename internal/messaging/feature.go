package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/store"
)

// FeatureConfirmer grants only confirmation, never workflow admission.
type FeatureConfirmer interface {
	Confirm(context.Context, string, string, string, string, time.Time) (string, error)
}

// EnableFeatures installs typed commands on the existing owner-only intake.
func (s *Service) EnableFeatures(drafter *feature.Drafter, confirmer FeatureConfirmer) {
	s.drafter, s.confirmer = drafter, confirmer
	s.RegisterCommand(CommandDefinition{Name: "draft", Description: "Preview a feature issue", Arguments: []Argument{
		{Name: "project", Type: TextArgument, Description: "Registered owner/name or unique name"},
		{Name: "note", Type: TextArgument, Description: "Feature idea or change, up to 2000 bytes"},
	}}, nil)
	s.RegisterCommand(CommandDefinition{Name: "confirm", Description: "Create the previewed feature issue", Arguments: []Argument{{Name: "code", Type: TextArgument, Required: true, Description: "8-character preview code"}}}, nil)
}

func (s *Service) featureReply(ctx context.Context, r store.ChatReply, at time.Time) (store.ChatReply, string, error) {
	if r.AnswerAttempts >= 2 {
		return r, "Could not complete this request. Please send a new command.", nil
	}
	r, err := s.db.BeginChatAnswer(ctx, r, r.ResolvedRepoID, at)
	if err != nil {
		return r, "", err
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Minute-20*time.Second)
	defer cancel()
	text := "Feature issues unavailable right now."
	if r.Command == "draft" && s.drafter != nil {
		var repoID int64
		project := r.ProjectOption
		if r.ResolvedRepoID > 0 {
			repo, e := s.db.GetRepo(work, r.ResolvedRepoID)
			if e != nil {
				err = e
			} else {
				project = repo.Slug()
			}
		}
		if err == nil {
			drafter := *s.drafter
			drafter.Bind = func(ctx context.Context, id int64) error { return s.db.ResolveChatProject(ctx, r, id) }
			text, repoID, err = drafter.Reply(work, r.Transport, r.ChannelRef, r.MessageID, project, r.Question, at)
		}
		if err == nil && repoID > 0 {
			err = s.db.ResolveChatProject(work, r, repoID)
		}
	} else if r.Command == "confirm" && s.confirmer != nil {
		text, err = s.confirmer.Confirm(work, r.Transport, r.ChannelRef, r.MessageID, r.Question, at)
	}
	if ctx.Err() != nil {
		return r, "", ctx.Err()
	}
	if err != nil || work.Err() != nil {
		text = "Could not complete the request. Send /confirm again to check creation, or /draft for a new preview."
	}
	if s.Protect != nil {
		text, err = s.Protect(ctx, text)
		if err != nil {
			return r, "", err
		}
	}
	if err = s.db.CompleteChatAnswer(ctx, r, text); err != nil {
		return r, "", err
	}
	r.State = "sent"
	r.ReplyText = text
	return r, text, nil
}
