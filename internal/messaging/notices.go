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
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// NoticeEvent is a transport-neutral projection of a durable factory event.
type NoticeEvent struct {
	Key, Kind, Workflow, From, To, Question, QuestionLink string
	Task                                                  naming.Ref
	RunID                                                 int64
	At                                                    time.Time
	Usage                                                 *report.MeterSummary
}

// NoticeEvents maps source facts to stable event identities. Graph node
// transitions, same-mode follow-on runs and review waits are intentionally silent.
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
		} else if r.Run.Succeeded() {
			e.Kind = "completed"
		}
	case "transitions":
		// Graph node moves stay within one graph instance, and so within one
		// mode; they are factory internals, never owner notices.
		return nil, nil
	case "owner_waits":
		e.Key = "wait:" + r.Key + "@" + store.FormatTime(r.At) + "@" + r.TaskRef
		e.Kind = "question"
		e.Question, err = noticeQuestion(r.Candidate, r.StopReason)
		if err != nil {
			return nil, err
		}
		e.QuestionLink = fmt.Sprintf("%s#issuecomment-%d", ref.IssueURL(), r.CommentID)
		if parked {
			stopped := e
			stopped.Key = "stop:" + r.Key + "@" + store.FormatTime(r.At) + "@" + r.TaskRef
			// An owner-cancelled run and its parked graph are the same stop.
			// Other graph parking is a separate event from the source run finish.
			if _, ownerStop := r.Run.OwnerStop(); ownerStop {
				stopped.Key = fmt.Sprintf("run:%d:finish", r.Run.ID)
			}
			stopped.Kind = "stopped"
			stopped.Question = ""
			stopped.QuestionLink = ""
			if r.Ready {
				return []NoticeEvent{stopped, e}, nil
			}
			return []NoticeEvent{stopped}, nil
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

// NoticeText budgets the question separately so task and comment links survive
// even a long owner question. The budget counts UTF-16 units used by chat APIs.
func NoticeText(e NoticeEvent) string {
	header := fmt.Sprintf("%s: %s (%s)\n%s", e.Kind, e.Task.String(), e.Workflow, e.Task.IssueURL())
	if e.From != "" {
		header += fmt.Sprintf("\nMode: %s → %s", e.From, e.To)
	}
	if e.QuestionLink != "" {
		header += "\n" + e.QuestionLink
	}
	if e.Question != "" {
		header += "\n" + noticeLimit(e.Question, max(0, 1900-noticeUnits(header)-1))
	}
	if e.Usage != nil && (e.Kind == "completed" || e.Kind == "failed" || e.Kind == "stopped") {
		header += "\n" + noticeUsage(e.Usage)
	}
	return noticeLimit(header, 1900)
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
	b.WriteRune('…')
	return b.String()
}
func noticeUsage(s *report.MeterSummary) string {
	value := func(n *int64) string {
		if n == nil {
			return "unknown"
		}
		return fmt.Sprint(*n)
	}
	text := fmt.Sprintf("Usage: input %s, output %s, cache read %s, cache write %s tokens. Collection: %s; pricing: %s.", value(s.KnownTokens.InputUncached), value(s.KnownTokens.Output), value(s.KnownTokens.CacheRead), value(s.KnownTokens.CacheWrite), s.CollectionStatus, s.PricingStatus)
	if s.EstimatedUSD != nil {
		text += " Estimated cost: $" + *s.EstimatedUSD + " USD."
	} else if s.PricedSubtotalUSD != nil {
		text += " Estimated priced subtotal: $" + *s.PricedSubtotalUSD + " USD."
	}
	return text
}

// NoticeCollector reads existing tables only and writes chat-owned state. It
// runs solely in the sender goroutine; failures never enter run execution.
type NoticeCollector struct {
	DB                 *store.DB
	Transport, Channel string
	Logger             *slog.Logger
	Protect            func(context.Context, string) (string, error)
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
	for _, source := range []string{"run_starts", "run_finishes", "transitions", "owner_waits"} {
		if err := c.collectSource(ctx, source, at); err != nil {
			errs = append(errs, fmt.Errorf("collect %s: %w", source, err))
		}
	}
	return errors.Join(errs...)
}
func (c *NoticeCollector) collectSource(ctx context.Context, source string, at time.Time) error {
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
		d := s.sender.Deliver(ctx, Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef}, r.ReplyText)
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
