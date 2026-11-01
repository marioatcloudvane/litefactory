package stagedecision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Service decorates the existing confirmation domain without replacing its policy.
type Service struct {
	db        *store.DB
	now       func() time.Time
	protect   func(context.Context, string) (string, error)
	publisher Publisher
	boundary  Boundary
	actions   *owneraction.Service
}

// New requires ready store, protection and publication boundaries.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Now == nil || o.Protect == nil || o.Publisher == nil {
		return nil, errors.New("stage decision boundaries unavailable")
	}
	if o.Boundary == nil {
		o.Boundary = o.DB
	}
	return &Service{db: o.DB, now: o.Now, protect: o.Protect, publisher: o.Publisher, boundary: o.Boundary}, nil
}

// Ready reports whether confirmation and publication are composed.
func (s *Service) Ready() bool { return s != nil && s.actions != nil && s.publisher != nil }

// BindActions composes the stage domain before the confirmation service starts.
func (s *Service) BindActions(a *owneraction.Service) {
	s.actions = a
	a.DecorateDomain(func(next owneraction.Domain) owneraction.Domain { return domain{s, next} })
}
func valid(v string, n int) bool {
	return strings.TrimSpace(v) != "" && len(v) <= n && utf8.ValidString(v)
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func actor(p owneraction.Principal) store.OwnerActionPrincipal {
	return store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID, ChannelID: p.ChannelID, DiscordOwnerID: p.DiscordOwnerID}
}
func (s *Service) authorize(ctx context.Context, p owneraction.Principal, task string) (store.DiscussionScope, error) {
	target, err := s.db.ResolveConnectorDiscussionTarget(ctx, task)
	if err != nil {
		return store.DiscussionScope{}, err
	}
	owner, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil {
		return store.DiscussionScope{}, store.ErrConnectorOwner
	}
	if p.Transport != "mcp" {
		return store.DiscussionScope{}, store.ErrConnectorOwner
	}
	a := actor(p)
	err = s.db.CheckOwnerActionAuthority(ctx, a, store.OwnerAction{Principal: a, Scope: "repository:actions", RepositoryID: target.RepoID, RepositoryRef: target.RepositoryRef}, s.now())
	return store.DiscussionScope{OwnerID: owner, OwnerGeneration: p.OwnerGeneration, RepositoryRef: target.RepositoryRef, TaskRef: task}, err
}

// Propose records review facts only. Shared confirmation remains a separate call.
func (s *Service) Propose(ctx context.Context, p owneraction.Principal, operation string, r Request) (owneraction.Proposal, error) {
	if !s.Ready() {
		return owneraction.Proposal{}, owneraction.ErrUnavailable
	}
	if operation != "revise_task" && operation != "approve_stage" {
		return owneraction.Proposal{}, store.ErrConnectorInvalid
	}
	if strings.TrimSpace(r.Change) != "" {
		operation = "revise_task"
	}
	if err := validateRequest(operation, r); err != nil {
		return owneraction.Proposal{}, err
	}
	raw, _ := json.Marshal(r)
	if len(raw) > 60<<10 {
		return owneraction.Proposal{}, store.ErrConnectorCapacity
	}
	p.ChannelID = r.DiscordChannelID
	return s.actions.Propose(ctx, p, owneraction.Invocation{Command: operation, Arguments: map[string]string{"request": string(raw)}}, r.ClientRequestKey)
}

func validateRequest(operation string, r Request) error {
	if !valid(r.TaskRef, 512) || !valid(r.Stage, 64) || !valid(r.ClientRequestKey, 128) || len(r.ConversationRef) > 128 || len(r.ConclusionRef) > 128 || len(r.DiscordChannelID) > 128 {
		return store.ErrConnectorInvalid
	}
	if operation == "revise_task" && !valid(r.Change, 8192) {
		return errors.New("revision requires bounded change guidance")
	}
	if operation == "approve_stage" && r.Change != "" {
		return errors.New("combined changes and approval require a revision proposal first")
	}
	for _, v := range []string{r.Summary.Question, r.Summary.Conclusion, r.Summary.AlternativesOrUncertainty, r.Summary.EvidenceBasis, r.Summary.Attribution} {
		if !valid(v, 8192) {
			return errors.New("summary requires question, conclusion, alternatives or uncertainty, evidence and attribution")
		}
	}
	return nil
}

type domain struct {
	s    *Service
	next owneraction.Domain
}

func isDecision(inv owneraction.Invocation) bool {
	return inv.Command == "revise_task" || inv.Command == "approve_stage"
}
func (d domain) Resolve(ctx context.Context, p owneraction.Principal, inv owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	if !isDecision(inv) {
		return d.next.Resolve(ctx, p, inv, at)
	}
	var r Request
	decoder := json.NewDecoder(strings.NewReader(inv.Arguments["request"]))
	decoder.DisallowUnknownFields()
	if len(inv.Arguments) != 1 || decoder.Decode(&r) != nil {
		return owneraction.Facts{}, store.ErrConnectorInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return owneraction.Facts{}, store.ErrConnectorInvalid
	}
	if err := validateRequest(inv.Command, r); err != nil {
		return owneraction.Facts{}, err
	}
	scope, err := d.s.authorize(ctx, p, r.TaskRef)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if r.ConversationRef != "" {
		if _, err = d.s.db.ReadDiscussion(ctx, scope, r.ConversationRef); err != nil {
			return owneraction.Facts{}, err
		}
	}
	reference := ""
	if r.ConclusionRef != "" {
		c, e := d.s.db.ReadDiscussionConclusion(ctx, scope, r.ConclusionRef)
		if e != nil {
			return owneraction.Facts{}, e
		}
		if c.State != "delivered" || c.CommentURL == "" || r.ConversationRef != "" && c.ConversationID != r.ConversationRef {
			return owneraction.Facts{}, errors.New("conclusion reference must be a published conclusion for this task")
		}
		reference = c.CommentURL
	}
	t, err := d.s.boundary.ResolveStageDecisionTarget(ctx, r.TaskRef, r.Stage, inv.Command)
	if err != nil {
		return owneraction.Facts{}, err
	}
	change := r.Change
	if change != "" {
		protected, e := d.s.protect(ctx, change)
		if e != nil {
			return owneraction.Facts{}, e
		}
		if protected != change {
			return owneraction.Facts{}, ErrProtectedChange
		}
	}
	text := fmt.Sprintf("Owner stage decision\n\nTask: %s\nStage: %s\n\nQuestion: %s\n\nConclusion: %s\n\nAlternatives or uncertainty: %s\n\nEvidence basis (client supplied): %s\n\nAttribution: Owner confirms the requested operation only. Client supplied attribution: %s\n\nRequested operation: %s\nRequested change: %s\n\nEffect: %s\n\nDependencies: %s\n\nOnly the supplied summary and saved current target were reviewed; no full chat transcript was supplied. A revision admits continuation, without claiming replacement completion or approval.", r.TaskRef, t.Stage, r.Summary.Question, r.Summary.Conclusion, r.Summary.AlternativesOrUncertainty, r.Summary.EvidenceBasis, r.Summary.Attribution, inv.Command, change, t.Effect, t.Dependencies)
	if reference != "" {
		text += "\n\nPrior discussion conclusion: " + reference
	}
	text, err = d.s.protect(ctx, text)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if len(text) > 48<<10 {
		return owneraction.Facts{}, store.ErrConnectorCapacity
	}
	f := frozen{Target: t, Request: r, Rendered: text}
	payload, _ := json.Marshal(f)
	preview := fmt.Sprintf("Task: %s\nCurrent stage: %s\nCurrent candidate: %s\nRequested effect: %s\nDependencies: %s\n\nExact GitHub decision summary:\n%s", r.TaskRef, t.Stage, t.Summary, t.Effect, t.Dependencies, text)
	preview, err = d.s.protect(ctx, preview)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if !strings.Contains(preview, text) {
		return owneraction.Facts{}, errors.New("preview protection changed reviewed summary; submit corrected summary")
	}
	if len(preview) > 60<<10 {
		return owneraction.Facts{}, store.ErrConnectorCapacity
	}
	facts := owneraction.Facts{Invocation: inv, RepositoryID: t.RepositoryID, RepositoryRef: t.RepositoryRef, TargetKind: "task-stage", TargetID: t.TaskRef, TargetRevision: t.Revision, Summary: preview, Estimate: "Existing stage workflow; revision may start a paid continuation.", Payload: string(payload)}
	// MCP returns both JSON text and structured JSON, and status repeats the exact
	// summary alongside the proposal. Reserve envelope and bounded history space.
	public, _ := json.Marshal(struct {
		Facts   owneraction.Facts
		Summary string
	}{facts, text})
	if len(public) > 24<<10 {
		return owneraction.Facts{}, ErrPreviewTooLarge
	}
	return facts, nil
}
func decode(f owneraction.Facts) (frozen, error) {
	var v frozen
	err := json.Unmarshal([]byte(f.Payload), &v)
	if err != nil || v.Target.TaskRef == "" || v.Rendered == "" {
		return v, store.ErrConnectorInvalid
	}
	return v, nil
}
func (d domain) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	if !isDecision(f.Invocation) {
		return d.next.Recheck(ctx, p, f, at)
	}
	v, err := decode(f)
	if err != nil {
		return err
	}
	if _, err = d.s.authorize(ctx, p, v.Target.TaskRef); err != nil {
		return err
	}
	current, err := d.s.boundary.ResolveStageDecisionTarget(ctx, v.Target.TaskRef, v.Target.Stage, f.Invocation.Command)
	if err != nil {
		return err
	}
	if current.Revision != v.Target.Revision || current.Guard != v.Target.Guard {
		return errors.New("displayed stage, candidate or dependencies changed; request a fresh proposal")
	}
	return nil
}
func (d domain) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	if !isDecision(f.Invocation) {
		return d.next.Execute(ctx, p, f, key, at)
	}
	v, err := decode(f)
	if err != nil {
		return owneraction.Result{}, err
	}
	receipt, err := d.s.boundary.ApplyStageDecision(ctx, v.Target, v.Request.Change, key, at)
	if err != nil {
		return owneraction.Result{}, err
	}
	if receipt.State == "applied" {
		if err = d.s.ensureDelivery(ctx, p, v, key); err != nil {
			return owneraction.Result{}, err
		}
	}
	return result(receipt), nil
}
func (d domain) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	if !isDecision(f.Invocation) {
		return d.next.Recover(ctx, p, f, key, at)
	}
	v, err := decode(f)
	if err != nil {
		return owneraction.Result{}, false, err
	}
	receipt, found, err := d.s.boundary.RecoverStageDecision(ctx, key, at)
	if err != nil || !found {
		return owneraction.Result{}, found, err
	}
	if receipt.State == "applied" {
		if err = d.s.ensureDelivery(ctx, p, v, key); err != nil {
			return owneraction.Result{}, false, err
		}
	}
	return result(receipt), true, nil
}
func result(r store.StageDecisionReceipt) owneraction.Result {
	state := "failed"
	if r.State == "applied" {
		state = "succeeded"
	}
	return owneraction.Result{State: state, Summary: r.Transition + " " + r.Reason, Reference: r.ContinuationRef}
}
func (d domain) ActionCatalog() []owneraction.ActionDefinition {
	var out []owneraction.ActionDefinition
	if c, ok := d.next.(interface {
		ActionCatalog() []owneraction.ActionDefinition
	}); ok {
		out = append(out, c.ActionCatalog()...)
	}
	for _, name := range []string{"revise_task", "approve_stage"} {
		tier, scope, _ := owneraction.Classify(owneraction.Invocation{Command: name})
		out = append(out, owneraction.ActionDefinition{Command: name, Tier: tier, Scope: scope, Arguments: []owneraction.ArgumentDefinition{{Name: "request", Type: "string", Required: true, MaxBytes: 60 << 10}}})
	}
	return out
}
