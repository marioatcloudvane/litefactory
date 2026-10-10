package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// EnableAsk installs the read-only answer service on the existing registry.
// It is called once before the daemon starts its single chat worker.
func (s *Service) EnableAsk(service *ask.Service) {
	s.ask = service
	s.RegisterCommand(CommandDefinition{Name: "ask", Description: "Ask about a project and its open work", Arguments: []Argument{
		{Name: "question", Type: TextArgument, Required: true, Description: "Question, up to 8192 bytes"},
		{Name: "project", Type: TextArgument, Description: "Registered owner/name or unique name"},
	}}, nil)
}

func (s *Service) answer(ctx context.Context, r store.ChatReply, at time.Time) (store.ChatReply, string, error) {
	failure := "Could not answer now, try again."
	var sanitizer *redact.Redactor
	if s.ask != nil {
		sanitizer = s.ask.Redactor
	}
	if sanitizer == nil {
		sanitizer = redact.New(redact.Options{})
	}
	if r.AnswerAttempts >= 2 {
		text, err := ask.RenderNotice(ctx, r.Question, r.ProjectOption, failure, sanitizer)
		return r, text, err
	}
	r, err := s.db.BeginChatAnswer(ctx, r, r.ResolvedRepoID, at)
	if err != nil {
		return r, "", err
	}
	// Reserve twenty seconds of the five-minute reply budget for meter cleanup,
	// durable reply preparation and transport delivery after a provider timeout.
	answerCtx, cancel := context.WithTimeout(ctx, 5*time.Minute-20*time.Second)
	defer cancel()
	var text string
	var repoID int64
	if s.ask == nil {
		text, err = ask.RenderNotice(answerCtx, r.Question, r.ProjectOption, "Answers unavailable right now.", sanitizer)
	} else {
		project := r.ProjectOption
		if r.ResolvedRepoID > 0 {
			repo, readErr := s.db.GetRepo(answerCtx, r.ResolvedRepoID)
			err = readErr
			if readErr == nil {
				project = repo.Slug()
			}
		}
		if err == nil {
			prepared, prepareErr := s.ask.Prepare(answerCtx, r.Question, project, r.ChannelRef, at)
			err = prepareErr
			if err == nil {
				repoID = prepared.Repository.ID
				if r.ResolvedRepoID > 0 && r.ProjectOption == "" {
					prepared.Options.InheritedProject = true
				}
				if repoID > 0 {
					err = s.db.ResolveChatProject(answerCtx, r, repoID)
				}
				if err == nil {
					text, _, err = s.ask.ReplyPrepared(answerCtx, prepared)
				}
			}
		}
	}
	// Daemon shutdown preserves the waiting row for a fresh restart attempt.
	if ctx.Err() != nil {
		return r, "", ctx.Err()
	}
	if err != nil || answerCtx.Err() != nil {
		text, err = ask.RenderNotice(ctx, r.Question, r.ProjectOption, failure, sanitizer)
		if err != nil {
			return r, "", err
		}
	}
	// Apply the transport's configured credential redactor before fixing reply
	// text durably, including the small interval before ordinary delivery Step.
	if s.Protect != nil {
		text, err = s.Protect(ctx, text)
		if err != nil {
			return r, "", err
		}
	}
	if err := s.db.CompleteChatAnswer(ctx, r, text); err != nil {
		return r, "", err
	}
	r.State = "sent"
	r.ReplyText = text
	return r, text, nil
}
