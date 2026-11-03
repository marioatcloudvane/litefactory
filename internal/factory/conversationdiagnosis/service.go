// Package conversationdiagnosis owns queued diagnosis admission, recovery and publication.
package conversationdiagnosis

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/factory/localdoctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"log/slog"
	"strconv"
	"time"
)

// Diagnoser supplies the existing read-only diagnostic invocation.
type Diagnoser interface {
	Diagnose(context.Context, doctor.DiagnoseRequest) (doctor.Conversation, error)
}

// Input carries protected conversation content and transport access observations.
type Input struct {
	Note       string
	Temporary  bool
	RetryAfter time.Duration
	Err        error
}

// Options supplies primitive execution, presentation and conversation effects.
// The trusted host owns capacity and joins every worker admitted through Start.
type Options struct {
	Try         func() bool
	Done        func()
	Start       func(func())
	TakeNote    func(string) string
	RestoreNote func(string, string)
	Input       func(context.Context, store.ChatReply, time.Time) Input
	ReadIssue   func(context.Context, github.Repo, int) (github.Issue, error)
	Diagnoser   Diagnoser
	Render      func(context.Context, doctor.Conversation) (string, error)
	Notify      func()
	Push        func(context.Context, store.ChatReply, string, time.Time) error
	SessionID   func(context.Context) int64
	Connections *projectconnection.Factory
	Redactor    *redact.Redactor
	OwnerID     int64
	Now         func() time.Time
	Logger      *slog.Logger
}

// Service keeps all workflow persistence and publication decisions in core.
type Service struct {
	db      *store.DB
	options Options
}

// New binds trusted capabilities; consumers receive only bounded operations.
func New(db *store.DB, options Options) *Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, options: options}
}
func sourceForDiagnosis(source string, attempt int) string {
	if attempt > 1 {
		return source + ":retry"
	}
	return source
}

// Dispatch claims at most one queued diagnosis and preserves existing execution fences.
func (d *Service) Dispatch(ctx context.Context, at time.Time) {
	if d.options.Try == nil || !d.options.Try() {
		return
	}
	r, err := d.db.ClaimChatDiagnosis(ctx, "discord", at)
	if err != nil {
		d.options.Done()
		if !errors.Is(err, store.ErrNotFound) {
			d.options.Logger.WarnContext(ctx, "chat diagnosis claim failed")
		}
		return
	}
	// This worker shares the doctor's single reserved slot and its shutdown join.
	d.options.Start(func() {
		defer d.options.Done()
		text := "diagnosis failed"
		outcomeStatus := "error"
		source := "chat:" + r.Transport + ":" + r.MessageID
		note := d.options.TakeNote(source)
		conversationAllowed := true
		if r.ConversationTurnID != "" {
			input := d.options.Input(ctx, r, at)
			note = input.Note
			if input.Err != nil {
				if input.Temporary {
					if e := d.db.RequeueChatDiagnosisBusy(context.WithoutCancel(ctx), r, at.Add(max(5*time.Second, input.RetryAfter))); e != nil {
						d.options.Logger.WarnContext(ctx, "conversation diagnosis access retry could not be saved")
					}
					return
				}
				conversationAllowed = false
				text = "The queued diagnosis was not started because its conversation authorization, target or protected content is unavailable."
				outcomeStatus = "refusal"
			}
		}
		if conversationAllowed && r.State != "exhausted" && at.Before(r.CreatedAt.Add(24*time.Hour)) {
			ref, e := naming.ParseRef(r.TargetRef)
			var issue github.Issue
			if e == nil {
				if d.options.ReadIssue == nil {
					e = errors.New("issue lookup unavailable")
				} else {
					issue, e = d.options.ReadIssue(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, ref.Issue)
					if errors.Is(e, github.ErrNotFound) {
						text = "issue was not found; no diagnosis ran"
					}
				}
			}
			if e == nil {
				service := d.options.Diagnoser
				if service != nil {
					instructions := "Explain what is wrong or stuck with this issue and the likely causes"
					if note != "" {
						instructions += "\n\nOwner note:\n" + note
					}
					c, runErr := service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: instructions, SourceID: sourceForDiagnosis(source, r.AnswerAttempts)})
					if errors.Is(runErr, store.ErrDoctorBusy) {
						d.options.RestoreNote(source, note)
						if e := d.db.RequeueChatDiagnosisBusy(context.WithoutCancel(ctx), r, at); e != nil {
							d.options.Logger.WarnContext(ctx, "chat diagnosis requeue failed")
						}
						return
					}
					if ctx.Err() != nil {
						return
					}
					if runErr == nil {
						summary, summaryErr := d.options.Render(ctx, c)
						if summaryErr == nil {
							text = summary
							outcomeStatus = "success"
						}
						if e := d.Present(context.WithoutCancel(ctx), r, issue, c); e != nil {
							d.options.Logger.WarnContext(ctx, "chat diagnosis repair proposal could not be presented on the issue")
						}
					}
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if safe, e := d.options.Redactor.Apply(context.WithoutCancel(ctx), text, 65536); e == nil {
			if e = d.db.CompleteChatDiagnosisResult(context.WithoutCancel(ctx), r.Transport, r.MessageID, r.AnswerAttempts, safe.String(), d.options.Now(), outcomeStatus); e != nil {
				d.options.Logger.WarnContext(ctx, "chat diagnosis completion failed")
			}
		} else {
			d.options.Logger.WarnContext(ctx, "chat diagnosis redaction failed")
		}
		d.options.Notify()
	})
}

// Present hands a chat diagnosis's repair proposal to the doctor's
// issue presentation, the same one a `/doctor` comment gets, so the owner can
// review it there and approve it with `/doctor apply`.
func (d *Service) Present(ctx context.Context, r store.ChatReply, issue github.Issue, c doctor.Conversation) error {
	if c.ProposalID <= 0 || issue.ID <= 0 {
		return nil
	}
	ref, err := naming.ParseRef(c.Conversation.TaskRef)
	if err != nil {
		return err
	}
	repoID := r.ResolvedRepoID
	if repoID == 0 {
		repo, err := d.db.GetRepoBySlug(ctx, ref.Forge, ref.Owner, ref.Repo)
		if err != nil {
			return err
		}
		repoID = repo.ID
	}
	body := "Repair proposed by a diagnosis requested from chat.\n\n" + doctor.RenderConversation(c)
	if c.ApplyAvailable {
		body += "\nTo approve this repair, reply `/doctor apply` on this issue.\n"
	}
	safe, err := d.options.Redactor.Apply(ctx, body, 262144)
	if err != nil {
		return err
	}
	if safe.Truncated() {
		return errors.New("chat repair proposal exceeds publication bound")
	}
	source, err := d.presentationSource(ctx, repoID, issue)
	if err != nil {
		return err
	}
	return d.db.PresentChatDiagnosis(ctx, store.DoctorCommand{Source: source, RepoID: repoID, TaskRef: ref.String(), IssueID: issue.ID, IssueNumber: ref.Issue, ActorID: d.options.OwnerID, CreatedAt: d.options.Now(), Instructions: "Diagnosis requested from chat", ResultBody: safe.String(), ProposalID: c.ProposalID})
}

// presentationSource routes a chat presentation through the issue's ticket
// connection and publishing identity, resolved as for a `/doctor` comment.
// Without a selected ticket connection the default publisher posts it.
func (d *Service) presentationSource(ctx context.Context, repoID int64, issue github.Issue) (*store.DoctorCommandSource, error) {
	if d.options.Connections == nil {
		return nil, nil
	}
	if _, err := d.db.CurrentConnection(ctx, repoID, store.TicketConnection); errors.Is(err, store.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	repo, err := d.db.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	target, err := localdoctor.ResolveTicketTarget(ctx, d.db, repo, issue, d.options.SessionID(ctx))
	if err != nil {
		return nil, err
	}
	clients, err := d.options.Connections.Clients(ctx, target.Connection, target.PublisherID)
	if err != nil {
		return nil, err
	}
	ref := ticket.Ref{Locator: target.Locator, Identity: ticket.Identity{Provider: target.Locator.Provider, Origin: target.Locator.Origin, RemoteID: strconv.FormatInt(issue.ID, 10)}}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	return &store.DoctorCommandSource{ConnectionVersionID: target.Connection.ID, PublisherID: clients.Actor.ID, Ticket: ref}, nil
}

// chatPresentationWait bounds how long a finished conversation diagnosis waits
// for its repair proposal's issue comment, so the channel post can link to it.
const chatPresentationWait = 10 * time.Minute

// Results posts finished conversation diagnoses to their channel
// as soon as they are ready. Each result is queued once as durable notices,
// and rows left by an interrupted daemon are posted on the next pass.
func (d *Service) Results(ctx context.Context, at time.Time) error {
	results, err := d.db.ListConversationDiagnosisResults(ctx, "discord", 16)
	if err != nil {
		return err
	}
	// One unpostable result never holds back the others.
	var failures []error
	for _, r := range results {
		link := ""
		source := "chat:" + r.Transport + ":" + r.MessageID
		p, err := d.db.ReadChatDiagnosisPresentation(ctx, source, source+":retry")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			failures = append(failures, err)
			continue
		}
		if err == nil {
			ref, e := naming.ParseRef(p.TaskRef)
			if e != nil {
				failures = append(failures, e)
				continue
			}
			switch {
			case p.Complete && p.CommentID > 0:
				link = fmt.Sprintf("The proposed repair is posted for review on the issue: %s#issuecomment-%d\nTo approve it, reply `/doctor apply` there.", ref.IssueURL(), p.CommentID)
			case at.Before(r.NextAttemptAt.Add(chatPresentationWait)):
				continue
			default:
				link = "The proposed repair is still being posted on the issue " + ref.IssueURL() + "; once it appears, approve it there with `/doctor apply`."
			}
		}
		failures = append(failures, d.options.Push(ctx, r, link, at))
	}
	return errors.Join(failures...)
}
