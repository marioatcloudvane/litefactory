package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type doctorForge interface {
	ListRepositoryCommentsPage(context.Context, github.Repo, time.Time, int, int) (github.RepositoryCommentPage, error)
	ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
}

// doctorLane adds exactly one reserved diagnosis worker beyond workflow capacity
// (CONC-2). It uses the existing pool join; no provider or publication retry loops
// run in workers. Stop is handled on the short tick/intake path independently.
type doctorLane struct {
	service      *doctor.Service
	pool         *pool
	lastTask     string
	lastRepo     int64
	lastDelivery string
}

func (d *Daemon) doctorTick(ctx context.Context, at time.Time) {
	if d.doctor.service == nil {
		return
	}
	if err := d.recoverDoctorRepairs(ctx); err != nil {
		d.log.ErrorContext(ctx, "doctor repair recovery remains pending", slog.Any("error", err))
	}
	if err := d.recoverDoctor(ctx); err != nil {
		d.log.ErrorContext(ctx, "doctor recovery remains pending", slog.Any("error", err))
	}
	if err := d.doctorControls(ctx); err != nil {
		d.log.ErrorContext(ctx, "doctor controls remain pending", slog.Any("error", err))
	}
	d.dispatchDoctor(ctx)
	if _, held := d.backoff.hold(at); held {
		return
	}
	if err := d.discoverDoctor(ctx, at); err != nil {
		d.log.ErrorContext(ctx, "doctor discovery remains pending", slog.Any("error", err))
		var rate *github.RateLimitError
		if errors.As(err, &rate) {
			d.rateLimited(ctx, d.log, at, err)
		}
	}
	if err := d.doctorControls(ctx); err != nil {
		d.log.ErrorContext(ctx, "doctor controls remain pending", slog.Any("error", err))
	}
	d.dispatchDoctor(ctx)
	if _, held := d.backoff.hold(at); held {
		return
	}
	if err := d.publishDoctor(ctx); err != nil {
		d.log.ErrorContext(ctx, "doctor publication remains pending", slog.Any("error", err))
		var rate *github.RateLimitError
		if errors.As(err, &rate) {
			d.rateLimited(ctx, d.log, at, err)
		}
	}
}

// doctorCall reserves a bounded request before background discovery. It shares
// quota observations and token accounting but uses the reserved operational
// share so a depleted background scan cannot wedge the control lane.
func doctorCall[T any](ctx context.Context, d *Daemon, call func(context.Context) (T, error)) (T, error) {
	b := &d.discovery
	b.mu.Lock()
	if b.tokens < 1 || (b.limit > 0 && b.now.Before(b.reset) && b.remaining-b.reserved <= 0) {
		b.mu.Unlock()
		var zero T
		return zero, ErrDiscoveryDeferred
	}
	b.tokens--
	b.reserved++
	r := &discoveryReservation{budget: b, bound: 1, reset: b.reset}
	b.mu.Unlock()
	defer r.finish()
	return call(github.WithRequestObserver(ctx, r.observe))
}

func (d *Daemon) discoverDoctor(ctx context.Context, at time.Time) error {
	forge, ok := d.forge.(doctorForge)
	if !ok {
		return errors.New("doctor forge transport unavailable")
	}
	repos, err := d.db.ListRepos(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return nil
	}
	start := 0
	for start < len(repos) && repos[start].ID <= d.doctor.lastRepo {
		start++
	}
	if start == len(repos) {
		start = 0
	}
	// One bounded repository page per tick plus at most twelve target reads.
	repo := repos[start]
	d.doctor.lastRepo = repo.ID
	scan, err := d.db.DoctorScanWindow(ctx, repo.ID, at)
	if err != nil {
		return err
	}
	if !scan.WindowEnd.After(scan.CompletedAt) {
		return nil
	}
	fr := github.Repo{Owner: repo.Owner, Name: repo.Name}
	since := scan.CompletedAt.Add(-time.Minute)
	if since.Before(scan.ActivatedAt) {
		since = scan.ActivatedAt
	}
	page, err := doctorCall(ctx, d, func(ctx context.Context) (github.RepositoryCommentPage, error) {
		return forge.ListRepositoryCommentsPage(ctx, fr, since, scan.Page, 12)
	})
	if err != nil {
		return err
	}
	ended := false
	for _, entry := range page.Comments {
		c := entry.Comment
		if c.UpdatedAt.After(scan.WindowEnd) {
			ended = true
			continue
		}
		if c.CreatedAt.Before(scan.ActivatedAt) {
			continue
		}
		if _, ok := doctor.ParseCommand(c.Body); !ok {
			continue
		}
		ids := d.filter.Identities()
		if c.Author.ID != ids.OwnerUserID || c.Author.ID == ids.BotUserID || c.Author.IsBot {
			continue
		}
		issue, err := doctorCall(ctx, d, func(ctx context.Context) (github.Issue, error) { return d.forge.GetIssue(ctx, fr, entry.IssueNumber) })
		if err != nil {
			return err
		}
		if issue.Number != entry.IssueNumber || issue.ID <= 0 {
			return errors.New("doctor target is not a verified issue")
		}
		if _, err := d.admitDoctor(ctx, repo, issue, c); err != nil {
			return err
		}
	}
	next := page.NextPage
	if ended {
		next = 0
	}
	return d.db.AdvanceDoctorScan(ctx, scan, next)
}

// admitDoctor is shared by priority discovery and normal ordered consumers.
// Only authenticated diagnostic text is retained; source edits are deduplicated.
func (d *Daemon) admitDoctor(ctx context.Context, repo store.Repo, issue github.Issue, c github.Comment) (bool, error) {
	command, ok := doctor.ParseCommand(c.Body)
	if !ok {
		return false, nil
	}
	ids := d.filter.Identities()
	if c.Author.ID != ids.OwnerUserID || c.Author.ID == ids.BotUserID || c.Author.IsBot {
		return true, nil
	}
	if d.doctor.service == nil {
		return true, errors.New("doctor service unavailable")
	}
	ref := naming.Ref{Forge: naming.ForgeGitHub, Owner: strings.ToLower(repo.Owner), Repo: strings.ToLower(repo.Name), Issue: issue.Number}
	if issue.ID <= 0 || issue.Number <= 0 {
		return true, errors.New("doctor issue identity unavailable")
	}
	safe, err := d.doctor.service.Redactor.Apply(ctx, command.Text, 32768)
	if err != nil {
		return true, err
	}
	if safe.Truncated() {
		return true, errors.New("doctor instruction exceeded redaction budget")
	}
	repairApprovalID := ""
	if command.Operation == "apply" && d.doctor.service.CheckRepairCapability(ctx, ref) == nil {
		repairApprovalID = rand.Text()
	}
	retained, err := d.db.AdmitDoctorCommand(ctx, store.DoctorCommand{RepairApprovalID: repairApprovalID, RepoID: repo.ID, TaskRef: ref.String(), IssueID: issue.ID, IssueNumber: issue.Number, CommentID: c.ID, ActorID: c.Author.ID, CreatedAt: c.CreatedAt, Operation: command.Operation, Instructions: safe.String()})
	if err != nil {
		return true, err
	}
	if command.Operation == "stop" && retained.State == "queued" {
		if err := d.doctor.service.CancelStopped(ctx, ref); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (d *Daemon) doctorControls(ctx context.Context) error {
	runs, err := d.db.OwnerStopsRunning(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		d.inflight.cancel(run.SessionID, run.Status == store.RunRunning)
	}

	for range 16 {
		c, err := d.db.ClaimDoctorCommand(ctx, "", true, d.instanceID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		body := "Use `/doctor <instructions>` to diagnose this issue, or `/doctor stop` to end the conversation."
		switch c.Operation {
		case "stop":
			var stop store.OwnerStop
			if err := json.Unmarshal([]byte(c.ResultBody), &stop); err != nil {
				return err
			}
			if stop.RunID != 0 {
				run, err := d.db.GetRun(ctx, stop.RunID)
				if err != nil {
					return err
				}
				if d.inflight.cancel(run.SessionID, run.Status == store.RunRunning) || run.Status == store.RunRunning {
					return d.db.RequeueDoctorBusy(ctx, c.ID)
				}
			}
			body = doctor.StopSummary(stop)
			if stop.RunID != 0 {
				run, err := d.db.GetRun(ctx, stop.RunID)
				if err != nil {
					return err
				}
				body += " " + doctor.StopRunSummary(run)
			}
			if stop.RunID != 0 || stop.Conversation {
				body += " Reply on this issue to resume; this does not approve a repair."
			}
		case "apply":
			body = "Doctor apply is unavailable. This command did not approve or perform a repair."
		}
		if c.Operation != "stop" {
			body += "\n\nContinue with `/doctor <instructions>`."
		}
		safe, err := d.doctor.service.Redactor.Apply(ctx, body, 262144)
		if err != nil {
			return err
		}
		if safe.Truncated() {
			return errors.New("doctor stop result exceeds publication bound")
		}
		if err := d.db.CompleteDoctorCommand(ctx, c.ID, safe.String(), 0); err != nil {
			return err
		}
	}
	return nil
}
func (d *Daemon) dispatchDoctor(ctx context.Context) {
	if !d.doctor.pool.try() {
		return
	}
	c, err := d.db.ClaimDoctorCommand(ctx, d.doctor.lastTask, false, d.instanceID)
	if err != nil {
		d.doctor.pool.done()
		if !errors.Is(err, store.ErrNotFound) {
			d.log.ErrorContext(ctx, "doctor claim failed", slog.Any("error", err))
		}
		return
	}
	d.doctor.lastTask = c.TaskRef
	// Bounded by one reserved slot; provider context and Quiesce/shutdown join own
	// this goroutine's lifetime. It never waits for a workflow worker.
	go func() {
		defer d.doctor.pool.done()
		if err := d.runDoctor(ctx, c); err != nil {
			d.log.ErrorContext(ctx, "doctor outcome remains pending", slog.Int64("command_id", c.ID), slog.Any("error", err))
		}
	}()
}
func (d *Daemon) runDoctor(ctx context.Context, c store.DoctorCommand) error {
	ref, err := naming.ParseRef(c.TaskRef)
	if err != nil {
		return err
	}
	var conversation doctor.Conversation
	var runErr error
	if c.Operation == "apply" {
		operation, readErr := d.db.ReadDoctorRepair(ctx, c.TaskRef)
		if readErr != nil {
			return readErr
		}
		source := "github-doctor-apply:" + strconv.FormatInt(c.ID, 10)
		if operation.SourceID != source {
			return errors.New("doctor approved operation source mismatch")
		}
		_, runErr = d.doctor.service.Apply(ctx, doctor.ApplyRequest{Target: ref, SourceID: source, Principal: "github-owner:" + strconv.FormatInt(c.ActorID, 10), InboxID: c.ID, ReservedID: operation.ID})
		if runErr == nil {
			_, runErr = d.doctor.service.DispatchRepairRestart(ctx, ref)
		}
		conversation, err = d.doctor.service.ReadConversation(context.WithoutCancel(ctx), ref)
		runErr = errors.Join(runErr, err)
	} else {
		conversation, runErr = d.doctor.service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: c.Instructions, SourceID: c.SourceID()})
	}
	if errors.Is(runErr, store.ErrDoctorBusy) {
		return d.db.RequeueDoctorBusy(context.WithoutCancel(ctx), c.ID)
	}
	body := doctor.RenderConversation(conversation)
	if runErr != nil {
		body = "Doctor diagnosis did not complete: " + runErr.Error() + "\n\n" + body
	}
	safe, err := d.doctor.service.Redactor.Apply(context.WithoutCancel(ctx), body, 262144)
	if err != nil {
		return err
	}
	if safe.Truncated() {
		return errors.New("doctor result exceeds publication bound")
	}
	err = d.db.CompleteDoctorCommand(context.WithoutCancel(ctx), c.ID, safe.String(), conversation.Conversation.ID)
	if errors.Is(err, store.ErrDoctorFenced) {
		return nil
	}
	return err
}

func (d *Daemon) recoverDoctor(ctx context.Context) error {
	if d.doctor.service == nil {
		return nil
	}
	commands, err := d.db.RecoverDoctorCommands(ctx, d.instanceID)
	if err != nil {
		return err
	}
	var failures []error
	for _, c := range commands {
		ref, err := naming.ParseRef(c.TaskRef)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if c.Operation == "apply" {
			// A writing invocation is never replayed after process loss.
			if _, reconcileErr := d.doctor.service.ReconcileRepair(ctx, ref); reconcileErr != nil {
				failures = append(failures, reconcileErr)
				continue
			}
			conversation, readErr := d.doctor.service.ReadConversation(ctx, ref)
			if readErr != nil {
				failures = append(failures, readErr)
				continue
			}
			body := "Doctor repair was interrupted; retained status requires review. No writing-agent replay occurred.\n\n" + doctor.RenderConversation(conversation)
			failures = append(failures, d.db.CompleteDoctorCommand(ctx, c.ID, body, conversation.Conversation.ID))
			continue
		}
		conversation, err := d.doctor.service.RecoverSource(ctx, ref, c.SourceID())
		if errors.Is(err, store.ErrDoctorBusy) {
			continue
		}
		if err == nil {
			body := doctor.RenderConversation(conversation)
			failures = append(failures, d.db.CompleteDoctorCommand(ctx, c.ID, body, conversation.Conversation.ID))
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			failures = append(failures, err)
			continue
		}
		if c.Attempts < 2 {
			failures = append(failures, d.db.RetryDoctorCommand(ctx, c.ID))
		} else {
			failures = append(failures, d.db.CompleteDoctorCommand(ctx, c.ID, "Doctor diagnosis was interrupted twice. Saved findings remain; send a new `/doctor <instructions>` command to continue.", 0))
		}
	}
	return errors.Join(failures...)
}

func (d *Daemon) publishDoctor(ctx context.Context) error {
	forge, ok := d.forge.(doctorForge)
	if !ok {
		return errors.New("doctor publication unavailable")
	}
	commands, err := d.db.DoctorCommandsNeedingReports(ctx)
	if err != nil {
		return err
	}
	for _, c := range commands {
		parts, err := doctor.PresentationParts(c.ResultBody)
		if err != nil {
			return err
		}
		if err := d.db.EnsureDoctorDeliveries(ctx, c, parts, d.filter.Identities().BotUserID); err != nil {
			return err
		}
	}
	deliveries, err := d.db.PendingDoctorDeliveries(ctx, d.doctor.lastDelivery)
	if err != nil {
		return err
	}
	var failures []error
	for _, delivery := range deliveries[:min(len(deliveries), 4)] {
		d.doctor.lastDelivery = delivery.Key
		failures = append(failures, d.deliverDoctor(ctx, forge, delivery))
	}
	return errors.Join(failures...)
}
func (d *Daemon) deliverDoctor(ctx context.Context, forge doctorForge, delivery store.ReportDelivery) error {
	ref, err := naming.ParseRef(delivery.Destination)
	if err != nil {
		return err
	}
	repo := github.Repo{Owner: ref.Owner, Name: ref.Repo}
	if delivery.CommentID > 0 {
		return d.db.CompleteDoctorDelivery(ctx, delivery.Key)
	}
	if delivery.PossiblySent {
		probe, err := d.db.DoctorDeliveryProbe(ctx, delivery.Key)
		if err != nil {
			return err
		}
		page, err := doctorCall(ctx, d, func(ctx context.Context) (github.CommentPage, error) {
			return forge.ListCommentsPage(ctx, repo, ref.Issue, probe, 100)
		})
		if err != nil {
			return err
		}
		for _, c := range page.Comments {
			if c.Author.ID == delivery.BotUserID && strings.Contains(c.Body, delivery.Marker) && c.Body == delivery.Body {
				if _, err := d.db.ConfirmReportDelivery(ctx, delivery.Key, c.ID); err != nil {
					return err
				}
				return d.db.CompleteDoctorDelivery(ctx, delivery.Key)
			}
		}
		return d.db.AdvanceDoctorDeliveryProbe(ctx, delivery.Key, page.NextPage) // Absence is not proof to repost; debt stays uncertain.
	}
	safe, err := d.doctor.service.Redactor.Apply(ctx, delivery.Body, 65536)
	if err != nil {
		return err
	}
	if safe.Truncated() || safe.String() != delivery.Body {
		return errors.New("retained doctor publication requires renewed redaction")
	}
	claimed, err := d.db.MarkReportDeliverySent(ctx, delivery.Key)
	if err != nil || !claimed {
		return err
	}
	comment, err := forge.PostComment(ctx, repo, ref.Issue, safe)
	if err != nil {
		return err
	}
	if comment.ID <= 0 || comment.Author.ID != delivery.BotUserID {
		return fmt.Errorf("doctor post receipt lacks trusted bot identity")
	}
	if _, err := d.db.ConfirmReportDelivery(ctx, delivery.Key, comment.ID); err != nil {
		return err
	}
	return d.db.CompleteDoctorDelivery(ctx, delivery.Key)
}
