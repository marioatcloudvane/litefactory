package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// NoticeEvent is a transport-neutral projection of a durable factory event.
type NoticeEvent struct {
	Key, Kind, Workflow, From, To, Question, QuestionLink string
	// Title is the issue title read live for the notice; Asked names what an
	// owner wait asks for (question or review).
	Title, Asked         string
	DisplayKey, IssueURL string
	NeedsGuidance        bool
	Task                 naming.Ref
	RunID                int64
	At                   time.Time
	Usage                *report.MeterSummary
}

// NoticeEvents maps source facts to stable event identities. Graph node
// transitions and same-mode follow-on runs are intentionally silent.
func NoticeEvents(source string, r store.NoticeRecord) ([]NoticeEvent, error) {
	parked := source == "owner_waits" && r.StopOrigin != "" && r.StopCause != "diagnosis_question" && r.StopCause != "diagnosis_review"
	if r.Ignore || (!r.Ready && !parked) {
		return nil, nil
	}
	ref, err := naming.ParseRef(r.TaskRef)
	if err != nil {
		return nil, err
	}
	e := NoticeEvent{Task: ref, Workflow: r.Workflow, RunID: r.Run.ID, At: r.At}
	switch source {
	case "run_starts":
		// A further run in the same mode continues work already announced,
		// including a run recovered after a daemon restart.
		if r.PreviousMode != "" && r.PreviousMode == r.Workflow {
			return nil, nil
		}
		e.Key = fmt.Sprintf("run:%d:start", r.ID)
		e.Kind = "started"
		if r.PreviousMode != "" {
			e.From = r.PreviousMode
			e.To = r.Workflow
		}
	case "run_finishes":
		e.Key = fmt.Sprintf("run:%d:finish", r.ID)
		e.Kind = "failed"
		if _, ok := r.Run.OwnerStop(); ok {
			e.Kind = "stopped"
		} else if strings.HasPrefix(r.Run.Error, "abandoned:") {
			// The daemon abandoned the run in process, as on shutdown;
			// recovery relaunches it silently, so it is not a failure.
			return nil, nil
		} else if r.Run.Succeeded() {
			e.Kind = "completed"
		}
	case "transitions":
		// Graph node moves stay within one graph instance, and so within one
		// mode; they are factory internals, never owner notices.
		return nil, nil
	case "owner_waits":
		// Only a published wait that still waits for the owner is announced.
		published := r.State == "waiting" && r.Published
		e.Key = "wait:" + r.Key + "@" + store.FormatTime(r.At) + "@" + r.TaskRef
		e.Kind = "waiting for you"
		// Asked names the wait in plain words. A review-ready Bug analysis is
		// read on GitHub; its full text never enters the notice.
		switch r.StopCause {
		case "diagnosis_review":
			e.Asked = "Bug analysis ready for your approval"
		case "diagnosis_question":
			e.Asked = "Bug analysis question"
		default:
			e.Asked = noticeAsked(r.Workflow, r.Kind)
		}
		if r.Kind == "question" && r.StopCause != "diagnosis_review" {
			e.Question, err = noticeQuestion(r.Candidate, r.StopReason)
			if err != nil {
				return nil, err
			}
		}
		if ref.Forge == "github" && r.CommentID > 0 {
			e.QuestionLink = fmt.Sprintf("%s#issuecomment-%d", ref.IssueURL(), r.CommentID)
		}
		if parked {
			stopped := e
			stopped.Key = "stop:" + r.Key + "@" + store.FormatTime(r.At) + "@" + r.TaskRef
			// An owner-cancelled run and its parked graph are the same stop.
			// Other graph parking is a separate event from the source run finish.
			if _, ownerStop := r.Run.OwnerStop(); ownerStop {
				stopped.Key = fmt.Sprintf("run:%d:finish", r.Run.ID)
			}
			stopped.Kind = "stopped"
			if _, ownerStop := r.Run.OwnerStop(); !ownerStop {
				stopped.NeedsGuidance = true
			}
			stopped.Question = ""
			stopped.QuestionLink = ""
			stopped.Asked = ""
			if published {
				return []NoticeEvent{stopped, e}, nil
			}
			return []NoticeEvent{stopped}, nil
		}
		if !published {
			return nil, nil
		}
	default:
		return nil, errors.New("unknown notice source")
	}
	return []NoticeEvent{e}, nil
}

func noticeQuestion(candidate []byte, reason string) (string, error) {
	var text struct {
		Questions []struct {
			Text string `json:"text"`
		} `json:"questions"`
		Reason string `json:"reason"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(candidate, &text); err != nil {
		return "", fmt.Errorf("notice question: %w", err)
	}
	parts := []string{}
	for _, q := range text.Questions {
		if q.Text != "" {
			parts = append(parts, q.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n"), nil
	}
	if reason != "" {
		return reason, nil
	}
	if text.Reason != "" {
		return text.Reason, nil
	}
	if text.Text != "" {
		return text.Text, nil
	}
	return "Owner input requested.", nil
}

// NoticeText keeps a notice to a few lines: what happened and to which issue,
// one link, and at most one short sentence of an owner question. Details are
// read on the forge. The budget counts UTF-16 units used by chat APIs.
func NoticeText(e NoticeEvent) string {
	key := e.DisplayKey
	if key == "" {
		key = "task"
		if e.Task.Forge == "github" {
			key = fmt.Sprintf("#%d", e.Task.Issue)
		}
	}
	workflow := map[string]string{"bug": "Bug", "research": "Research", "plan": "Plan", "implement": "Implementation", "validate": "Validation", "review": "Review", "fix": "Fix", "spike": "Spike"}[e.Workflow]
	if workflow == "" {
		workflow = "Work on"
	}
	action := " started"
	switch e.Kind {
	case "completed":
		action = " finished"
	case "failed":
		action = " failed"
	case "stopped":
		action = " stopped"
		if e.NeedsGuidance {
			action += " and needs your guidance"
		}
	case "waiting for you", "question":
		action = " needs your input"
		if strings.Contains(strings.ToLower(e.Asked), "review") {
			action = " is ready for your review"
		}
		if strings.Contains(e.Asked, "approval") {
			action = " has a Bug analysis ready for your approval"
		}
		if q := noticeSentence(e.Question); q != "" {
			action = " has a question for you: " + noticeLimit(q, noticeQuestionLimit)
		}
	}
	if !strings.HasSuffix(action, "?") && !strings.HasSuffix(action, "!") && !strings.HasSuffix(action, ".") {
		action += "."
	}
	if e.Usage != nil && (e.Kind == "completed" || e.Kind == "failed" || e.Kind == "stopped") {
		if cost := noticeUsage(e.Usage); cost != "" {
			action += " " + cost
		}
	}
	link := e.IssueURL
	if link == "" {
		link = e.Task.IssueURL()
	}
	if e.QuestionLink != "" {
		link = e.QuestionLink
	}
	if e.Kind == "failed" || e.Kind == "stopped" {
		action += " Open the issue below to inspect its retained report before requesting more work."
	}
	suffix := action
	if link != "" {
		suffix += "\n" + link
	}
	prefix := workflow + " " + key
	title := strings.Join(strings.Fields(e.Title), " ")
	if title != "" {
		title = noticeLimit(title, min(240, max(0, 1900-noticeUnits(prefix+suffix)-3)))
		if title != "" {
			prefix += " (" + title + ")"
		}
	}
	return prefix + suffix
}

// noticeQuestionLimit caps the one sentence of a question a notice shows.
const noticeQuestionLimit = 160

// noticeAsked names a generic wait, for example "Research question".
func noticeAsked(workflow, kind string) string {
	if workflow == "" {
		return kind
	}
	return strings.ToUpper(workflow[:1]) + workflow[1:] + " " + kind
}

// noticeSentence keeps the first line of the text, cut after its first
// sentence.
func noticeSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	for i := 0; i+1 < len(s); i++ {
		if (s[i] == '.' || s[i] == '?' || s[i] == '!') && s[i+1] == ' ' {
			return s[:i+1]
		}
	}
	return s
}
func noticeUnits(s string) int { return len(utf16.Encode([]rune(s))) }
func noticeLimit(s string, limit int) string {
	if noticeUnits(s) <= limit {
		return s
	}
	if limit < 1 {
		return ""
	}
	var b strings.Builder
	units := 0
	for _, r := range s {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > limit-1 {
			break
		}
		b.WriteRune(r)
		units += n
	}
	clipped := b.String()
	// Prefer a complete word; a single oversized word still has a bounded fallback.
	if i := strings.LastIndexFunc(clipped, unicode.IsSpace); i > 0 && !unicode.IsSpace([]rune(s)[len([]rune(clipped))]) {
		clipped = clipped[:i]
	}
	return strings.TrimSpace(clipped) + "…"
}

// noticeUsage is one short cost phrase, or nothing when no cost is known.
func noticeUsage(s *report.MeterSummary) string {
	value := s.EstimatedUSD
	if value == nil {
		return ""
	}
	usd, err := strconv.ParseFloat(*value, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("≈ $%.2f", usd)
}

// NoticeCollector reads existing tables only and writes chat-owned state. It
// runs solely in the sender goroutine; failures never enter run execution.
type NoticeCollector struct {
	DB                 *store.DB
	Transport, Channel string
	Logger             *slog.Logger
	Protect            func(context.Context, string) (string, error)
	// Issues reads public issue metadata live; nil or a failed read omits it.
	Issues NoticeIssues
}

// NoticeIssues reads provider metadata through the existing issue boundary.
type NoticeIssues interface {
	GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error)
}

// Collect does nothing when notices are off. Each source is independent: an
// error in one source cannot keep other sources from collecting their events.
func (c *NoticeCollector) Collect(ctx context.Context, at time.Time) error {
	if c.Channel == "" {
		return nil
	}
	if err := c.DB.InitNoticePositions(ctx, c.Transport, at); err != nil {
		return err
	}
	var errs []error
	titles := make(map[string]github.Issue)
	for _, source := range []string{"run_starts", "run_finishes", "transitions", "owner_waits"} {
		if err := c.collectSource(ctx, source, at, titles); err != nil {
			errs = append(errs, fmt.Errorf("collect %s: %w", source, err))
		}
	}
	return errors.Join(errs...)
}
func (c *NoticeCollector) collectSource(ctx context.Context, source string, at time.Time, titles map[string]github.Issue) error {
	before, records, err := c.DB.ListNoticeRecords(ctx, c.Transport, source, at)
	if err != nil {
		return err
	}

	after := before
	after.Pending = []int64{}
	after.Seen = make(map[string]string)
	for id, fingerprint := range before.Seen {
		_, stamp, ok := strings.Cut(fingerprint, "@")
		if !ok {
			continue
		}
		eventAt, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return err
		}
		if at.Sub(eventAt) <= 24*time.Hour {
			after.Seen[id] = fingerprint
		}
	}
	notices := []store.ChatNotice{}
	for _, r := range records {
		after.LastID = max(after.LastID, r.ID)
		if source == "transitions" || source == "owner_waits" {
			after.Seen[strconv.FormatInt(r.ID, 10)] = r.TaskRef + "@" + store.FormatTime(r.At)
		}
		if !r.Ready {
			after.Pending = append(after.Pending, r.ID)
			if r.StopOrigin == "" {
				continue
			}
		}
		if !r.Ignore && at.Sub(r.At) > 24*time.Hour {
			if c.Logger != nil && r.Ready {
				c.Logger.WarnContext(ctx, "skipping stale chat notice", slog.String("source", source), slog.Int64("record_id", r.ID))
			}
			continue
		}

		events, err := NoticeEvents(source, r)
		if err != nil {
			return err
		}
		for _, event := range events {
			// A parked stop is already history at first enable; its future question
			// publication can still be noticed through the unresolved wait.
			if source == "owner_waits" && event.Kind == "stopped" && r.ID <= before.InitialID && !r.At.After(before.InitialAt) {
				continue
			}
			if event.RunID > 0 && (event.Kind == "completed" || event.Kind == "failed" || event.Kind == "stopped") {
				event.Usage, err = c.runUsage(ctx, event.RunID)
				if err != nil {
					return err
				}
			}
			issue := c.issueMetadata(ctx, event.Task, titles)
			event.Title, event.IssueURL = issue.Title, issue.HTMLURL
			if snapshot := issue.QualifiedSnapshot; snapshot != nil {
				event.DisplayKey, event.Title, event.IssueURL = snapshot.DisplayKey, snapshot.Title, snapshot.BrowserURL
			}
			if event.Kind == "waiting for you" && event.Task.Forge == "github" && event.IssueURL != "" && r.CommentID > 0 {
				event.QuestionLink = fmt.Sprintf("%s#issuecomment-%d", event.IssueURL, r.CommentID)
			}
			text := NoticeText(event)
			if c.Protect != nil {
				text, err = c.Protect(ctx, text)
				if err != nil {
					return err
				}
			}
			notices = append(notices, store.ChatNotice{TaskRef: event.Task.String(), ChatReply: store.ChatReply{Transport: c.Transport, MessageID: event.Key, ChannelRef: c.Channel, Command: event.Kind, CreatedAt: at, ReplyText: text}})
		}
	}
	if len(records) == 0 && len(before.Seen) == len(after.Seen) {
		return nil
	}
	return c.DB.AdmitNotices(ctx, c.Transport, before, after, notices)
}

// issueMetadata reads each task once per Collect pass. A missing reader
// or a failed read yields no title, and the notice still posts.
func (c *NoticeCollector) issueMetadata(ctx context.Context, ref naming.Ref, titles map[string]github.Issue) github.Issue {
	if c.Issues == nil {
		return github.Issue{}
	}
	key := ref.String()
	if title, ok := titles[key]; ok {
		return title
	}
	issue, err := c.Issues.GetIssue(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, ref.Issue)
	if err != nil {
		if c.Logger != nil {
			c.Logger.WarnContext(ctx, "chat notice issue title unavailable", slog.String("task", key), slog.Any("error", err))
		}
		issue = github.Issue{}
	}
	titles[key] = issue
	return issue
}

func (c *NoticeCollector) runUsage(ctx context.Context, runID int64) (summary *report.MeterSummary, err error) {
	snapshot, err := c.DB.NewMeterSnapshot(ctx, store.MeterFilter{RunID: runID}, store.MeterSnapshotOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	for {
		rows, e := snapshot.Next(ctx, 256)
		if e != nil {
			return nil, e
		}
		if len(rows) == 0 {
			break
		}
		if summary == nil {
			summary = &report.MeterSummary{}
		}
		for _, row := range rows {
			if e = summary.Add(report.MeterValues{Tokens: agent.TokenUsage(row.Tokens), EstimatedUSD: row.EstimatedUSD, PricedSubtotalUSD: row.PricedSubtotalUSD, PriceRevisionID: row.PriceRevisionID, CollectionStatus: row.CollectionStatus, PricingStatus: row.PricingStatus, Diagnostics: row.Diagnostics}); e != nil {
				return nil, e
			}
		}
	}
	return summary, nil
}

// NoticeStep uses the command sender's retry policy and neutral transport. The
// event key becomes the channel post's nonce key, with no interaction handle.
func (s *Service) NoticeStep(ctx context.Context, at time.Time) (bool, error) {
	r, err := s.db.ClaimChatNotice(ctx, s.transport, at)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	failed := !at.Before(r.CreatedAt.Add(24 * time.Hour))
	if !failed {
		d := s.sender.Deliver(ctx, Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef, Notice: true}, r.ReplyText)
		if !d.Confirmed && !d.Permanent {
			if s.Logger != nil {
				s.Logger.WarnContext(ctx, "chat notice delivery failed; retrying", slog.String("event_key", r.MessageID))
			}
			return true, s.db.RetryChatNotice(ctx, r, retryAt(r, at, d.RetryAfter), at)
		}
		failed = !d.Confirmed
	}
	err = s.db.FinishChatNotice(ctx, r, failed, at)
	if err == nil && failed && s.Logger != nil {
		s.Logger.WarnContext(ctx, "chat notice failed permanently or exceeded its retry window", slog.String("event_key", r.MessageID))
	}
	return true, err
}

// Announce queues a factory message for one channel. It is split into
// message-sized parts and delivered in order by NoticeStep; key identifies the
// message, so queuing the same key again changes nothing. operation, when set,
// is the key of the confirmed operation the message reports on: the message
// waits until the conversation has posted that operation's confirmation.
func (s *Service) Announce(ctx context.Context, channel, key, operation, text string, at time.Time) error {
	notices, err := s.announcement(ctx, channel, key, operation, text, "", at)
	if err != nil {
		return err
	}
	return s.db.QueueChatNotices(ctx, s.transport, notices)
}

// announcement protects and splits one factory message into its notices. A
// verbatim factory line, such as a link, follows as its own unescaped message.
func (s *Service) announcement(ctx context.Context, channel, key, operation, text, verbatim string, at time.Time) ([]store.ChatNotice, error) {
	if s.Protect != nil {
		protected, err := s.Protect(ctx, text)
		if err != nil {
			return nil, err
		}
		text = protected
		if verbatim != "" {
			if verbatim, err = s.Protect(ctx, verbatim); err != nil {
				return nil, err
			}
		}
	}
	parts, err := presentation.RenderConversation(key, []presentation.AnswerBlock{{Text: text}}, nil, verbatim)
	if err != nil {
		return nil, err
	}
	notices := make([]store.ChatNotice, len(parts))
	for i, part := range parts {
		notices[i] = store.ChatNotice{TaskRef: key, After: strings.TrimPrefix(operation, s.transport+":"+channel+":"), ChatReply: store.ChatReply{Transport: s.transport, MessageID: fmt.Sprintf("%s:%04d", key, i+1), ChannelRef: channel, Command: "announce", CreatedAt: at, ReplyText: part.Content}}
	}
	return notices, nil
}
