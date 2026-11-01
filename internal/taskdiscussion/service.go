package taskdiscussion

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

// Service admits and retrieves private task discussions under current repository consent.
type Service struct {
	db        *store.DB
	now       func() time.Time
	capture   Capture
	runner    Runner
	publisher Publisher
	protect   func(context.Context, string) (string, error)
	deadline  time.Duration
	mu        sync.Mutex
	cursorKey [32]byte
	active    atomic.Int64
}

// New requires safe execution boundaries and a finite installed provider deadline.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Now == nil || o.Capture == nil || o.Runner == nil || o.Protect == nil || o.Deadline <= 0 || o.Deadline > 30*time.Minute {
		return nil, errors.New("safe advisory dependencies and finite budget required")
	}
	service := &Service{db: o.DB, now: o.Now, capture: o.Capture, runner: o.Runner, publisher: o.Publisher, protect: o.Protect, deadline: o.Deadline}
	if _, err := rand.Read(service.cursorKey[:]); err != nil {
		return nil, err
	}
	return service, nil
}
func identity(prefix string) string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return prefix + hex.EncodeToString(b[:])
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func valid(s string, n int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= n && utf8.ValidString(s)
}
func (s *Service) authorize(ctx context.Context, p Principal, task, scope string) (store.DiscussionScope, error) {
	target, err := s.db.ResolveConnectorDiscussionTarget(ctx, task)
	if err != nil {
		return store.DiscussionScope{}, err
	}
	owner, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil {
		return store.DiscussionScope{}, store.ErrConnectorOwner
	}
	record := store.OwnerAction{Principal: store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID}, Scope: scope, RepositoryID: target.RepoID, RepositoryRef: target.RepositoryRef}
	if p.Transport != "mcp" {
		return store.DiscussionScope{}, store.ErrConnectorOwner
	}
	err = s.db.CheckOwnerActionAuthority(ctx, record.Principal, record, s.now())
	return store.DiscussionScope{OwnerID: owner, OwnerGeneration: p.OwnerGeneration, RepositoryRef: target.RepositoryRef, TaskRef: task}, err
}

// Open registers a conversation without paid execution or transcript retention.
func (s *Service) Open(ctx context.Context, p Principal, task, key string) (Conversation, error) {
	if !valid(key, 128) {
		return Conversation{}, store.ErrConnectorInvalid
	}
	scope, err := s.authorize(ctx, p, task, AdvisoryScope)
	if err != nil {
		scope, err = s.authorize(ctx, p, task, PublicationScope)
	}
	if err != nil {
		return Conversation{}, err
	}
	c, err := s.db.OpenDiscussion(ctx, store.DiscussionConversation{DiscussionScope: scope, ID: identity("conversation-"), ClientID: p.ClientID, RequestKey: key, CreatedAt: s.now()})
	return Conversation{ConversationRef: c.ID, ConversationRevision: c.Revision, TaskRef: task}, err
}

// Ask freezes one authorized advisory request before paid execution.
func (s *Service) Ask(ctx context.Context, p Principal, r Request) (JobView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !valid(r.TaskRef, 512) || !valid(r.ClientRequestKey, 128) || (r.Operation != "ask" && r.Operation != "judge") || len(r.ConversationRef) > 128 || r.ConversationRevision < 0 {
		return JobView{}, store.ErrConnectorInvalid
	}
	question := r.Question
	if r.Operation == "judge" {
		question = r.Focus
		if question == "" {
			question = "Independent assessment of the current saved analysis"
		}
	}
	if !valid(question, 32<<10) {
		return JobView{}, store.ErrConnectorInvalid
	}
	scope, err := s.authorize(ctx, p, r.TaskRef, AdvisoryScope)
	if err != nil {
		return JobView{}, err
	}
	digest := hash(r)
	old, err := s.db.AdvisoryByRequest(ctx, scope, r.Operation, r.ClientRequestKey)
	if err == nil {
		if old.Digest != digest {
			return JobView{}, store.ErrWorkflowConflict
		}
		return s.get(ctx, p, scope, old.JobID, "")
	}
	if !errors.Is(err, store.ErrNotFound) {
		return JobView{}, err
	}
	basis, err := s.capture.Capture(ctx, r.TaskRef, r.Operation)
	if err != nil {
		return JobView{}, err
	}
	if basis.TaskRef != r.TaskRef || basis.RepositoryRef != scope.RepositoryRef || basis.Revision == "" || basis.Role == "" || !json.Valid(basis.Evidence) || !json.Valid(basis.Guidance) {
		return JobView{}, ErrTargetUnavailable
	}
	var c store.DiscussionConversation
	if r.ConversationRef == "" {
		c, err = s.db.OpenDiscussion(ctx, store.DiscussionConversation{DiscussionScope: scope, ID: identity("conversation-"), ClientID: p.ClientID, RequestKey: "advisory:" + r.Operation + ":" + r.ClientRequestKey, CreatedAt: s.now()})
	} else {
		c, err = s.db.ReadDiscussion(ctx, scope, r.ConversationRef)
		if err == nil && c.Revision != r.ConversationRevision {
			err = store.ErrWorkflowRevision
		}
	}
	if err != nil {
		return JobView{}, err
	}
	exchanges, omitted, err := store.BoundedDiscussionContext(c.Exchanges, question)
	if err != nil {
		return JobView{}, err
	}
	contextBytes, _ := json.Marshal(struct {
		Question  string                     `json:"question"`
		Exchanges []store.DiscussionExchange `json:"exchanges"`
		Omitted   int64                      `json:"omitted_history"`
	}{question, exchanges, c.OmittedCount + int64(omitted)})
	for len(contextBytes) > 32<<10 && len(exchanges) > 0 {
		exchanges = exchanges[1:]
		omitted++
		contextBytes, _ = json.Marshal(struct {
			Question  string                     `json:"question"`
			Exchanges []store.DiscussionExchange `json:"exchanges"`
			Omitted   int64                      `json:"omitted_history"`
		}{question, exchanges, c.OmittedCount + int64(omitted)})
	}
	if len(contextBytes) > 32<<10 {
		return JobView{}, store.ErrConnectorInvalid
	}
	fresh, err := s.capture.Capture(ctx, r.TaskRef, r.Operation)
	if err != nil || hash(fresh) != hash(basis) {
		return JobView{}, ErrBasisChanged
	}
	if _, err = s.authorize(ctx, p, r.TaskRef, AdvisoryScope); err != nil {
		return JobView{}, err
	}
	raw, _ := json.Marshal(basis)
	a, err := s.db.AdmitAuthorizedAdvisory(ctx, store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID}, store.AdvisoryAdmission{DiscussionScope: scope, ID: identity("admission-"), ConversationID: c.ID, Operation: r.Operation, RequestKey: r.ClientRequestKey, Digest: digest, Basis: string(raw), TargetGuard: basis.StoreGuard, Context: string(contextBytes), Question: question, GrantID: p.GrantID, ConsentRevision: principalJSON(p), ConversationRevision: c.Revision, JobID: identity("advisory-"), ExecutionID: identity("execution-"), CreatedAt: s.now()})
	if err != nil {
		return JobView{}, err
	}
	return s.get(ctx, p, scope, a.JobID, "")
}

// Get retrieves a private result page under current owner and repository authority.
func (s *Service) Get(ctx context.Context, p Principal, id, cursor string) (JobView, error) {
	owner, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil {
		return JobView{}, store.ErrConnectorOwner
	}
	scope, err := s.db.AdvisoryTarget(ctx, owner, p.OwnerGeneration, id)
	if err != nil {
		return JobView{}, err
	}
	if _, err = s.authorize(ctx, p, scope.TaskRef, AdvisoryScope); err != nil {
		return JobView{}, err
	}
	return s.get(ctx, p, scope, id, cursor)
}
func (s *Service) get(ctx context.Context, p Principal, scope store.DiscussionScope, id, cursor string) (JobView, error) {
	a, err := s.db.ReadAdvisory(ctx, scope, id)
	if err != nil {
		return JobView{}, err
	}
	var basis Basis
	if err = json.Unmarshal([]byte(a.Basis), &basis); err != nil {
		return JobView{}, err
	}
	c, err := s.db.ReadDiscussion(ctx, scope, a.ConversationID)
	if err != nil {
		return JobView{}, err
	}
	v := JobView{JobRef: a.JobID, ConversationRef: c.ID, ConversationRevision: c.Revision, State: a.Job.State, Freshness: "unknown", OmittedHistory: c.OmittedCount, NextActions: []string{"advisory_job_get"}, CostGuidance: "One potentially paid invocation; estimate unknown. Repeating the same request key recovers this job.", ResultComplete: true}
	var input struct {
		Omitted int64 `json:"omitted_history"`
	}
	_ = json.Unmarshal([]byte(a.Context), &input)
	v.OmittedHistory = input.Omitted
	v.Freshness = s.freshness(ctx, basis, a.Operation)
	v.Basis, err = s.publicBasis(ctx, basis)
	if err != nil {
		return JobView{}, err
	}
	if a.Job.State == "succeeded" || a.Job.State == "failed" || a.Job.State == "cancelled" || a.Job.State == "uncertain" {
		if a.Job.Result != "" && a.Job.Result != "{}" {
			var answer Answer
			if err = json.Unmarshal([]byte(a.Job.Result), &answer); err != nil {
				return JobView{}, err
			}
			position, err := s.openResultCursor(cursor, p, a.JobID, hash(a.Job.Result))
			if err != nil {
				return JobView{}, err
			}
			page, next, complete, err := resultPage(answer, position)
			if err != nil {
				return JobView{}, err
			}
			v.Answer = &page
			v.ResultPageOffset = position.AnswerOffset
			v.ResultComplete = complete
			if !complete {
				v.NextCursor, err = s.sealResultCursor(next, p, a.JobID, hash(a.Job.Result))
				if err != nil {
					return JobView{}, err
				}
			}
		} else if cursor != "" {
			return JobView{}, errors.New("invalid advisory cursor")
		}
	} else if cursor != "" {
		return JobView{}, errors.New("invalid advisory cursor")
	}
	if a.Job.State == "queued" || a.Job.State == "running" || a.Job.State == "stop_requested" {
		v.NextActions = append(v.NextActions, "advisory_job_cancel")
	}
	if _, err = s.authorize(ctx, p, scope.TaskRef, AdvisoryScope); err != nil {
		return JobView{}, err
	}
	return v, nil
}

// Cancel cancels queued work or records a best-effort running stop request.
func (s *Service) Cancel(ctx context.Context, p Principal, id string) (JobView, error) {
	v, err := s.Get(ctx, p, id, "")
	if err != nil {
		return v, err
	}
	scope, err := s.authorize(ctx, p, v.Basis.TaskRef, AdvisoryScope)
	if err != nil {
		return JobView{}, err
	}
	_, won, err := s.db.CancelAdvisory(ctx, scope, id, s.now())
	if err != nil {
		return JobView{}, err
	}
	v, err = s.Get(ctx, p, id, "")
	v.CompletionWon = won
	return v, err
}

func principalJSON(p Principal) string { b, _ := json.Marshal(p); return string(b) }
