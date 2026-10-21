package ask

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// History is ephemeral and newest first. Unavailable readers are optional.
type History struct {
	Turns        []Turn
	OlderLeftOut bool
	Unavailable  bool
}

// HistoryReader supplies only the transport's own explicit ask replies.
type HistoryReader interface {
	ReadAskHistory(context.Context, string) (History, error)
}

// Model can only return a typed answer. It receives no issue or factory client.
type Model interface {
	Answer(context.Context, int64, string) ([]byte, error)
}

// ErrUnavailable indicates that no configured provider has verified isolation.
var ErrUnavailable = response.ErrUnavailable

// Service combines read-only project context and a response-only model. It has
// no inbox writer: delivery and restart fences remain in the messaging service.
type Service struct {
	Instructions           prompts.Resolver
	InstructionDiagnostics prompts.Sink
	Repositories           RepositoryReader
	Collector              Collector
	History                HistoryReader
	Model                  Model
	Redactor               *redact.Redactor
}

// Prepared is ephemeral read-only context. The inbox worker can retain the
// resolved repository ID before invoking the model, but never stores this value.
type Prepared struct {
	Repository store.Repo
	Bundle     Bundle
	History    History
	Options    RenderOptions
	Refusal    string
}

// Prepare resolves a project and freshly collects evidence without invoking a
// provider. Keeping this stage separate lets the worker bind restart identity.
func (s *Service) Prepare(ctx context.Context, question, project, channel string, at time.Time) (Prepared, error) {
	var prepared Prepared
	if s.Redactor == nil {
		return prepared, errors.New("ask: redactor missing")
	}
	history := History{Unavailable: true}
	if s.History != nil {
		var err error
		history, err = s.History.ReadAskHistory(ctx, channel)
		if err != nil {
			history = History{Unavailable: true}
		}
	}
	resolution, err := ResolveProject(ctx, s.Repositories, project, history.Turns)
	if err != nil {
		return prepared, err
	}
	prepared.History = history
	prepared.Options = RenderOptions{Question: question, InheritedProject: resolution.Inherited, HistoryUnavailable: history.Unavailable, OlderContextCut: history.OlderLeftOut, Redactor: s.Redactor}
	if resolution.Refusal != "" {
		message := resolution.Refusal
		if history.Unavailable {
			message += "\nHistory unavailable; answered without it."
		}
		prepared.Refusal, err = RenderNotice(ctx, question, project, message, s.Redactor)
		return prepared, err
	}
	prepared.Repository = resolution.Repository
	targets := ExplicitIssues(question)
	if len(targets) > 1 {
		prepared.Refusal, err = RenderNotice(ctx, question, resolution.Repository.Slug(), "Please ask about one issue at a time.", s.Redactor)
		return prepared, err
	}
	target := 0
	if len(targets) == 1 {
		target = targets[0]
		prepared.History.Turns = nil
	}
	prepared.Bundle, err = s.Collector.collect(ctx, resolution.Repository, at, target)
	return prepared, err
}

// RenderNotice uses the same turn prefix for refusals and provider failures.
func RenderNotice(ctx context.Context, question, project, message string, r *redact.Redactor) (string, error) {
	if r == nil {
		return "", errors.New("ask: redactor missing")
	}
	text := TurnPrefix(question, project) + message
	body, err := r.Apply(ctx, text, 64<<10)
	return body.String(), err
}

// TurnPrefix is the transport-neutral history marker. Quotes are single line,
// so neither a question nor project can inject another turn prefix.
func TurnPrefix(question, project string) string {
	return "> " + runeExcerpt(strings.Join(strings.Fields(question), " "), 160) + "\n> Project: " + runeExcerpt(strings.Join(strings.Fields(project), " "), 100) + "\n[lf:ask:v1]\n"
}
