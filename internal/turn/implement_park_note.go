package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/github"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// The revision is the parked revision, including when publication precedes the
// transition. Neither a retry run nor a retry attempt changes this identity.
func implementParkIntent(r *Reporter, parent, destination naming.Ref, row store.ImplementStory) store.ReportDelivery {
	revision := row.Revision
	if row.TimeoutAttemptID != 0 {
		revision = row.TimeoutParkRevision
	} else if row.State != store.ImplementParked {
		revision++
	}
	return r.deliveryIntent(destination, destination, row.WorkerSession, 0, 0,
		fmt.Sprintf("implement-park:%s:%d:%s:%d", parent.String(), row.Generation, row.Key, revision))
}

func (rt *implementRuntime) publishParkAnswer(ctx context.Context, opts Options, a ActivityRequest, row store.ImplementStory, reason string) error {
	r := opts.ResearchReporter
	if r == nil {
		return errors.New("parked answer requires the report delivery service")
	}
	revision, err := implementStoryRevision(ctx, opts.DB, row)
	if err != nil {
		return err
	}
	parent, err := naming.ParseRef(row.Parent)
	if err != nil {
		return err
	}
	destination := parent
	destination.Issue = revision.IssueNumber
	if err := destination.Validate(); err != nil {
		return err
	}
	intent := implementParkIntent(r, parent, destination, row)
	// Historical parked rows have no publication intent. Never reconstruct or
	// backfill their clipped answers. A retained intent is always replayed as-is.
	if row.State == store.ImplementParked {
		if _, err := opts.DB.GetReportDelivery(ctx, intent.Key); errors.Is(err, store.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
	}
	_, err = r.deliver(ctx, intent, func() (redact.Body, error) {
		var node agentresult.ResearchNode
		var readErr error
		if row.TimeoutAttemptID == 0 {
			node, readErr = rt.retainedAnswer(ctx, opts, a)
		}
		if readErr != nil && row.TimeoutAttemptID == 0 && row.Attempts < store.MaxImplementAttempts {
			return redact.Body{}, readErr
		}
		if row.TimeoutAttemptID != 0 || readErr != nil || node.Response == agentresult.ImplementResponseCompleted {
			if row.TimeoutAttemptID != 0 {
				reason = row.LastFailure
				outputs, outputErr := opts.DB.ListGraphOutputs(ctx, row.TimeoutAttemptID)
				if outputErr != nil {
					reason += " Attempt diagnostic evidence unavailable: " + outputErr.Error() + "."
				} else {
					found := false
					for _, output := range outputs {
						if output.Kind != store.GraphOutputDiagnostic {
							continue
						}
						diagnostic := strings.TrimSpace(string(output.Payload))
						if diagnostic == "" {
							continue
						}
						found = true
						if !strings.Contains(reason, diagnostic) {
							reason += "\n" + diagnostic
						}
					}
					if !found {
						reason += " Attempt diagnostic evidence unavailable."
					}
				}
				timing, timingErr := opts.DB.GetAttemptTiming(ctx, row.TimeoutAttemptID, 0)
				if timingErr == nil {
					reason = fmt.Sprintf("Effective worker limit: %g seconds. Deadline: %s.\n%s", timing.Allowance.Seconds(), timing.Deadline.UTC().Format(time.RFC3339Nano), reason)
				} else {
					reason += " Effective deadline evidence unavailable: " + timingErr.Error() + "."
				}
				reason += " Available branch, worktree, worker session and recorded results are kept. Reply on this story issue to continue the kept work once."
			}
			node = agentresult.ResearchNode{Result: agentresult.Result{Reason: reason}}
		}
		return renderImplementParkAnswer(ctx, r, node)
	})
	if err != nil || row.TimeoutAttemptID == 0 {
		return err
	}
	note, err := opts.DB.GetReportDelivery(ctx, intent.Key)
	if err != nil {
		return err
	}
	if !note.Completed || note.CommentID < 1 {
		return errors.New("timeout notice publication is not confirmed")
	}
	return opts.DB.ConfirmImplementTimeoutNotice(ctx, row.Parent, row.Generation, row.Key, row.TimeoutAttemptID, intent.Key, strconv.FormatInt(note.CommentID, 10), rt.now())
}

func renderImplementParkAnswer(ctx context.Context, r *Reporter, node agentresult.ResearchNode) (redact.Body, error) {
	// Decode the owner-facing result to render every field, including nested
	// questions and contributions, without JSON escaping hiding raw secrets.
	content, err := json.Marshal(node.Result)
	if err != nil {
		return redact.Body{}, err
	}
	var fields any
	if err := json.Unmarshal(content, &fields); err != nil {
		return redact.Body{}, err
	}
	text := implementAnswerText(fields)
	safe, err := r.renderer.RenderReportDelivery(ctx, text, "")
	if err != nil {
		return redact.Body{}, err
	}
	response := "factory reason"
	if node.Response == agentresult.ImplementResponseBlocked {
		response = "blocked"
	}
	if node.Response == agentresult.ImplementResponseIncomplete {
		response = "incomplete"
	}
	text = "**Story parked**\n\nWorker response: " + response + "\n\n" + escapeImplementText(safe.String())
	return r.renderer.RenderReportDelivery(ctx, text, "")
}

// implementParkNotes only reads receipts; periodic status refresh never posts.
func implementParkNotes(ctx context.Context, db *store.DB, a ImplementAuthority, rows []store.ImplementStory) (map[string]store.ReportDelivery, error) {
	notes := map[string]store.ReportDelivery{}
	for _, row := range rows {
		if row.State != store.ImplementParked {
			continue
		}
		story, ok := a.story(row.Key)
		if !ok || story.IssueNumber < 1 {
			continue
		}
		destination := a.Parent
		destination.Issue = story.IssueNumber
		intent := implementParkIntent(&Reporter{}, a.Parent, destination, row)
		note, err := db.GetReportDelivery(ctx, intent.Key)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if note.Completed && note.CommentID > 0 && note.Destination == destination.String() {
			notes[row.Key] = note
		}
	}
	return notes, nil
}

// escapeImplementText preserves all text while preventing Markdown, mentions,
// HTML and markers from acting as report structure.
func escapeImplementText(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch {
		case r == '\n':
			out.WriteString("<br>\n")
		case strings.ContainsRune("&<>@#[]()!*_`~\\:./=-+|", r):
			fmt.Fprintf(&out, "&#%d;", r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Keep original string values intact until outbound redaction has run.
func implementAnswerText(value any) string {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var out strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&out, "%s: %s\n", key, implementAnswerText(v[key]))
		}
		return out.String()
	case []any:
		var out strings.Builder
		for _, item := range v {
			out.WriteString(implementAnswerText(item))
			out.WriteByte('\n')
		}
		return out.String()
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// RecoverImplementTimeoutPark settles a durable timeout after interrupted graph
// bookkeeping. It only delivers the retained notice and parks the held row; it
// never prepares a worktree or invokes a provider.
func RecoverImplementTimeoutPark(ctx context.Context, opts Options, row store.ImplementStory) error {
	if row.TimeoutAttemptID == 0 {
		return nil
	}
	parent, err := naming.ParseRef(row.Parent)
	if err != nil {
		return err
	}
	rt := &implementRuntime{opts: opts, parentRef: parent, generation: row.Generation, worker: &row}
	_, err = rt.parkStory(ctx, opts, ActivityRequest{})
	return err
}

// implementTimeoutNoticeTime observes the exact retained child notice when a
// parent reply is admitted, closing the remote-publication/local-receipt gap.
func implementTimeoutNoticeTime(ctx context.Context, opts Options, row store.ImplementStory) (time.Time, error) {
	if opts.MCP == nil || opts.MCP.Comments == nil {
		return time.Time{}, errors.New("timeout notice observation is unavailable")
	}
	revision, err := implementStoryRevision(ctx, opts.DB, row)
	if err != nil {
		return time.Time{}, err
	}
	ref, err := naming.ParseRef(row.Parent)
	if err != nil {
		return time.Time{}, err
	}
	for page := 1; page > 0; {
		observed, err := opts.MCP.Comments.ListCommentsPage(ctx, github.RepoFromRef(ref), revision.IssueNumber, page, 100)
		if err != nil {
			return time.Time{}, err
		}
		for _, comment := range observed.Comments {
			if strconv.FormatInt(comment.ID, 10) == row.TimeoutCommentID {
				return comment.CreatedAt, nil
			}
		}
		if observed.NextPage <= page {
			break
		}
		page = observed.NextPage
	}
	return time.Time{}, errors.New("confirmed timeout notice is unavailable from provider")
}
