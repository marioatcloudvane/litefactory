package durableworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// AttemptView exposes execution history without protected proposal payloads.
type AttemptView struct {
	Confirmation string    `json:"confirmation"`
	State        string    `json:"state"`
	QueuedAt     time.Time `json:"queued_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	Deadline     time.Time `json:"deadline,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

// NoticeView keeps delivery status independent from the execution outcome.
type NoticeView struct {
	Event   string `json:"event"`
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// JobView is an authorized, bounded projection of one retained job.
type JobView struct {
	Job        string         `json:"job"`
	Definition CatalogueEntry `json:"definition"`
	Project    string         `json:"project,omitempty"`
	State      string         `json:"state"`
	Revision   int64          `json:"revision"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Output
	Attempts   []AttemptView `json:"attempts"`
	Delivery   []NoticeView  `json:"delivery"`
	NextAction string        `json:"next_action"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// Get reads an authorized bounded result page and independent delivery status.
func (s *Service) Get(ctx context.Context, p owneraction.Principal, id, raw string, limit int) (JobView, error) {
	if limit < 1 || limit > 50 || len(raw) > 4096 {
		return JobView{}, errors.New("invalid result page")
	}
	job, err := s.db.ReadWorkflowJob(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	var v frozen
	if err = json.Unmarshal([]byte(job.Snapshot), &v); err != nil {
		return JobView{}, err
	}
	if err = s.authorizeJob(ctx, p, job, v); err != nil {
		return JobView{}, err
	}
	fp, err := s.reader(ctx, p)
	if err != nil {
		return JobView{}, err
	}
	c := cursor{Owner: p.OwnerID, Generation: p.OwnerGeneration, Grant: p.GrantID, Fingerprint: fp, Job: id, Revision: job.Revision}
	if raw != "" {
		old, e := s.decodeCursor(raw)
		if e != nil || old.Owner != c.Owner || old.Generation != c.Generation || old.Grant != c.Grant || old.Fingerprint != c.Fingerprint || old.Job != id || old.Revision != job.Revision || old.Project != "" || old.Offset < 0 {
			return JobView{}, errors.New("invalid result cursor")
		}
		c = old
	}
	var out Output
	if err = json.Unmarshal([]byte(job.Result), &out); err != nil {
		return JobView{}, err
	}
	if c.Offset > len(out.Items) {
		return JobView{}, errors.New("invalid result cursor")
	}
	view := JobView{Job: id, Definition: catalogueEntry(v.Definition, s.adapters[v.Definition.Adapter] != nil, ""), Project: v.Resolved.Project, State: job.State, Revision: job.Revision, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Output: out, Attempts: []AttemptView{}, Delivery: []NoticeView{}}
	end := min(len(out.Items), c.Offset+limit)
	view.Items = out.Items[c.Offset:end]
	if view.Items == nil {
		view.Items = []map[string]any{}
	}
	if end < len(out.Items) {
		c.Offset = end
		view.NextCursor = s.encodeCursor(c)
	}
	attempts, err := s.db.WorkflowRecentAttempts(ctx, id, 50)
	if err != nil {
		return JobView{}, err
	}
	if len(attempts) > 50 {
		attempts = attempts[len(attempts)-50:]
	}
	for _, a := range attempts {
		view.Attempts = append(view.Attempts, AttemptView{Confirmation: bounded(a.ProposalID, 128), State: bounded(a.State, 32), QueuedAt: a.QueuedAt, StartedAt: a.StartedAt, Deadline: a.Deadline, FinishedAt: a.FinishedAt})
	}
	notices, err := s.db.WorkflowRecentNotices(ctx, id, 50)
	if err != nil {
		return JobView{}, err
	}
	if len(notices) > 50 {
		notices = notices[len(notices)-50:]
	}
	for _, n := range notices {
		view.Delivery = append(view.Delivery, NoticeView{Event: bounded(n.Event, 32), State: bounded(n.State, 32), Reason: bounded(n.Reason, 512), Message: bounded(n.MessageID, 128)})
	}
	switch job.State {
	case "queued", "running":
		view.NextAction = "Read this job again for progress"
	case "waiting_for_input":
		view.NextAction = "Propose answers for the exact questions and session revision, then confirm"
	case "uncertain":
		view.NextAction = "Review retained domain evidence; do not repeat unresolved external effects"
	default:
		view.NextAction = "Read the retained result"
	}
	// Leave room for both MCP structured content and its quoted text equivalent.
	const responseBudget = 40 << 10
	for {
		encoded, err := json.Marshal(view)
		if err != nil {
			return JobView{}, err
		}
		if len(encoded) <= responseBudget {
			break
		}
		if len(view.Delivery) > 1 {
			view.Delivery = view.Delivery[1:]
			continue
		}
		if len(view.Attempts) > 1 {
			view.Attempts = view.Attempts[1:]
			continue
		}
		if len(view.Items) > 1 {
			view.Items = view.Items[:len(view.Items)-1]
			end--
			c.Offset = end
			view.NextCursor = s.encodeCursor(c)
			continue
		}
		return JobView{}, errors.New("workflow result metadata exceeds display limits; review the saved domain record")
	}
	return view, nil
}
func principalFromStore(p store.OwnerActionPrincipal) owneraction.Principal {
	return owneraction.Principal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID, ChannelID: p.ChannelID, DiscordOwnerID: p.DiscordOwnerID}
}
func (s *Service) attemptFacts(ctx context.Context, a store.WorkflowAttempt) (owneraction.Principal, owneraction.Facts, frozen, error) {
	record, err := s.db.ReadOwnerAction(ctx, a.ProposalID)
	if err != nil {
		return owneraction.Principal{}, owneraction.Facts{}, frozen{}, err
	}
	var f owneraction.Facts
	if err = json.Unmarshal([]byte(record.Facts), &f); err != nil {
		return owneraction.Principal{}, f, frozen{}, err
	}
	f.Payload = record.Payload
	v, err := unpack(f)
	if err == nil {
		request, readErr := s.db.WorkflowRequestByProposal(ctx, a.ProposalID)
		if readErr != nil {
			return owneraction.Principal{}, f, v, readErr
		}
		if err = json.Unmarshal([]byte(request.Inputs), &v); err == nil {
			v.Domain.Payload = v.DomainPayload
		}
	}
	return principalFromStore(record.Principal), f, v, err
}

// RunOnce executes at most one admitted attempt. A claim is durable and globally exclusive.
func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !s.workerMu.TryLock() {
		return false, nil
	}
	ownsRunner := true
	defer func() {
		if ownsRunner {
			s.workerMu.Unlock()
		}
	}()
	limit := time.Second
	for _, d := range s.registry.Definitions() {
		if candidate := time.Duration(d.DeadlineSeconds) * time.Second; candidate > limit {
			limit = candidate
		}
	}
	a, j, err := s.db.ClaimWorkflowAttempt(ctx, s.now(), limit)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	p, f, v, err := s.attemptFacts(ctx, a)
	if err != nil {
		return true, err
	}
	if err = s.Recheck(ctx, p, f, s.now()); err != nil {
		return true, s.finish(ctx, a, j, Output{State: "failed", Summary: "Workflow authority or installed definition is no longer available"}, false)
	}
	duration := time.Duration(v.Definition.DeadlineSeconds) * time.Second
	if duration <= 0 {
		return true, s.finish(ctx, a, j, Output{State: "failed", Summary: "Workflow deadline is invalid"}, false)
	}
	deadline := a.StartedAt.Add(duration)
	if err := s.db.SetWorkflowAttemptDeadline(ctx, a, deadline); err != nil {
		return true, err
	}
	a.Deadline = deadline
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	runCtx = store.WithOwnerActionEffect(runCtx, a.ExecutionID, s.now)
	runCtx = store.WithStandardsOwnerActionClock(runCtx, s.now)
	type answer struct {
		out Output
		err error
	}
	completed := make(chan answer, 1)
	adapter := s.adapters[v.Definition.Adapter]
	ownsRunner = false
	go func() {
		defer s.workerMu.Unlock()
		out, e := adapter.Execute(runCtx, p, v.Domain, a.ExecutionID, s.now())
		completed <- answer{out, e}
	}()
	var out Output
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
wait:
	for {
		select {
		case result := <-completed:
			out = result.out
			if result.err != nil {
				if out.State == "" {
					out.State = "uncertain"
				}
				if out.Summary == "" {
					out.Summary = "Workflow did not return conclusive completion; retained evidence requires review"
				}
			}
			if runCtx.Err() != nil {
				out.State = "uncertain"
				out.Summary = "Workflow stopped at its deadline or shutdown; external effects require reconciliation"
			}
			break wait
		case <-heartbeat.C:
			if err := s.db.HeartbeatWorkflowAttempt(runCtx, a, s.now()); err != nil {
				cancel()
				out = Output{State: "uncertain", Summary: "Workflow execution claim could not be maintained; review retained domain evidence"}
				break wait
			}
		case <-runCtx.Done():
			out = Output{State: "uncertain", Summary: "Workflow stopped at its deadline or shutdown; external effects require reconciliation"}

			break wait
		}
	}
	// Persistence survives daemon cancellation, but remains bounded and fenced.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer saveCancel()
	if !s.now().Before(a.Deadline) {
		out.State = "uncertain"
		out.Summary = "Workflow output arrived after its execution deadline; review retained domain evidence"
		return true, s.finish(saveCtx, a, j, out, true)
	}
	return true, s.finish(saveCtx, a, j, out, false)
}

// RecoverRunning reconciles receipts after restart. It never dispatches an adapter.
func (s *Service) RecoverRunning(ctx context.Context) error {
	a, err := s.db.RunningWorkflowAttempt(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	j, err := s.db.ReadWorkflowJob(ctx, a.JobID)
	if err != nil {
		return err
	}
	p, _, v, err := s.attemptFacts(ctx, a)
	if err != nil {
		return err
	}
	out := Output{State: "uncertain", Summary: "A previous attempt has no conclusive receipt; review retained domain evidence"}
	adapter := s.adapters[v.Definition.Adapter]
	if adapter != nil && s.authorize(ctx, p, v.Definition, v.Resolved.Project) == nil {
		recovered, found, e := adapter.Recover(ctx, p, v.Domain, a.ExecutionID, s.now())
		if found {
			out = recovered
			if e != nil {
				out.State = "uncertain"
			}
		}
	}
	return s.finish(ctx, a, j, out, true)
}
func bounded(text string, limit int) string {
	// Bound the encoded form too: escaped control characters otherwise expand
	// enough to make a short-looking question exceed the transport envelope.
	encoded, _ := json.Marshal(text)
	if len(encoded) <= limit {
		return text
	}
	lower, upper := 0, min(len(text), limit)
	for lower < upper {
		middle := (lower + upper + 1) / 2
		candidate := text[:middle]
		for !utf8.ValidString(candidate) {
			candidate = candidate[:len(candidate)-1]
		}
		encoded, _ = json.Marshal(candidate)
		if len(encoded) <= limit {
			lower = middle
		} else {
			upper = middle - 1
		}
	}
	text = text[:lower]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

func (s *Service) safeValue(ctx context.Context, v any, depth int) (any, error) {
	if depth > 32 {
		return nil, errors.New("workflow result exceeds nesting limit")
	}
	switch x := v.(type) {
	case string:
		text, err := s.protect(ctx, x)
		return text, err
	case map[string]any:
		if len(x) > 4096 {
			return nil, errors.New("workflow result object exceeds limit")
		}
		out := map[string]any{}
		for k, value := range x {
			safeKey, err := s.protect(ctx, k)
			if err != nil {
				return nil, err
			}
			safe, err := s.safeValue(ctx, value, depth+1)
			if err != nil {
				return nil, err
			}
			out[safeKey] = safe
		}
		return out, nil
	case []any:
		if len(x) > 4096 {
			return nil, errors.New("workflow result array exceeds limit")
		}
		out := make([]any, len(x))
		for i, value := range x {
			safe, err := s.safeValue(ctx, value, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = safe
		}
		return out, nil
	case nil, bool, float64:
		return x, nil
	default:
		return nil, fmt.Errorf("unsupported workflow result value %T", v)
	}
}

// retainedItems splits only the display representation; joining each ordered
// fragment's content reconstructs the complete redacted original item JSON.
func retainedItems(items []map[string]any, domainRef string) ([]map[string]any, error) {
	const maxBytes = 10 << 20
	const maxItems = 4096
	retained := make([]map[string]any, 0, len(items))
	total := 0
	for index, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return retained, err
		}
		parts := []map[string]any{item}
		if len(raw) > 8<<10 {
			chunks := []string{}
			for offset := 0; offset < len(raw); {
				end := min(len(raw), offset+(5<<10))
				candidate := string(raw[offset:end])
				for !utf8.ValidString(candidate) {
					candidate = candidate[:len(candidate)-1]
				}
				content := bounded(candidate, 6<<10)
				chunks = append(chunks, content)
				offset += len(content)
			}
			parts = make([]map[string]any, 0, len(chunks))
			for part, content := range chunks {
				parts = append(parts, map[string]any{"format": "json_fragment", "item_index": index, "part": part + 1, "parts": len(chunks), "content": content})
			}
		}
		partBytes := 0
		for _, part := range parts {
			encoded, _ := json.Marshal(part)
			partBytes += len(encoded)
		}
		// Reserve a final marker rather than return an incomplete fragment group.
		itemLimit := maxItems
		if index < len(items)-1 {
			itemLimit--
		}
		if total+partBytes > maxBytes-(1<<10) || len(retained)+len(parts) > itemLimit {
			retained = append(retained, map[string]any{"truncated": true, "summary": "Result exceeded the retained size ceiling; inspect the saved domain record", "domain_ref": domainRef})
			return retained, errors.New("workflow retained result ceiling exceeded")
		}
		retained = append(retained, parts...)
		total += partBytes
	}
	return retained, nil
}

func (s *Service) finish(ctx context.Context, a store.WorkflowAttempt, j store.WorkflowJob, out Output, recovery bool) error {
	if out.State == "uncertain" || out.State == "failed" {
		var previous Output
		if json.Unmarshal([]byte(j.Result), &previous) == nil {
			if out.DomainRef == "" {
				out.DomainRef = previous.DomainRef
			}
			if len(out.Items) == 0 {
				out.Items = previous.Items
			}
			if len(out.Questions) == 0 {
				out.Questions = previous.Questions
			}
			if out.SessionRevision == 0 {
				out.SessionRevision = previous.SessionRevision
			}
		}
	}
	switch out.State {
	case "succeeded", "failed", "uncertain", "waiting_for_input":
	default:
		out.State = "uncertain"
	}
	if len(out.Items) > 4096 {
		out.Items = append(out.Items[:4095], map[string]any{"truncated": true, "summary": "Result exceeded the retained item ceiling; inspect the saved domain record"})
		out.State = "uncertain"
		out.Summary = "Result exceeded retained limits; review domain evidence"
	}
	if len(out.Questions) > 20 {
		out.Questions = out.Questions[:20]
		out.State = "uncertain"
	}
	// Admit at most ten MiB of source result data before redaction or paging.
	sourceBytes := 0
	for i, item := range out.Items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if sourceBytes+len(encoded) > 10<<20 {
			out.Items = append(out.Items[:i], map[string]any{"truncated": true, "summary": "Result exceeded the retained ten MiB ceiling; inspect the domain record", "domain_ref": out.DomainRef})
			out.State = "uncertain"
			out.Summary = "Some result details exceeded retention limits; review the saved domain record"
			break
		}
		sourceBytes += len(encoded)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if len(raw) > 10<<20 {
		out = Output{State: "uncertain", Summary: "Result exceeded the retained ten MiB ceiling; inspect the domain record", DomainRef: out.DomainRef, Items: []map[string]any{{"truncated": true}}}
		raw, err = json.Marshal(out)
		if err != nil {
			return err
		}
	}
	// Redact domain data, keeping the server's typed envelope and state intact.
	var normalized map[string]any
	if err = json.Unmarshal(raw, &normalized); err != nil {
		return err
	}
	safe, protectErr := s.safeValue(ctx, normalized["items"], 0)
	if protectErr == nil {
		raw, err = json.Marshal(safe)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &out.Items); err != nil {
			return err
		}
		out.Summary, protectErr = s.protect(ctx, out.Summary)
	}
	if protectErr == nil {
		out.DomainRef, protectErr = s.protect(ctx, out.DomainRef)
	}
	for i := range out.Questions {
		if protectErr != nil {
			break
		}
		out.Questions[i].ID, protectErr = s.protect(ctx, out.Questions[i].ID)
		if protectErr == nil {
			out.Questions[i].Text, protectErr = s.protect(ctx, out.Questions[i].Text)
		}
	}
	if protectErr != nil {
		var previous Output
		if json.Unmarshal([]byte(j.Result), &previous) == nil {
			out = previous
		} else {
			out = Output{}
		}
		out.State = "uncertain"
		out.Summary = "Result could not be safely retained; review domain evidence"
	}

	// State is server-owned. A redactor cannot rewrite lifecycle values.
	if out.State != "succeeded" && out.State != "failed" && out.State != "waiting_for_input" {
		out.State = "uncertain"
	}
	if out.State == "waiting_for_input" && (len(out.Questions) == 0 || out.SessionRevision < 1) {
		out.State = "uncertain"
		out.Summary = "Author input receipt is incomplete; review domain evidence"
	}
	out.Summary = bounded(strings.TrimSpace(out.Summary), 2048)
	out.DomainRef = bounded(out.DomainRef, 256)
	for i := range out.Questions {
		q := &out.Questions[i]
		id, text := bounded(q.ID, 128), bounded(q.Text, 1024)
		if id != q.ID || text != q.Text {
			out.State = "uncertain"
		}
		q.ID, q.Text = id, text
	}
	if out.Items == nil {
		out.Items = []map[string]any{}
	}
	out.Items, err = retainedItems(out.Items, out.DomainRef)
	if err != nil {
		out.State = "uncertain"
		out.Summary = "Some result details exceeded retention limits; review the saved domain record"
	}

	raw, err = json.Marshal(out)
	if err != nil {
		return err
	}
	questions, err := json.Marshal(out.Questions)
	if err != nil {
		return err
	}
	if string(questions) == "null" {
		questions = []byte("[]")
	}
	destination := ""
	p, _, v, readErr := s.attemptFacts(ctx, a)
	if readErr == nil && v.NoticeDestination != "" && s.noticeDestination != nil {
		current, e := s.noticeDestination(ctx, p, v.Definition, v.Resolved)
		if e == nil && current == v.NoticeDestination {
			destination = v.NoticeDestination
		}
	}

	event := "terminal"
	if out.State == "waiting_for_input" {
		event = "input"
	}
	f := store.WorkflowFinish{State: out.State, Result: string(raw), Questions: string(questions), DomainRef: out.DomainRef, Summary: out.Summary, Revision: j.Revision + 1, Notice: &store.WorkflowNotice{Event: event, Body: "Workflow " + a.JobID + ": " + out.State + ". " + bounded(out.Summary, 1024), Destination: destination}}
	if recovery {
		return s.db.ReconcileWorkflowAttempt(ctx, a, f, s.now())
	}
	return s.db.FinishWorkflowAttempt(ctx, a, f, s.now())
}

// RecoverExpired reconciles abandoned expired claims without replaying effects.
func (s *Service) RecoverExpired(ctx context.Context) error {
	if !s.workerMu.TryLock() {
		return nil
	}
	defer s.workerMu.Unlock()
	a, err := s.db.RunningWorkflowAttempt(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.now().Before(a.Deadline) {
		return nil
	}
	return s.RecoverRunning(ctx)
}
