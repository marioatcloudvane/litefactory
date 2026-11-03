// Package researchanswers owns research answer admission and saved-note reconciliation.
package researchanswers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// Notes provides primitive observations and publication only; the service owns retry policy.
type Notes interface {
	ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
}

// Identities retains the configured owner and publisher for existing claims.
type Identities struct{ OwnerUserID, BotUserID int64 }

// Options supplies primitive provider effects and protected output capabilities.
type Options struct {
	ReadIssue  func(context.Context, github.Repo, int) (github.Issue, error)
	Notes      Notes
	Identities Identities
	Redactor   *redact.Redactor
	Logger     *slog.Logger
}

// Service exposes bounded answers and reconciliation, never its persistence capability.
type Service struct {
	db         *store.DB
	readIssue  func(context.Context, github.Repo, int) (github.Issue, error)
	notes      Notes
	identities Identities
	redactor   *redact.Redactor
	log        *slog.Logger
}

// New is called only by trusted composition with the unchanged shared store.
func New(db *store.DB, options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, readIssue: options.ReadIssue, notes: options.Notes, identities: options.Identities, redactor: options.Redactor, log: logger}
}

// QuestionToken preserves the published question identity across extraction.
func QuestionToken(key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", sum[:6])
}

type currentQuestion struct {
	question owneractions.Question
	session  store.Session
	instance store.GraphInstance
	wait     store.GraphOwnerWait
	delivery store.ReportDelivery
}

const reviewRefusal = "this is a review, not a question; answer it on the issue"

func (a Service) current(ctx context.Context, ref string) (out currentQuestion, err error) {
	d := a
	out.question.Reason = "there is no open research question"
	out.session, err = d.db.GetSessionByTaskRef(ctx, ref)
	if errors.Is(err, store.ErrNotFound) {
		out.question.Reason = "there is no factory task for this issue"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.instance, err = d.db.LatestSessionGraphInstance(ctx, out.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.wait, err = d.db.GetGraphOwnerWait(ctx, out.instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if out.wait.State != "waiting" {
		out.question.Reason = "already answered"
		if out.wait.State == "cancelled" {
			out.question.Reason = "the research question has ended"
		}
		return out, nil
	}
	pin, e := d.db.GetGraphDefinition(ctx, out.instance.DefinitionID)
	if e != nil {
		return out, e
	}
	if pin.Mode != "research" {
		out.question.Reason = reviewRefusal
		return out, nil
	}
	if out.instance.Status != store.GraphRunning {
		out.question.Reason = "the research run has ended"
		return out, nil
	}
	intent, e := d.db.GetGraphComment(ctx, out.wait.DeliveryKey)
	if e != nil {
		return out, e
	}
	if intent.Kind != "question" {
		out.question.Reason = reviewRefusal
		return out, nil
	}
	outputs, e := d.db.ListGraphResultOutputsBounded(ctx, out.wait.SourceAttemptID, 1<<20, 16)
	if e != nil {
		return out, e
	}
	asks := 0
	for _, output := range outputs {
		var result struct {
			Response string `json:"response"`
		}
		if output.Kind == store.GraphOutputJSON && json.Unmarshal(output.Payload, &result) == nil && result.Response == "ask" {
			asks++
		}
	}
	if asks != 1 {
		out.question.Reason = reviewRefusal
		return out, nil
	}
	out.delivery, err = d.db.GetReportDelivery(ctx, out.wait.DeliveryKey)
	if errors.Is(err, store.ErrNotFound) {
		// A retained question can precede its publication journal. It is not
		// answerable during that normal publication window.
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if !out.delivery.Completed || out.delivery.CommentID < 1 {
		return out, nil
	}
	var candidate struct {
		Questions []struct {
			Text string `json:"text"`
		} `json:"questions"`
	}
	if err = json.Unmarshal(intent.Candidate, &candidate); err != nil {
		return out, err
	}
	var texts []string
	for _, q := range candidate.Questions {
		if strings.TrimSpace(q.Text) != "" {
			texts = append(texts, q.Text)
		}
	}
	if len(texts) == 0 {
		return out, nil
	}
	out.question = owneractions.Question{Text: strings.Join(texts, "\n"), Token: QuestionToken(out.wait.DeliveryKey)}
	return out, nil
}

// Current returns the published research question without exposing workflow rows.
func (a Service) Current(ctx context.Context, ref string) (owneractions.Question, error) {
	out, err := a.current(ctx, ref)
	return out.question, err
}

// Answer protects and atomically claims the current question under its session lock.
func (a Service) Answer(ctx context.Context, ref, token, text string, at time.Time) (owneractions.Question, error) {
	if a.redactor == nil {
		return owneractions.Question{}, errors.New("answer protection unavailable")
	}
	out, err := a.current(ctx, ref)
	if err != nil {
		return owneractions.Question{}, err
	}
	if out.question.Reason != "" {
		return out.question, nil
	}
	refuse := func(reason string) (owneractions.Question, error) {
		out.question.Reason = reason
		return out.question, nil
	}
	if token != out.question.Token {
		return refuse("the question token does not match")
	}
	if strings.TrimSpace(text) == "" || len(text) > 65536 {
		return refuse("answer must contain 1–65536 bytes")
	}
	d := a
	owner := "chat-answer:" + rand.Text()
	locked, err := d.db.AcquireSessionLock(ctx, out.session.ID, owner)
	if err != nil {
		return owneractions.Question{}, err
	}
	if !locked {
		return refuse("the issue is busy; nothing was applied")
	}
	defer func() {
		if e := d.db.ReleaseSessionLock(context.WithoutCancel(ctx), out.session.ID, owner); e != nil {
			d.log.WarnContext(ctx, "chat answer lock release failed")
		}
	}()
	current, err := a.current(ctx, ref)
	if err != nil {
		return owneractions.Question{}, err
	}
	if current.question.Reason != "" {
		return current.question, nil
	}
	if token != current.question.Token {
		current.question.Reason = "the question token does not match"
		return current.question, nil
	}
	parsed, err := naming.ParseRef(ref)
	if err != nil {
		return owneractions.Question{}, err
	}
	issue, err := d.readIssue(ctx, github.Repo{Owner: parsed.Owner, Name: parsed.Repo}, parsed.Issue)
	if err != nil {
		return owneractions.Question{}, err
	}
	if issue.State != "open" {
		return refuse("the issue is closed; nothing was applied")
	}
	safe, err := a.redactor.Apply(ctx, text, 65536)
	if err != nil || safe.Truncated() {
		return owneractions.Question{}, errors.New("answer redaction failed")
	}
	ids := d.identities
	_, err = d.db.ClaimChatResearchReply(ctx, store.GraphOwnerReply{InstanceID: current.instance.ID, OwnerID: ids.OwnerUserID, AuthorID: ids.OwnerUserID, QuestionCommentID: current.delivery.CommentID, TaskRef: ref, SourceRef: ref, Mode: "research", DeliveryKey: current.wait.DeliveryKey, Body: safe.String(), At: at})
	if errors.Is(err, store.ErrGraphOwnerWait) {
		return owneractions.Question{Reason: "already answered"}, nil
	}
	if errors.Is(err, store.ErrGraphInvariant) || errors.Is(err, store.ErrGraphAttemptRunning) {
		q, currentErr := a.Current(ctx, ref)
		if currentErr != nil {
			return owneractions.Question{}, currentErr
		}
		if q.Reason == "" {
			q.Reason = "the question is no longer answerable; nothing was applied"
		}
		return q, nil
	}
	if err != nil {
		return owneractions.Question{}, err
	}
	return owneractions.Question{Reason: "answer recorded"}, nil
}

// Reconcile resumes saved answer-note outcomes without repeating uncertain writes.
func (d Service) Reconcile(ctx context.Context) error {
	waits, err := d.db.ListChatResearchClaims(ctx, 32)
	if err != nil {
		return err
	}
	for _, wait := range waits {
		instance, e := d.db.GetGraphInstance(ctx, wait.InstanceID)
		if e != nil {
			return e
		}
		session, e := d.db.GetSession(ctx, instance.SessionID)
		if e != nil {
			return e
		}
		marker := "<!-- lf:chat-owner key=" + wait.DeliveryKey + " -->"
		body, e := d.redactor.Apply(ctx, "Owner answered from chat: "+wait.ReplyBody+"\n\n"+marker, 262144)
		if e != nil {
			return e
		}
		delivery, e := d.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "chat-owner:" + wait.DeliveryKey, SessionID: session.ID, TriggerID: wait.ReplyID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "chat-owner", Purpose: "owner-answer", BotUserID: d.identities.BotUserID, Body: body.String(), Marker: marker})
		if e != nil {
			return e
		}
		if e = d.deliverAnswerNote(ctx, delivery); e != nil {
			return e
		}
	}
	return nil
}

func (d Service) deliverAnswerNote(ctx context.Context, delivery store.ReportDelivery) error {
	forge := d.notes
	if forge == nil {
		return errors.New("chat issue notes unavailable")
	}
	ref, err := naming.ParseRef(delivery.Destination)
	if err != nil {
		return err
	}
	repo := github.Repo{Owner: ref.Owner, Name: ref.Repo}
	if delivery.CommentID > 0 {
		_, err = d.db.CompleteReportDelivery(ctx, delivery.Key)
		return err
	}
	if delivery.PossiblySent {
		for page := 1; page <= 10; page++ {
			comments, e := forge.ListCommentsPage(ctx, repo, ref.Issue, page, 100)
			if e != nil {
				return e
			}
			for _, c := range comments.Comments {
				if c.Author.ID == delivery.BotUserID && c.Body == delivery.Body {
					if _, e = d.db.ConfirmReportDelivery(ctx, delivery.Key, c.ID, c.HTMLURL); e != nil {
						return e
					}
					_, e = d.db.CompleteReportDelivery(ctx, delivery.Key)
					return e
				}
			}
			if comments.NextPage == 0 {
				break
			}
		}
		return errors.New("chat answer note delivery remains uncertain")
	}
	body, err := d.redactor.Apply(ctx, delivery.Body, 262144)
	if err != nil {
		return err
	}
	if body.String() != delivery.Body || body.Truncated() {
		return errors.New("chat note requires renewed redaction")
	}
	claimed, err := d.db.MarkReportDeliverySent(ctx, delivery.Key)
	if err != nil || !claimed {
		return err
	}
	comment, err := forge.PostComment(ctx, repo, ref.Issue, body)
	if err != nil {
		return err
	}
	if comment.ID < 1 || comment.Author.ID != delivery.BotUserID {
		return errors.New("chat note receipt lacks bot identity")
	}
	if _, err = d.db.ConfirmReportDelivery(ctx, delivery.Key, comment.ID, comment.HTMLURL); err != nil {
		return err
	}
	_, err = d.db.CompleteReportDelivery(ctx, delivery.Key)
	return err
}
