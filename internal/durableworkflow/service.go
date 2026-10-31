package durableworkflow

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Question identifies the exact domain question an answer addresses.
type Question struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Output contains a safe projection, never raw prompts or logs.
type Output struct {
	State           string           `json:"state"`
	Summary         string           `json:"summary"`
	DomainRef       string           `json:"domain_ref,omitempty"`
	SessionRevision int64            `json:"session_revision,omitempty"`
	Questions       []Question       `json:"questions,omitempty"`
	Items           []map[string]any `json:"items"`
}

// Continuation freezes the job, author revision and question answers.
type Continuation struct {
	Job             string            `json:"job"`
	SessionRevision int64             `json:"session_revision"`
	Answers         map[string]string `json:"answers"`
}

// Adapter is installed by trusted composition. Recovery may only read receipts.
type Adapter interface {
	Resolve(context.Context, owneraction.Principal, Definition, Resolved, Continuation, time.Time) (owneraction.Facts, error)
	Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error
	Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (Output, error)
	Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (Output, bool, error)
}

// AdapterPolicy records an installed adapter's server-owned action classification.
type AdapterPolicy struct{ Kind string }

// Options contains installed implementations and daemon-owned time.
type Options struct {
	DB                *store.DB
	Registry          *Registry
	Adapters          map[string]Adapter
	Policies          map[string]AdapterPolicy
	Now               func() time.Time
	Protect           func(context.Context, string) (string, error)
	NoticeDestination func(context.Context, owneraction.Principal, Definition, Resolved) (string, error)
}

// Service coordinates discovery, frozen proposals and retained job access.
type Service struct {
	db                *store.DB
	registry          *Registry
	adapters          map[string]Adapter
	policies          map[string]AdapterPolicy
	now               func() time.Time
	protect           func(context.Context, string) (string, error)
	noticeDestination func(context.Context, owneraction.Principal, Definition, Resolved) (string, error)
	actions           *owneraction.Service
	fallback          owneraction.Domain
	mu                sync.Mutex
	workerMu          sync.Mutex
	cursorKey         [32]byte
}

// New validates trusted coordinator dependencies.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Registry == nil || o.Now == nil || o.Protect == nil {
		return nil, errors.New("workflow dependencies unavailable")
	}
	policies := map[string]AdapterPolicy{"log-analysis": {Kind: "analysis"}, "standards-authoring": {Kind: "author"}}
	for name, p := range o.Policies {
		if name == "log-analysis" || name == "standards-authoring" {
			if p != policies[name] {
				return nil, errors.New("built-in workflow policy cannot be overridden")
			}
		}
		if p.Kind != "analysis" && p.Kind != "author" {
			return nil, errors.New("adapter policy must use an existing action classification")
		}
		policies[name] = p
	}
	adapters := map[string]Adapter{}
	for k, v := range o.Adapters {
		if v != nil {
			if _, ok := policies[k]; !ok {
				return nil, errors.New("installed adapter requires an explicit policy classification")
			}
			adapters[k] = v
		}
	}
	for _, d := range o.Registry.Definitions() {
		policy, ok := policies[d.Adapter]
		if !ok {
			continue
		}
		mandatory := "standards:management"
		if policy.Kind == "analysis" {
			mandatory = "repository:actions"
		}
		if !slices.Contains(d.RequiredPermissions, mandatory) {
			return nil, errors.New("workflow contract omits its fixed policy permission")
		}
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	return &Service{cursorKey: key, db: o.DB, registry: o.Registry, adapters: adapters, policies: policies, now: o.Now, protect: o.Protect, noticeDestination: o.NoticeDestination}, nil
}

// BindActions attaches the shared confirmation service before serving.
func (s *Service) BindActions(a *owneraction.Service) { s.actions = a }

// WrapDomain routes workflow admissions while preserving existing actions.
func (s *Service) WrapDomain(d owneraction.Domain) owneraction.Domain { s.fallback = d; return s }
func principal(p owneraction.Principal) store.OwnerActionPrincipal {
	return store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID, ChannelID: p.ChannelID, DiscordOwnerID: p.DiscordOwnerID}
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Service) repository(ctx context.Context, ref string) (store.Repo, error) {
	forge, slug, ok := strings.Cut(ref, ":")
	if !ok {
		forge = "github"
		slug = ref
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" {
		return store.Repo{}, errors.New("registered project reference required")
	}
	return s.db.GetRepoBySlug(ctx, forge, owner, name)
}
func (s *Service) command(d Definition) string {
	if s.policies[d.Adapter].Kind == "analysis" {
		return "workflow-analysis"
	}
	return "workflow-author"
}
func (s *Service) scope(d Definition) string {
	if s.policies[d.Adapter].Kind == "analysis" {
		return "repository:actions"
	}
	return "standards:management"
}
func (s *Service) authorize(ctx context.Context, p owneraction.Principal, d Definition, project string) error {
	// Fixed classification is independent of registry-supplied permission labels.
	required := s.scope(d)
	if _, ok := s.policies[d.Adapter]; !ok {
		return owneraction.ErrUnavailable
	}
	for _, ref := range []string{d.BindingProject, project} {
		record := store.OwnerAction{Principal: principal(p), Scope: required}
		if ref != "" {
			r, err := s.repository(ctx, ref)
			if err != nil {
				return err
			}
			record.RepositoryID = r.ID
			record.RepositoryRef = r.Forge + ":" + r.Slug()
		}
		if err := s.db.CheckOwnerActionAuthority(ctx, principal(p), record, s.now()); err != nil {
			return err
		}
		for _, permission := range d.RequiredPermissions {
			extra := record
			extra.Scope = permission
			if err := s.db.CheckOwnerActionAuthority(ctx, principal(p), extra, s.now()); err != nil {
				return err
			}
		}
	}
	return nil
}

// Summary presents an authorized installed workflow and its availability.
type Summary struct {
	Definition
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

// CatalogueEntry is a small discovery view; Describe carries complete schemas.
type CatalogueEntry struct {
	Ref                 string      `json:"workflow_ref"`
	Version             string      `json:"version"`
	Digest              string      `json:"definition_digest"`
	Name                string      `json:"name"`
	Purpose             string      `json:"purpose"`
	BindingProject      string      `json:"binding_project,omitempty"`
	ProjectMode         ProjectMode `json:"project_mode"`
	FixedProject        string      `json:"fixed_project,omitempty"`
	RequiredPermissions []string    `json:"required_permissions"`
	DeadlineSeconds     int         `json:"deadline_seconds"`
	Impact              string      `json:"impact"`
	Available           bool        `json:"available"`
	UnavailableReason   string      `json:"unavailable_reason,omitempty"`
}

// Page is an authorized catalogue page.
type Page struct {
	Workflows  []CatalogueEntry `json:"workflows"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
type cursor struct {
	Owner                       string
	Generation                  int64
	Grant, Project, Fingerprint string
	Offset                      int
	Job                         string
	Revision                    int64
}

func (s *Service) encodeCursor(c cursor) string {
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) decodeCursor(value string) (cursor, error) {
	var c cursor
	raw, signature, ok := strings.Cut(value, ".")
	if !ok {
		return c, errors.New("invalid workflow cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return c, err
	}
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, errors.New("invalid workflow cursor")
	}
	err = json.Unmarshal(b, &c)
	return c, err
}
func (s *Service) reader(ctx context.Context, p owneraction.Principal) (string, error) {
	v, e := s.db.OwnerActionReaderFingerprint(ctx, principal(p), s.now())
	return hash(v), e
}

// List filters current authority before applying pagination.
func (s *Service) List(ctx context.Context, p owneraction.Principal, project, raw string, limit int) (Page, error) {
	if limit < 1 || limit > 50 || len(raw) > 4096 {
		return Page{}, errors.New("invalid workflow page")
	}
	fp, err := s.reader(ctx, p)
	if err != nil {
		return Page{}, err
	}
	c := cursor{Owner: p.OwnerID, Generation: p.OwnerGeneration, Grant: p.GrantID, Project: project, Fingerprint: fp}
	if raw != "" {
		old, e := s.decodeCursor(raw)
		if e != nil || old.Owner != c.Owner || old.Generation != c.Generation || old.Grant != c.Grant || old.Project != c.Project || old.Fingerprint != c.Fingerprint || old.Job != "" || old.Offset < 0 {
			return Page{}, errors.New("invalid workflow cursor")
		}
		c = old
	}
	visible := []CatalogueEntry{}
	for _, d := range s.registry.Definitions() {
		if project != "" && d.BindingProject != "" && d.BindingProject != project {
			continue
		}
		target := project
		if d.ProjectMode == ProjectFixed {
			target = d.FixedProject
			if project != "" && d.BindingProject == "" && project != target {
				continue
			}
		}
		if d.ProjectMode == ProjectNone {
			target = ""
		}
		if err = s.authorize(ctx, p, d, target); err != nil {
			continue
		}
		available := s.adapters[d.Adapter] != nil
		reason := ""
		if !available {
			reason = "installed adapter unavailable"
		}
		visible = append(visible, catalogueEntry(d, available, reason))
	}
	if c.Offset > len(visible) {
		return Page{}, errors.New("invalid workflow cursor")
	}
	end := min(len(visible), c.Offset+limit)
	page := Page{Workflows: visible[c.Offset:end]}
	if end < len(visible) {
		c.Offset = end
		page.NextCursor = s.encodeCursor(c)
	}
	return page, nil
}

// Describe inspects one authorized version without admitting work.
func (s *Service) Describe(ctx context.Context, p owneraction.Principal, ref, version, project string) (Summary, error) {
	d, e := s.registry.Get(ref, version)
	if e != nil {
		return Summary{}, e
	}
	target := project
	if d.ProjectMode == ProjectFixed {
		if project != "" {
			return Summary{}, errors.New("fixed workflow rejects project overrides")
		}
		target = d.FixedProject
	}
	if d.ProjectMode == ProjectNone && project != "" {
		return Summary{}, errors.New("workflow does not accept project")
	}
	if e = s.authorize(ctx, p, d, target); e != nil {
		return Summary{}, e
	}
	return Summary{Definition: d, Available: s.adapters[d.Adapter] != nil}, nil
}

type frozen struct {
	Definition        Definition        `json:"definition"`
	Resolved          Resolved          `json:"resolved"`
	Continuation      Continuation      `json:"continuation"`
	Domain            owneraction.Facts `json:"domain"`
	DomainPayload     string            `json:"domain_payload"`
	Job               string            `json:"job"`
	NoticeDestination string            `json:"notice_destination,omitempty"`
}

func unpack(f owneraction.Facts) (frozen, error) {
	var v frozen
	e := json.Unmarshal([]byte(f.Payload), &v)
	v.Domain.Payload = v.DomainPayload
	return v, e
}

type proposalContextKey struct{}

// Proposal links immutable review facts to the reserved durable identity.
type Proposal struct {
	Job      string               `json:"job"`
	Proposal owneraction.Proposal `json:"proposal"`
}

// Propose freezes a start request without launching its adapter.
func (s *Service) Propose(ctx context.Context, p owneraction.Principal, ref, version, project string, inputs map[string]any, key string) (Proposal, error) {
	return s.propose(ctx, p, ref, version, project, inputs, key, Continuation{})
}

// Continue proposes exact answers against the retained domain revision.
func (s *Service) Continue(ctx context.Context, p owneraction.Principal, c Continuation, key string) (Proposal, error) {
	job, e := s.db.ReadWorkflowJob(ctx, c.Job)
	if e != nil {
		return Proposal{}, e
	}
	var v frozen
	if e = json.Unmarshal([]byte(job.Snapshot), &v); e != nil {
		return Proposal{}, e
	}
	if e = s.authorizeJob(ctx, p, job, v); e != nil {
		return Proposal{}, e
	}
	current, err := s.registry.Get(v.Definition.Ref, v.Definition.Version)
	if err != nil || current.Digest != v.Definition.Digest {
		return Proposal{}, errors.New("workflow definition changed")
	}
	old, err := s.db.WorkflowRequestByKey(ctx, p.OwnerID, p.OwnerGeneration, key)
	if err == nil {
		expected := requestIdentity(v.Definition, v.Resolved, c, v.NoticeDestination)
		if expected != old.Digest {
			return Proposal{}, owneraction.ErrRequestConflict
		}
		proposal, e := s.actions.Inspect(ctx, p, old.ProposalID)
		return Proposal{Job: old.JobID, Proposal: proposal}, e
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Proposal{}, err
	}
	var out Output
	if e = json.Unmarshal([]byte(job.Result), &out); e != nil {
		return Proposal{}, e
	}
	if job.State != "waiting_for_input" || out.SessionRevision != c.SessionRevision || len(c.Answers) == 0 {
		return Proposal{}, errors.New("stale author revision")
	}
	for id, text := range c.Answers {
		found := false
		for _, q := range out.Questions {
			found = found || q.ID == id
		}
		if !found || strings.TrimSpace(text) == "" {
			return Proposal{}, errors.New("unknown or empty question answer")
		}
	}
	return s.propose(ctx, p, v.Definition.Ref, v.Definition.Version, v.Resolved.Project, v.Resolved.Inputs, key, c)
}
func (s *Service) propose(ctx context.Context, p owneraction.Principal, ref, version, project string, inputs map[string]any, key string, c Continuation) (Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" || len(key) > 128 || s.actions == nil {
		return Proposal{}, errors.New("invalid workflow request")
	}
	d, e := s.registry.Get(ref, version)
	if e != nil {
		return Proposal{}, e
	}
	var resolved Resolved
	if c.Job != "" {
		resolved = Resolved{Project: project, Inputs: inputs}
	} else {
		resolved, e = Resolve(d, project, inputs)
		if e != nil {
			return Proposal{}, e
		}
	}
	if resolved.Project != "" {
		repo, err := s.repository(ctx, resolved.Project)
		if err != nil {
			return Proposal{}, err
		}
		resolved.Project = repo.Forge + ":" + repo.Slug()
	}
	if s.adapters[d.Adapter] == nil {
		return Proposal{}, errors.New("workflow adapter unavailable")
	}
	if e = s.authorize(ctx, p, d, resolved.Project); e != nil {
		return Proposal{}, e
	}
	destination := ""
	if s.noticeDestination != nil {
		destination, e = s.noticeDestination(ctx, p, d, resolved)
		if e != nil {
			destination = ""
		}
	}
	if c.Job != "" {
		saved, err := s.db.ReadWorkflowJob(ctx, c.Job)
		if err != nil {
			return Proposal{}, err
		}
		var original frozen
		if err = json.Unmarshal([]byte(saved.Snapshot), &original); err != nil {
			return Proposal{}, err
		}
		destination = original.NoticeDestination
	}
	requestDigest := requestIdentity(d, resolved, c, destination)
	old, e := s.db.WorkflowRequestByKey(ctx, p.OwnerID, p.OwnerGeneration, key)
	if e == nil {
		if old.Digest != requestDigest {
			return Proposal{}, owneraction.ErrRequestConflict
		}
		proposal, err := s.actions.Inspect(ctx, p, old.ProposalID)
		return Proposal{Job: old.JobID, Proposal: proposal}, err
	}
	if !errors.Is(e, store.ErrNotFound) {
		return Proposal{}, e
	}
	job := c.Job
	if job == "" {
		job = "job-" + hash(struct {
			Owner      string
			Generation int64
			Key        string
		}{p.OwnerID, p.OwnerGeneration, key})[:40]
	}
	v := frozen{Definition: d, Resolved: resolved, Continuation: c, Job: job, NoticeDestination: destination}
	raw, _ := json.Marshal(v)
	inv := owneraction.Invocation{Command: s.command(d), Arguments: map[string]string{"snapshot": string(raw)}}
	proposal, e := s.actions.Propose(context.WithValue(ctx, proposalContextKey{}, true), p, inv, "workflow:"+hash(struct {
		Owner      string
		Generation int64
		Key        string
	}{p.OwnerID, p.OwnerGeneration, key}))
	if e != nil {
		return Proposal{}, e
	}
	v, e = unpack(proposal.Facts)
	if e != nil {
		return Proposal{}, e
	}
	raw, _ = json.Marshal(v)
	expected := int64(0)
	kind := "start"
	if c.Job != "" {
		kind = "continue"
		j, err := s.db.ReadWorkflowJob(ctx, job)
		if err != nil {
			return Proposal{}, err
		}
		expected = j.Revision
	}
	req := store.WorkflowRequest{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Key: key, Digest: requestDigest, ProposalID: proposal.ID, JobID: job, Snapshot: string(raw), Inputs: string(raw), Kind: kind, ExpectedRevision: expected, CreatedAt: s.now()}
	saved, e := s.db.ReserveWorkflowRequest(ctx, req)
	if e != nil {
		return Proposal{}, e
	}
	if saved.ProposalID != proposal.ID {
		proposal, e = s.actions.Inspect(ctx, p, saved.ProposalID)
	}
	return Proposal{Job: saved.JobID, Proposal: proposal}, e
}
func isWorkflow(inv owneraction.Invocation) bool {
	return inv.Command == "workflow-analysis" || inv.Command == "workflow-author"
}

// Resolve handles workflow snapshots; other existing actions keep their adapter.
func (s *Service) Resolve(ctx context.Context, p owneraction.Principal, inv owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	if !isWorkflow(inv) {
		if s.fallback == nil {
			return owneraction.Facts{}, owneraction.ErrUnavailable
		}
		return s.fallback.Resolve(ctx, p, inv, at)
	}
	if allowed, _ := ctx.Value(proposalContextKey{}).(bool); !allowed {
		return owneraction.Facts{}, owneraction.ErrUnavailable
	}
	var v frozen
	if e := json.Unmarshal([]byte(inv.Arguments["snapshot"]), &v); e != nil {
		return owneraction.Facts{}, e
	}
	d, e := s.registry.Get(v.Definition.Ref, v.Definition.Version)
	if e != nil || d.Digest != v.Definition.Digest || s.command(d) != inv.Command {
		return owneraction.Facts{}, errors.New("workflow definition changed")
	}
	if e = s.authorize(ctx, p, d, v.Resolved.Project); e != nil {
		return owneraction.Facts{}, e
	}
	adapter := s.adapters[d.Adapter]
	if adapter == nil {
		return owneraction.Facts{}, owneraction.ErrUnavailable
	}
	inputJSON := mustJSON(v.Resolved.Inputs) + mustJSON(v.Continuation)
	protected, err := s.protect(ctx, inputJSON)
	if err != nil || protected != inputJSON {
		return owneraction.Facts{}, errors.New("request contains protected values; revise before proposing")
	}
	domain, e := adapter.Resolve(ctx, p, d, v.Resolved, v.Continuation, at)
	if e != nil {
		return owneraction.Facts{}, e
	}
	v.Domain = domain
	v.DomainPayload = domain.Payload
	raw, _ := json.Marshal(v)
	f := domain
	if v.Resolved.Project != "" {
		repo, err := s.repository(ctx, v.Resolved.Project)
		if err != nil {
			return owneraction.Facts{}, err
		}
		f.RepositoryID = repo.ID
		f.RepositoryRef = repo.Forge + ":" + repo.Slug()
	}
	f.Invocation = inv
	f.Payload = string(raw)
	f.TargetKind = "workflow"
	f.TargetID = v.Job
	f.TargetRevision = d.Digest + "; " + domain.TargetRevision
	binding := d.BindingProject
	if binding == "" {
		binding = "independent"
	}
	target := v.Resolved.Project
	if target == "" {
		target = "book-only (no project)"
	}
	f.Summary = fmt.Sprintf("%s version %s; definition binding %s; target %s; inputs %s; notice destination %s; %s", d.Ref, d.Version, binding, target, mustJSON(v.Resolved.Inputs), v.NoticeDestination, domain.Summary)
	protected, err = s.protect(ctx, f.Summary)
	if err != nil || protected != f.Summary {
		return owneraction.Facts{}, errors.New("review facts contain protected values; revise before proposing")
	}

	f.Estimate = strconv.Itoa(d.DeadlineSeconds) + " seconds"
	return f, nil
}
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// Recheck verifies current installation, binding, target and domain authority.
func (s *Service) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	if !isWorkflow(f.Invocation) {
		return s.fallback.Recheck(ctx, p, f, at)
	}
	v, e := unpack(f)
	if e != nil {
		return e
	}
	d, e := s.registry.Get(v.Definition.Ref, v.Definition.Version)
	if e != nil || d.Digest != v.Definition.Digest {
		return errors.New("workflow definition changed")
	}
	if e = s.authorize(ctx, p, d, v.Resolved.Project); e != nil {
		return e
	}
	a := s.adapters[d.Adapter]
	if a == nil {
		return owneraction.ErrUnavailable
	}
	return a.Recheck(ctx, p, v.Domain, at)
}

// Execute hands an admitted workflow to the durable queue without running it.
func (s *Service) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	if !isWorkflow(f.Invocation) {
		return s.fallback.Execute(ctx, p, f, key, at)
	}
	v, e := unpack(f)
	if e != nil {
		return owneraction.Result{}, e
	}
	a, err := s.db.WorkflowAttemptByExecution(ctx, key)
	if err != nil || a.JobID != v.Job {
		return owneraction.Result{State: "failed", Summary: "Workflow admission is missing; request a fresh proposal"}, nil //nolint:nilerr // Missing admission is a pre-effect refusal, not an ambiguous dispatch.
	}
	return owneraction.Result{State: "executing", Summary: "Workflow queued for background execution", Reference: v.Job}, nil
}

// Recover preserves action authority until the linked attempt settles.
func (s *Service) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	if !isWorkflow(f.Invocation) {
		return s.fallback.Recover(ctx, p, f, key, at)
	}
	v, e := unpack(f)
	if e != nil {
		return owneraction.Result{}, false, e
	}
	job, e := s.db.ReadWorkflowJob(ctx, v.Job)
	if e != nil {
		return owneraction.Result{}, false, e
	}
	state := job.State
	if state == "queued" || state == "running" {
		state = "executing"
	}
	if state == "waiting_for_input" {
		state = "succeeded"
	}
	return owneraction.Result{State: state, Summary: job.Summary, Reference: job.ID}, true, nil
}
func (s *Service) authorizeJob(ctx context.Context, p owneraction.Principal, j store.WorkflowJob, v frozen) error {
	if j.OwnerID != p.OwnerID || j.OwnerGeneration != p.OwnerGeneration {
		return store.ErrNotFound
	}
	return s.authorize(ctx, p, v.Definition, v.Resolved.Project)
}

// ReconcileNotices preserves ambiguous sends as unconfirmed across restart.
func (s *Service) ReconcileNotices(ctx context.Context) error {
	return s.db.ReconcileWorkflowNotices(ctx)
}

func requestIdentity(d Definition, r Resolved, c Continuation, destination string) string {
	return hash(struct {
		Definition   Definition
		Resolved     Resolved
		Continuation Continuation
		Destination  string
	}{d, r, c, destination})
}

// AuthorizeNotice rechecks the event's original grant and frozen destination before sending.
func (s *Service) AuthorizeNotice(ctx context.Context, key, channel string) error {
	prefix, raw, ok := strings.Cut(key, ":")
	if !ok || prefix != "workflow-notice" {
		return store.ErrConnectorInvalid
	}
	id, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || id < 1 {
		return store.ErrConnectorInvalid
	}
	n, e := s.db.ReadWorkflowNotice(ctx, id)
	if e != nil {
		return e
	}
	if n.Destination != channel || n.State != "sending" {
		return store.ErrConnectorScope
	}
	attempt, e := s.db.WorkflowAttemptAtRevision(ctx, n.JobID, n.Revision)
	if e != nil {
		return e
	}
	record, e := s.db.ReadOwnerAction(ctx, attempt.ProposalID)
	if e != nil {
		return e
	}
	request, e := s.db.WorkflowRequestByProposal(ctx, attempt.ProposalID)
	if e != nil {
		return e
	}
	var v frozen
	if e = json.Unmarshal([]byte(request.Inputs), &v); e != nil {
		return e
	}
	p := principalFromStore(record.Principal)
	if e = s.authorize(ctx, p, v.Definition, v.Resolved.Project); e != nil {
		return e
	}
	if v.NoticeDestination != channel || s.noticeDestination == nil {
		return store.ErrConnectorScope
	}
	current, e := s.noticeDestination(ctx, p, v.Definition, v.Resolved)
	if e != nil {
		return e
	}
	if current != channel {
		return store.ErrConnectorScope
	}
	return nil
}

func catalogueEntry(d Definition, available bool, reason string) CatalogueEntry {
	return CatalogueEntry{Ref: d.Ref, Version: d.Version, Digest: d.Digest, Name: d.Name, Purpose: d.Purpose, BindingProject: d.BindingProject, ProjectMode: d.ProjectMode, FixedProject: d.FixedProject, RequiredPermissions: d.RequiredPermissions, DeadlineSeconds: d.DeadlineSeconds, Impact: d.Impact, Available: available, UnavailableReason: reason}
}
