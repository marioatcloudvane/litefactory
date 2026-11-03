package feature

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"github.com/cloudvane/litefactory/internal/redact"
)

// Heading retains the feature issue's fixed body heading.
const Heading = featuredraft.Heading

// MarkerPrefix retains the saved preview publication marker.
const MarkerPrefix = featuredraft.MarkerPrefix

// Draft is the factory-owned isolated generation response.
type Draft = featuredraft.Draft

// PreviewStore is the bounded persistence capability used by factory drafting.
type PreviewStore = featuredraft.PreviewStore

// Decode delegates strict response validation to the factory owner.
func Decode(raw []byte) (Draft, error) { return featuredraft.Decode(raw) }

// Sanitize delegates protected draft normalization to the factory owner.
func Sanitize(s string) string { return featuredraft.Sanitize(s) }

// Hash retains the exact saved title and body compatibility encoding.
func Hash(title, body string) string { return featuredraft.Hash(title, body) }

// Marker returns the factory's saved publication marker.
func Marker(p messagingstate.ChatIssuePreview) string { return featuredraft.Marker(p) }

// IssueBody returns the exact protected body and origin footer.
func IssueBody(p messagingstate.ChatIssuePreview) string { return featuredraft.IssueBody(p) }

// Render delegates the complete protected preview's presentation budget.
func Render(p messagingstate.ChatIssuePreview, project string) (string, error) {
	return featuredraft.Render(p, project)
}

// Drafter adapts chat context, instruction capture and presentation to the
// factory-owned draft operation. It contains no preview admission policy.
type Drafter struct {
	Instructions           prompts.Resolver
	InstructionDiagnostics prompts.Sink
	Repositories           ask.RepositoryReader
	History                ask.HistoryReader
	Model                  ask.Model
	Redactor               *redact.Redactor
	Store                  PreviewStore
	Now                    func() time.Time
	Bind                   func(context.Context, int64) error
	Protect                func(context.Context, string) (string, error)
}

// Factory supplies primitive content and effect capabilities. The returned
// service makes every project, preview and durable-write decision itself.
func (d *Drafter) Factory() *featuredraft.Service {
	service := &featuredraft.Service{
		Instructions: func(ctx context.Context) (featuredraft.Instructions, error) {
			instructions, err := prompts.Load(ctx, d.Instructions, prompts.Draft, d.InstructionDiagnostics)
			return featuredraft.Instructions{Main: instructions.Main, Correction: instructions.Correction}, err
		},
		Repositories: d.Repositories, Redactor: d.Redactor, Store: d.Store,
		Now: d.Now, Bind: d.Bind, Protect: d.Protect,
	}
	if d.Model != nil {
		service.Model = modelAdapter{d.Model}
	}
	if d.History != nil {
		service.History = func(ctx context.Context, channel string) (featuredraft.History, error) {
			history, err := d.History.ReadAskHistory(ctx, channel)
			var turns []featuredraft.Turn
			if history.Turns != nil {
				turns = make([]featuredraft.Turn, len(history.Turns))
				for i, turn := range history.Turns {
					turns[i] = featuredraft.Turn{Question: turn.Question, Project: turn.Project, Answer: turn.Answer, At: turn.At, SourceID: turn.SourceID, Revision: turn.Revision}
				}
			}
			return featuredraft.History{Turns: turns}, err
		}
	}
	return service
}

// Reply presents the factory response without reinterpreting a refusal.
func (d *Drafter) Reply(ctx context.Context, transport, channel, message, project, note string, at time.Time) (string, int64, error) {
	result, err := d.Factory().Reply(ctx, transport, channel, message, project, note, at)
	presentation.Assess(ctx, presentation.Status(result.Status))
	return result.Text, result.RepoID, err
}

type modelAdapter struct{ model ask.Model }

func (m modelAdapter) Answer(ctx context.Context, repoID int64, prompt string) ([]byte, error) {
	raw, err := m.model.Answer(ctx, repoID, prompt)
	if errors.Is(err, ask.ErrUnavailable) {
		return nil, featuredraft.ErrUnavailable
	}
	return raw, err
}
