package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateForge is the forge Validate's owner steps use: the delivery calls that
// Bug finalization combines today, plus the follow-up issue calls.
type ValidateForge interface {
	BugCodeForge
	ValidateTickets
}

// ValidateTickets contains ticket lifecycle and follow-up publication only.
type ValidateTickets interface {
	BugCloseForge
	ListFollowupIssues(context.Context, github.Repo) ([]github.Issue, error)
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	SetStoryState(ctx context.Context, repo github.Repo, parentNumber int, parentID int64, number int, issueID int64, completed bool) error
	// UpdateIssue writes the ticked criteria into a story body.
	UpdateIssue(context.Context, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
}

// ValidateIssueRef names one issue by number and numeric id. A story issue
// also carries its story key, title and frozen approved revision and body, so
// the report links its row and the story's criteria can be ticked.
type ValidateIssueRef struct {
	Display  ticketDisplay
	Number   int
	ID       int64
	Key      string
	Title    string
	Revision string
	Body     string
}

// ValidateDeliveryTarget is where an accepted run is delivered and which issues
// its close completes: the parent and the accepted story issues.
type ValidateDeliveryTarget struct {
	Task    naming.Ref
	Repo    github.Repo
	Branch  string
	Base    string
	Parent  ValidateIssueRef
	Stories []ValidateIssueRef
	BotID   int64
	// Generation is the plan generation the stories were approved in.
	Generation int
}

// CloseSet is the issue numbers a close command lists: the parent and the
// accepted story issues.
func (t ValidateDeliveryTarget) CloseSet() []int {
	out := []int{t.Parent.Number}
	for _, s := range t.Stories {
		out = append(out, s.Number)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ValidateDelivery supplies the trusted context of the owner steps. Every value
// comes from factory state, never from agent or comment text.
type ValidateDelivery struct {
	Forge    ValidateForge
	Code     BugCodeForge
	Tickets  ValidateTickets
	Redactor *redact.Redactor
	// Target names the repository, branch, base, parent and accepted stories.
	Target func(ctx context.Context, opts Options, req ActivityRequest) (ValidateDeliveryTarget, error)
	// Head reads the current head of the feature branch in the task worktree.
	Head func(ctx context.Context) (string, error)
	// Publish pushes the tested commit to the feature branch, without force and
	// only while the remote branch is still at expectedHead ("" when absent).
	Publish func(ctx context.Context, commit, expectedHead string) error
	// OwnerID is the resolved numeric owner identity.
	OwnerID func(ctx context.Context, opts Options) (int64, error)
}

func (d ValidateDelivery) ready() bool {
	return d.code() != nil && d.tickets() != nil && d.Redactor != nil && d.Target != nil && d.Head != nil && d.Publish != nil && d.OwnerID != nil
}

// validateWorkflowComment judges the one comment the Validate graph publishes
// as its owner wait: a question about what is open.
func validateWorkflowComment(_ store.GraphNodeAttempt, kind string, candidate agentresult.Result, scope agentresult.Context) error {
	if kind != "question" {
		return fmt.Errorf("the Validate graph publishes no %s comment", kind)
	}
	if scope.Mode != "validate" {
		return errors.New("a Validate question belongs to a Validate invocation")
	}
	if candidate.Outcome != "incomplete" || strings.TrimSpace(candidate.Comment) == "" || strings.TrimSpace(candidate.Reason) == "" {
		return errors.New("a Validate question is an incomplete result with a comment and a reason")
	}
	return nil
}

// renderValidateComment lays out the owner wait: where the run stands, what is
// open and the exact commands.
func renderValidateComment(candidate agentresult.Result, _ string, _ naming.Ref, _ string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(candidate.Comment))
	b.WriteString("\n\n")
	for _, q := range candidate.Questions {
		b.WriteString("- " + strings.TrimSpace(q.Text) + "\n")
	}
	b.WriteString("\n" + strings.TrimSpace(candidate.Reason))
	return b.String()
}

// ValidateReportActivity keeps the one report comment current. It never changes
// workflow state, and a failed post or edit never fails the step: the records
// are the truth and the next refresh catches up.
type ValidateReportActivity struct{ b ValidateFlowBindings }

// Execute refreshes the report.
func (h ValidateReportActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if _, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateReportAction); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	refreshed, err := h.b.RefreshValidateReport(ctx, opts, req)
	payload := map[string]any{"refreshed": refreshed}
	if err != nil {
		payload["error"] = clipText(err.Error(), 300)
		if opts.Logger != nil {
			opts.Logger.Warn("validate report refresh failed; the next step catches up", slog.Int64("instance_id", req.InstanceID), slog.Any("error", err))
		}
	}
	return validateJSONResult("done", payload)
}

// RefreshValidateReport builds the report from the records and posts or edits
// the one comment. It reports whether the comment changed.
func (b ValidateFlowBindings) RefreshValidateReport(ctx context.Context, opts Options, req ActivityRequest) (bool, error) {
	if opts.ResearchReporter == nil {
		return false, errors.New("validate report requires the factory reporter")
	}
	st, err := LoadValidateState(ctx, opts, b.Profile, req.InstanceID)
	if err != nil {
		return false, err
	}
	var target *ValidateDeliveryTarget
	if b.Delivery.Target != nil {
		if t, err := b.Delivery.Target(ctx, opts, req); err == nil {
			target = &t
		}
	}
	report, err := opts.DB.GetValidateReport(ctx, req.InstanceID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	text := renderValidateReport(st, target, report.Notice)
	// The story sub-issues follow the parent report: they are written only when
	// the report changed, and a failure returns before the report row is saved,
	// so the next refresh retries them.
	if target != nil && (report.CommentID == 0 || report.Digest != fmt.Sprintf("%x", sha256.Sum256([]byte(text)))) {
		if err := b.refreshValidateStories(ctx, opts, req, st, *target); err != nil {
			return false, err
		}
	}
	return opts.ResearchReporter.ReportValidate(ctx, req.InstanceID, st.Revision, report.Notice, text, b.now(opts))
}

// ReportValidate posts the Validate report once under a stable identity and then
// edits it in place. An unchanged digest skips the edit. The report row keeps
// the comment id, the revision and the last rendered digest.
func (r *Reporter) ReportValidate(ctx context.Context, instanceID int64, revision, notice, text string, now time.Time) (bool, error) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	saved, err := r.db.GetValidateReport(ctx, instanceID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if err == nil && saved.Digest == digest && saved.CommentID != 0 {
		return false, nil
	}
	instance, err := r.db.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return false, err
	}
	session, err := r.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return false, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return false, err
	}
	intent := r.deliveryIntent(ref, ref, session.ID, 0, 0, fmt.Sprintf("validate-report:%d", instanceID))
	delivery, err := r.upsertValidateComment(ctx, intent, ref, text)
	if err != nil {
		return false, err
	}
	return true, r.db.PutValidateReport(ctx, store.ValidateReport{InstanceID: instanceID, CommentID: delivery.CommentID, Marker: intent.Marker, Revision: revision, Digest: digest, Notice: notice, UpdatedAt: now})
}

// upsertValidateComment posts a Validate comment once under its delivery
// identity and then edits it in place.
func (r *Reporter) upsertValidateComment(ctx context.Context, intent store.ReportDelivery, destination naming.Ref, text string) (store.ReportDelivery, error) {
	// A factory notice to the owner answers no input, so completing it consumes
	// nothing. Plan notices are the same kind of delivery.
	intent.TriggerNamespace = store.PlanNoticeNamespace
	delivery, err := r.db.GetReportDelivery(ctx, intent.Key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if _, err = r.deliver(ctx, intent, func() (redact.Body, error) { return r.renderer.RenderReportDelivery(ctx, text, "") }); err != nil {
			return delivery, err
		}
		return r.db.GetReportDelivery(ctx, intent.Key)
	case err != nil:
		return delivery, err
	}
	if delivery.CommentID == 0 || !delivery.Completed {
		if _, err = r.dispatchDelivery(ctx, delivery); err != nil {
			return delivery, err
		}
		if delivery, err = r.db.GetReportDelivery(ctx, intent.Key); err != nil {
			return delivery, err
		}
	}
	if delivery.CommentID == 0 {
		return delivery, errors.New("the validate comment is not confirmed yet")
	}
	editor, ok := r.forge.(CommentEditor)
	if !ok {
		return delivery, errors.New("the validate comment cannot be edited in place")
	}
	body, err := r.renderer.RenderReportDelivery(ctx, text, intent.Marker)
	if err != nil {
		return delivery, err
	}
	return delivery, editor.EditComment(ctx, github.RepoFromRef(destination), delivery.CommentID, body)
}

// saveValidateNotice keeps the last refusal (or clears it) on the report row
// without touching the comment id or the rendered digest.
func saveValidateNotice(ctx context.Context, opts Options, instanceID int64, revision, notice string, at time.Time) error {
	saved, err := opts.DB.GetValidateReport(ctx, instanceID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	saved.InstanceID, saved.Revision, saved.Notice, saved.UpdatedAt = instanceID, revision, notice, at
	return opts.DB.PutValidateReport(ctx, saved)
}

// ValidateAskActivity publishes the one owner wait. Its source is the resolve
// step, so the owner's reply re-enters the run at the resolve step.
type ValidateAskActivity struct{ b ValidateFlowBindings }

// Execute posts the question and opens the owner wait.
func (h ValidateAskActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateAskAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if opts.ResearchReporter == nil {
		return ActivityResult{}, errors.New("validate wait requires the factory reporter")
	}
	source, err := opts.DB.LatestGraphNodeAttempt(ctx, req.InstanceID, step.node.Attrs["lf_source"])
	if err != nil {
		return ActivityResult{}, fmt.Errorf("find the step the owner's reply resumes: %w", err)
	}
	if source.Status != store.GraphSucceeded {
		return ActivityResult{}, errors.New("the step the owner's reply resumes has not finished")
	}
	st, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	var closeSet []int
	if h.b.Delivery.Target != nil {
		if t, e := h.b.Delivery.Target(ctx, opts, req); e == nil {
			closeSet = t.CloseSet()
		}
	}
	report, err := opts.DB.GetValidateReport(ctx, req.InstanceID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	candidate := validateQuestionCandidate(st, closeSet, report.Notice)
	policy := ValidateWorkflowPolicy()
	scope := policy.baseContext(true)
	posted, key, err := opts.ResearchReporter.ReportWorkflowCandidate(ctx, source, req.Node, "question", candidate, scope, "", policy)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	payload, err := json.Marshal(struct {
		DeliveryKey string `json:"delivery_key"`
		CommentID   int64  `json:"comment_id"`
		Revision    string `json:"revision"`
	}{key, posted.CommentID, st.Revision})
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: payload}, nil
}

// validateQuestionCandidate is the owner wait: what the run needs from the
// owner, with the refusal of the last command and the exact commands to give.
func validateQuestionCandidate(st ValidateState, closeSet []int, notice string) agentresult.Result {
	var comment strings.Builder
	fmt.Fprintf(&comment, "Validate is waiting for you on commit `%s`. The report comment shows the full state; its revision is `%s`.", short(st.Commit), st.Revision)
	if strings.TrimSpace(notice) != "" {
		comment.WriteString("\n\n**Your last command changed nothing:** " + strings.TrimSpace(notice))
	}
	reason := "Next: " + st.NextStep(closeSet) + "\n\nNothing is pushed, merged or closed until you give the command for it. One confirmation never implies the next."
	c := agentresult.Result{Outcome: "incomplete", Comment: comment.String(), Reason: reason, Payload: agentresult.Payload{Validate: &agentresult.Validate{}}}
	for i, o := range st.Open {
		if i >= 12 {
			c.Questions = append(c.Questions, agentresult.Question{ID: "q-more", Audience: "business", Text: fmt.Sprintf("%d more open items are listed in the report.", len(st.Open)-i)})
			break
		}
		c.Questions = append(c.Questions, agentresult.Question{ID: fmt.Sprintf("q%d", i+1), Audience: "business", Text: o})
	}
	return c
}

func (d ValidateDelivery) code() BugCodeForge {
	if d.Code != nil {
		return d.Code
	}
	return d.Forge
}
func (d ValidateDelivery) tickets() ValidateTickets {
	if d.Tickets != nil {
		return d.Tickets
	}
	return d.Forge
}

func (s ValidateIssueRef) display() ticketDisplay {
	if s.Display.Label != "" {
		return s.Display
	}
	return ticketDisplay{Label: fmt.Sprintf("#%d", s.Number)}
}
