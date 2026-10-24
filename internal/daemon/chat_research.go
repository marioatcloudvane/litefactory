package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ChatDiagnoser deliberately exposes no repair capability.
type ChatDiagnoser interface {
	Diagnose(context.Context, doctor.DiagnoseRequest) (doctor.Conversation, error)
}

type chatResearch struct{ daemon *Daemon }

func (a chatResearch) Queue(_ context.Context, r messaging.DiagnosisRequest) error {
	d := a.daemon
	if d.chat.diagnoser == nil && d.doctor.service == nil {
		return errors.New("diagnosis unavailable")
	}
	d.chat.mu.Lock()
	d.chat.notes[r.SourceID] = r.Note
	d.chat.mu.Unlock()
	return nil
}

func questionToken(key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", sum[:6])
}

type chatQuestion struct {
	question messaging.Question
	session  store.Session
	instance store.GraphInstance
	wait     store.GraphOwnerWait
	delivery store.ReportDelivery
}

const chatReviewRefusal = "this is a review, not a question; answer it on the issue"

func (a chatResearch) current(ctx context.Context, ref string) (out chatQuestion, err error) {
	d := a.daemon
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
		out.question.Reason = chatReviewRefusal
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
		out.question.Reason = chatReviewRefusal
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
		out.question.Reason = chatReviewRefusal
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
	out.question = messaging.Question{Text: strings.Join(texts, "\n"), Token: questionToken(out.wait.DeliveryKey)}
	return out, nil
}

func (a chatResearch) Current(ctx context.Context, ref string) (messaging.Question, error) {
	out, err := a.current(ctx, ref)
	return out.question, err
}

func (a chatResearch) Answer(ctx context.Context, ref, token, text string, at time.Time) (messaging.Question, error) {
	out, err := a.current(ctx, ref)
	if err != nil {
		return messaging.Question{}, err
	}
	if out.question.Reason != "" {
		return out.question, nil
	}
	refuse := func(reason string) (messaging.Question, error) {
		out.question.Reason = reason
		return out.question, nil
	}
	if token != out.question.Token {
		return refuse("the question token does not match")
	}
	if strings.TrimSpace(text) == "" || len(text) > 65536 {
		return refuse("answer must contain 1–65536 bytes")
	}
	d := a.daemon
	owner := "chat-answer:" + rand.Text()
	locked, err := d.db.AcquireSessionLock(ctx, out.session.ID, owner)
	if err != nil {
		return messaging.Question{}, err
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
		return messaging.Question{}, err
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
		return messaging.Question{}, err
	}
	issue, err := d.readIssue(ctx, github.Repo{Owner: parsed.Owner, Name: parsed.Repo}, parsed.Issue)
	if err != nil {
		return messaging.Question{}, err
	}
	if issue.State != "open" {
		return refuse("the issue is closed; nothing was applied")
	}
	safe, err := d.chat.redactor.Apply(ctx, text, 65536)
	if err != nil || safe.Truncated() {
		return messaging.Question{}, errors.New("answer redaction failed")
	}
	ids := d.filter.Identities()
	_, err = d.db.ClaimChatResearchReply(ctx, store.GraphOwnerReply{InstanceID: current.instance.ID, OwnerID: ids.OwnerUserID, AuthorID: ids.OwnerUserID, QuestionCommentID: current.delivery.CommentID, TaskRef: ref, SourceRef: ref, Mode: "research", DeliveryKey: current.wait.DeliveryKey, Body: safe.String(), At: at})
	if errors.Is(err, store.ErrGraphOwnerWait) {
		return messaging.Question{Reason: "already answered"}, nil
	}
	if errors.Is(err, store.ErrGraphInvariant) || errors.Is(err, store.ErrGraphAttemptRunning) {
		q, currentErr := a.Current(ctx, ref)
		if currentErr != nil {
			return messaging.Question{}, currentErr
		}
		if q.Reason == "" {
			q.Reason = "the question is no longer answerable; nothing was applied"
		}
		return q, nil
	}
	if err != nil {
		return messaging.Question{}, err
	}
	return messaging.Question{Reason: "answer recorded"}, nil
}

func sourceForDiagnosis(source string, attempt int) string {
	if attempt > 1 {
		return source + ":retry"
	}
	return source
}

func (d *Daemon) diagnosisSummary(ctx context.Context, c doctor.Conversation) (string, error) {
	if c.Diagnosis == nil {
		return "diagnosis failed", nil
	}
	var findings []string
	for _, f := range c.Diagnosis.Findings {
		text := f.Explanation
		if f.Uncertainty != "" {
			text += "\nUncertainty: " + f.Uncertainty
		}
		findings = append(findings, text)
	}
	summary := messaging.DiagnosisSummary{RepairProposed: len(c.Diagnosis.Proposal) > 0 || len(c.CurrentProposal) > 0 || c.Diagnosis.Repair != nil}
	for _, section := range []struct {
		raw    []string
		target *[]string
	}{{findings, &summary.Findings}, {c.Diagnosis.CoverageGaps, &summary.CoverageGaps}, {c.Diagnosis.Questions, &summary.OpenQuestions}} {
		safe, err := d.chat.redactor.Apply(ctx, strings.Join(section.raw, "\n"), 65536)
		if err != nil {
			return "", err
		}
		*section.target = []string{safe.String()}
	}
	return messaging.RenderDiagnosis(summary, max(0, d.chat.service.ReplyLimit()-128)), nil
}

func (d *Daemon) dispatchChatDiagnosis(ctx context.Context, at time.Time) {
	if d.chat == nil || d.doctor.pool == nil || !d.doctor.pool.try() {
		return
	}
	r, err := d.db.ClaimChatDiagnosis(ctx, "discord", at)
	if err != nil {
		d.doctor.pool.done()
		if !errors.Is(err, store.ErrNotFound) {
			d.log.WarnContext(ctx, "chat diagnosis claim failed")
		}
		return
	}
	// This worker shares the doctor's single reserved slot and its shutdown join.
	go func() {
		defer d.doctor.pool.done()
		text := "diagnosis failed"
		outcomeStatus := "error"
		source := "chat:" + r.Transport + ":" + r.MessageID
		d.chat.mu.Lock()
		note := d.chat.notes[source]
		delete(d.chat.notes, source)
		d.chat.mu.Unlock()
		conversationAllowed := true
		if r.ConversationTurnID != "" {
			var inputErr error
			note, inputErr = d.chat.service.ConversationDiagnosisInput(ctx, r, at)
			if inputErr != nil {
				var access *messaging.ConversationAccessError
				if errors.As(inputErr, &access) && !access.Permanent {
					if e := d.db.RequeueChatDiagnosisBusy(context.WithoutCancel(ctx), r, at.Add(max(5*time.Second, access.RetryAfter))); e != nil {
						d.log.WarnContext(ctx, "conversation diagnosis access retry could not be saved")
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
				if d.forge == nil {
					e = errors.New("issue lookup unavailable")
				} else {
					issue, e = d.readIssue(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, ref.Issue)
					if errors.Is(e, github.ErrNotFound) {
						text = "issue was not found; no diagnosis ran"
					}
				}
			}
			if e == nil {
				service := d.chat.diagnoser
				if service == nil && d.doctor.service != nil {
					service = d.doctor.service
				}
				if service != nil {
					instructions := "Explain what is wrong or stuck with this issue and the likely causes"
					if note != "" {
						instructions += "\n\nOwner note:\n" + note
					}
					c, runErr := service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: instructions, SourceID: sourceForDiagnosis(source, r.AnswerAttempts)})
					if errors.Is(runErr, store.ErrDoctorBusy) {
						d.chat.mu.Lock()
						d.chat.notes[source] = note
						d.chat.mu.Unlock()
						if e := d.db.RequeueChatDiagnosisBusy(context.WithoutCancel(ctx), r, at); e != nil {
							d.log.WarnContext(ctx, "chat diagnosis requeue failed")
						}
						return
					}
					if ctx.Err() != nil {
						return
					}
					if runErr == nil {
						summary, summaryErr := d.diagnosisSummary(ctx, c)
						if summaryErr == nil {
							text = summary
							outcomeStatus = "success"
						}
						if e := d.presentChatProposal(context.WithoutCancel(ctx), r, issue, c); e != nil {
							d.log.WarnContext(ctx, "chat diagnosis repair proposal could not be presented on the issue")
						}
					}
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if safe, e := d.chat.redactor.Apply(context.WithoutCancel(ctx), text, 65536); e == nil {
			if e = d.db.CompleteChatDiagnosisResult(context.WithoutCancel(ctx), r.Transport, r.MessageID, r.AnswerAttempts, safe.String(), time.Now(), outcomeStatus); e != nil {
				d.log.WarnContext(ctx, "chat diagnosis completion failed")
			}
		} else {
			d.log.WarnContext(ctx, "chat diagnosis redaction failed")
		}
		d.chat.service.Notify()
	}()
}

// presentChatProposal hands a chat diagnosis's repair proposal to the doctor's
// issue presentation, the same one a `/doctor` comment gets, so the owner can
// review it there and approve it with `/doctor apply`.
func (d *Daemon) presentChatProposal(ctx context.Context, r store.ChatReply, issue github.Issue, c doctor.Conversation) error {
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
	safe, err := d.chat.redactor.Apply(ctx, body, 262144)
	if err != nil {
		return err
	}
	if safe.Truncated() {
		return errors.New("chat repair proposal exceeds publication bound")
	}
	return d.db.PresentChatDiagnosis(ctx, store.DoctorCommand{RepoID: repoID, TaskRef: ref.String(), IssueID: issue.ID, IssueNumber: ref.Issue, ActorID: d.filter.Identities().OwnerUserID, CreatedAt: time.Now(), Instructions: "Diagnosis requested from chat", ResultBody: safe.String(), ProposalID: c.ProposalID})
}

// chatPresentationWait bounds how long a finished conversation diagnosis waits
// for its repair proposal's issue comment, so the channel post can link to it.
const chatPresentationWait = 10 * time.Minute

// chatDiagnosisResults posts finished conversation diagnoses to their channel
// as soon as they are ready. Each result is queued once as durable notices,
// and rows left by an interrupted daemon are posted on the next pass.
func (d *Daemon) chatDiagnosisResults(ctx context.Context, at time.Time) error {
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
		failures = append(failures, d.chat.service.PushConversationDiagnosis(ctx, r, link, at))
	}
	return errors.Join(failures...)
}

func (d *Daemon) chatResearchNotes(ctx context.Context) error {
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
		body, e := d.chat.redactor.Apply(ctx, "Owner answered from chat: "+wait.ReplyBody+"\n\n"+marker, 262144)
		if e != nil {
			return e
		}
		delivery, e := d.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "chat-owner:" + wait.DeliveryKey, SessionID: session.ID, TriggerID: wait.ReplyID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "chat-owner", Purpose: "owner-answer", BotUserID: d.filter.Identities().BotUserID, Body: body.String(), Marker: marker})
		if e != nil {
			return e
		}
		if e = d.deliverChatAnswerNote(ctx, delivery); e != nil {
			return e
		}
	}
	return nil
}

func (d *Daemon) deliverChatAnswerNote(ctx context.Context, delivery store.ReportDelivery) error {
	forge, ok := d.forge.(interface {
		ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error)
		PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
	})
	if !ok {
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
					if _, e = d.db.ConfirmReportDelivery(ctx, delivery.Key, c.ID); e != nil {
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
	body, err := d.chat.redactor.Apply(ctx, delivery.Body, 262144)
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
	if _, err = d.db.ConfirmReportDelivery(ctx, delivery.Key, comment.ID); err != nil {
		return err
	}
	_, err = d.db.CompleteReportDelivery(ctx, delivery.Key)
	return err
}

var _ messaging.Diagnosis = chatResearch{}
var _ messaging.ResearchAnswers = chatResearch{}
var _ ChatDiagnoser = (*doctor.Service)(nil)
