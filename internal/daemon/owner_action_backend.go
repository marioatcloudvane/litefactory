package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

// ownerActionRuntime owns one executor independently of Discord availability.
// Discord can join later without replacing the execution claims or worker.
type ownerActionRuntime struct {
	reconcileResearch func(context.Context) error
	sanitizer         *redact.Redactor
	adapter           atomic.Pointer[discord.Adapter]
	discussions       *taskdiscussion.Service
	actions           *owneraction.Service
	workflows         *durableworkflow.Service
	// brainstormDrafts is bound once the brainstorm service is prepared.
	brainstormDrafts brainstormDraftsPort
	notices          atomic.Pointer[messaging.Service]
	once             sync.Once
}

func (r *ownerActionRuntime) start(ctx context.Context, wg *sync.WaitGroup, logger *slog.Logger) {
	r.once.Do(func() {
		if r.discussions != nil {
			// One publication loop per runtime ends with the serving context and is
			// joined by its wait group; provider calls retain their own finite bounds.
			wg.Go(func() {
				timer := time.NewTicker(time.Second)
				defer timer.Stop()
				for {
					if ctx.Err() != nil {
						return
					}
					if err := r.discussions.PublishPending(ctx); err != nil {
						logger.WarnContext(ctx, "discussion publication requires reconciliation")
					}
					select {
					case <-ctx.Done():
						return
					case <-timer.C:
					}
				}
			})
		}
		r.actions.SetLifetime(ctx)
		if r.workflows != nil {
			wg.Go(func() {
				if err := r.workflows.ReconcileNotices(ctx); err != nil {
					logger.WarnContext(ctx, "workflow notice recovery requires inspection")
				}
				if err := r.workflows.RecoverRunning(ctx); err != nil {
					logger.WarnContext(ctx, "workflow recovery requires inspection")
				}
				timer := time.NewTicker(time.Second)
				defer timer.Stop()
				for {
					if ctx.Err() != nil {
						return
					}
					if err := r.workflows.RecoverExpired(ctx); err != nil {
						logger.WarnContext(ctx, "expired workflow reconciliation requires inspection")
					}
					if _, err := r.workflows.RunOnce(ctx); err != nil {
						logger.WarnContext(ctx, "workflow attempt requires inspection")
					}
					select {
					case <-ctx.Done():
						return
					case <-timer.C:
					}
				}
			})
		}
		wg.Go(func() {
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				if r.reconcileResearch != nil {
					if err := r.reconcileResearch(ctx); err != nil {
						logger.WarnContext(ctx, "chat answer note remains pending")
					}
				}
				if err := r.actions.Tick(ctx); err != nil {
					logger.WarnContext(ctx, "owner action progression needs inspection")
				}
				if chat := r.notices.Load(); chat != nil {
					if err := chat.OwnerActionNotifications(ctx, time.Now()); err != nil {
						logger.WarnContext(ctx, "owner action outcome notification remains pending")
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
		})
	})
}

// prepareIndependentOwnerActions composes the core coordinator before transport
// activation. Remote wiring may later attach its domains to the same instance.
func (d *Daemon) prepareIndependentOwnerActions(options *ChatOptions, sanitizer *redact.Redactor) {
	runtime := d.ownerActions
	if runtime == nil {
		runtime = &ownerActionRuntime{}
		if options == nil {
			options = &ChatOptions{}
		}
		clock := options.Now
		if clock == nil {
			clock = time.Now
		}
		if sanitizer == nil && options.Answers != nil {
			sanitizer = options.Answers.Redactor
		}
		if sanitizer == nil {
			sanitizer = redact.New(redact.Options{Logger: d.log})
		}
		runtime.sanitizer = sanitizer
		runtime.reconcileResearch = d.researchAnswers(sanitizer).Reconcile
		protect := func(ctx context.Context, text string) (string, error) {
			protected, err := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
			return protected.String(), err
		}
		research := ownerActionResearch{daemon: d, sanitizer: sanitizer}
		availability := owneractions.Availability{Answer: d.forge != nil, Diagnose: d.forge != nil}
		answers := ask.Service{Repositories: factory.NewMessaging(d.db), Collector: ask.Collector{Factory: factory.NewTaskFacts(d.db)}, Redactor: sanitizer}
		if options.Answers != nil {
			answers = *options.Answers
		}
		if answers.Redactor == nil {
			answers.Redactor = sanitizer
		}
		if options.Instructions != nil {
			answers.Instructions = options.Instructions
		}
		if options.InstructionDiagnostics != nil {
			answers.InstructionDiagnostics = options.InstructionDiagnostics
		}
		writer, _ := d.forge.(ChatIssueForge)
		var creator *ChatIssueCreator
		if writer != nil && d.filter.Identities().BotUserID > 0 {
			creator = &ChatIssueCreator{DB: d.db, Forge: writer, BotUserID: d.filter.Identities().BotUserID, Redactor: sanitizer, Now: clock}
			availability.Confirm = true
		}
		var drafter *featuredraft.Service
		if answers.Model != nil {
			drafter = (&feature.Drafter{Instructions: answers.Instructions, InstructionDiagnostics: answers.InstructionDiagnostics, Repositories: answers.Repositories, Model: answers.Model, Redactor: answers.Redactor, Store: d.db, Protect: protect, Now: clock}).Factory()
			availability.Draft = true
		}
		var standards *ConversationStandards
		if options.Standards != nil {
			operations := *options.Standards
			operations.OwnerID = options.Settings.OwnerID
			operations.Authorized = func(ctx context.Context, request messaging.StandardsRequest) error {
				adapter := runtime.adapter.Load()
				if adapter == nil {
					return errors.New("discord transport unavailable")
				}
				permitted, bound := false, false
				for _, channel := range options.Settings.ChannelIDs {
					permitted = permitted || channel == request.ChannelID
				}
				if options.Conversations != nil {
					for _, binding := range options.Conversations.Bindings {
						bound = bound || binding.Channel == request.ChannelID
					}
				}
				if !permitted || !bound {
					return errors.New("standards conversation is not authorized")
				}
				return adapter.CheckIntakeCapability(ctx, request.ChannelID)
			}
			standards = &operations
			availability.Standards = true
		}
		targets := owneractions.NewTargets(d.db)
		render := func(ctx context.Context, q owneractions.Question) (string, error) {
			return messaging.RenderQuestion(ctx, q, protect, presentation.MessageLimit)
		}
		availability.Brainstorm = true
		var bugFiles owneraction.Domain
		if labels, ok := d.bugFilingForge().(owneractions.BugLabels); ok {
			availability.BugFile = true
			bugFiles = owneractions.BugFiles{DB: d.db, Labels: labels, Protect: protect, Availability: availability}
		}
		core := owneractions.Domain{
			Actions:      owneractions.Actions{FallbackTransport: "mcp", Targets: targets, Protect: protect, Research: research, Features: independentFeatureEffects{drafter: drafter, confirmer: creator}, Answer: owneractions.AnswerAdmission{Research: research, Protect: protect, Render: render}, Availability: availability},
			Diagnosis:    owneractions.Diagnosis{Targets: targets, Protect: protect, Effects: doctorStartEffects{daemon: d}, Availability: availability},
			Standards:    owneractions.Standards{Targets: targets, Protect: protect, Available: standards != nil, Frozen: standards},
			Brainstorm:   owneractions.BrainstormTasks{DB: d.db, Drafts: &runtime.brainstormDrafts, Protect: protect, Availability: availability},
			BugFile:      bugFiles,
			Availability: availability,
		}
		domain := independentOwnerActionDomain{Domain: core, standards: standards, brainstorm: &runtime.brainstormDrafts}
		runtime.actions = owneraction.New(d.db, domain, clock)
	}
	d.ownerActions = runtime
	if d.remoteConnector == nil {
		return
	}
	if d.remoteConnector.actions == runtime {
		return
	}
	if options == nil {
		options = &ChatOptions{}
	}
	clock := options.Now
	if clock == nil {
		clock = time.Now
	}
	if sanitizer == nil {
		sanitizer = redact.New(redact.Options{Logger: d.log})
	}
	registry, err := installedWorkflows(d.remoteConnector.config.Workflows)
	if err != nil {
		d.log.Error("workflow registration unavailable")
	} else {
		adapters := map[string]durableworkflow.Adapter{}
		if options.Standards != nil {
			a := standardsWorkflowAdapter{standards: options.Standards, db: d.db}
			if options.Standards.Authoring != nil && options.Standards.Authoring.Runner != nil {
				adapters["standards-authoring"] = a
			}
			if options.Standards.Analyses != nil && options.Standards.Analyses.Start != nil {
				adapters["log-analysis"] = a
			}
		}
		protect := func(ctx context.Context, text string) (string, error) {
			body, e := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
			return body.String(), e
		}
		destination := func(ctx context.Context, p owneraction.Principal, definition durableworkflow.Definition, resolved durableworkflow.Resolved) (string, error) {
			if runtime.notices.Load() == nil {
				return "", nil
			}
			channel := ""
			for _, association := range d.remoteConnector.config.WorkflowNotices {
				if association.Project == resolved.Project {
					channel = association.ChannelID
					break
				}
			}
			if channel == "" {
				return "", nil
			}
			allowed := false
			for _, id := range options.Settings.ChannelIDs {
				allowed = allowed || id == channel
			}
			if !allowed {
				return "", nil
			}
			return channel, nil
		}
		workflows, e := durableworkflow.New(durableworkflow.Options{DB: d.db, Registry: registry, Adapters: adapters, Now: clock, Protect: protect, NoticeDestination: destination})
		if e == nil {
			runtime.actions.DecorateDomain(workflows.WrapDomain)
			workflows.BindActions(runtime.actions)
			runtime.workflows = workflows
			if d.chat != nil {
				bindWorkflowNoticeAuthorizer(d.chat.service, d.chat.adapter, workflows)
			}
			d.remoteConnector.server.SetWorkflows(workflows)
		} else {
			d.log.Error("workflow coordinator unavailable")
		}
	}
	d.remoteConnector.actions = runtime
	d.remoteConnector.server.SetActions(runtime.actions)
}

type independentOwnerActionDomain struct {
	owneractions.Domain
	standards  *ConversationStandards
	brainstorm *brainstormDraftsPort
}

func (d independentOwnerActionDomain) enabled(inv owneraction.Invocation) bool {
	if owneractions.BrainstormCommand(inv.Command) {
		return d.Availability.Allows(inv.Command) && d.brainstorm != nil && d.brainstorm.ready()
	}
	if !owneractions.StandardsCommand(inv.Command) {
		return d.Availability.Allows(inv.Command)
	}
	operation, err := owneractions.StandardsOperation(inv)
	if d.standards == nil || err != nil {
		return false
	}
	for _, entry := range d.Domain.ActionCatalog() {
		if entry.Command == inv.Command && entry.Subaction == inv.Arguments["action"] {
			return d.standards.capability(operation) == nil
		}
	}
	return false
}
func (d independentOwnerActionDomain) Resolve(ctx context.Context, p owneraction.Principal, inv owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	if !d.enabled(inv) {
		return owneraction.Facts{}, owneraction.ErrUnavailable
	}
	return d.Domain.Resolve(ctx, p, inv, at)
}
func (d independentOwnerActionDomain) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	if !d.enabled(f.Invocation) {
		return owneraction.ErrUnavailable
	}
	return d.Domain.Recheck(ctx, p, f, at)
}

func (d independentOwnerActionDomain) ActionCatalog() []owneraction.ActionDefinition {
	out := []owneraction.ActionDefinition{}
	for _, entry := range d.Domain.ActionCatalog() {
		if d.enabled(owneraction.Invocation{Command: entry.Command, Arguments: map[string]string{"action": entry.Subaction}}) {
			out = append(out, entry)
		}
	}
	return out
}

type ownerActionResearch struct {
	daemon    *Daemon
	sanitizer *redact.Redactor
}

func (a ownerActionResearch) Current(ctx context.Context, ref string) (messaging.Question, error) {
	return (chatResearch{a.daemon}).Current(ctx, ref)
}
func (a ownerActionResearch) Answer(ctx context.Context, ref, revision, text string, at time.Time) (messaging.Question, error) {
	return (chatResearch{a.daemon}).answerProtected(ctx, ref, revision, text, at, a.sanitizer)
}
func (ownerActionResearch) Queue(context.Context, messaging.DiagnosisRequest) error {
	return errors.New("an owner action starts the Doctor through its own effect")
}

// doctorStartEffects starts the Doctor on the reviewed task. The action's
// execution key names the start, so a replay finds the same Doctor run.
type doctorStartEffects struct{ daemon *Daemon }

func (e doctorStartEffects) DiagnosisAvailable() bool { return e.daemon.forge != nil }
func (e doctorStartEffects) Diagnose(ctx context.Context, task, note, key string) (owneraction.Result, error) {
	if err := e.daemon.startDoctor(ctx, task, note, key); err != nil {
		var refused *doctorRefusal
		if errors.As(err, &refused) {
			return owneraction.Result{State: "failed", Summary: refused.Error()}, nil
		}
		return owneraction.Result{}, err
	}
	return owneraction.Result{State: "succeeded", Summary: doctorStartedSummary(task)}, nil
}
func (e doctorStartEffects) RecoverDiagnosis(ctx context.Context, task, key string) (owneraction.Result, bool, error) {
	started, err := e.daemon.doctorStarted(ctx, task, key)
	if err != nil || !started {
		return owneraction.Result{}, false, err
	}
	return owneraction.Result{State: "succeeded", Summary: doctorStartedSummary(task)}, true, nil
}

type independentFeatureEffects struct {
	drafter   *featuredraft.Service
	confirmer *ChatIssueCreator
}

func (e independentFeatureEffects) DraftAvailable() bool {
	return e.drafter.Available()
}
func (e independentFeatureEffects) ConfirmAvailable() bool { return e.confirmer != nil }
func (e independentFeatureEffects) Draft(ctx context.Context, transport, channel, key, project, note string, at time.Time) (owneraction.Result, error) {
	result, err := e.drafter.Reply(ctx, transport, channel, key, project, note, at)
	return result.ActionResult(), err
}
func (e independentFeatureEffects) Confirm(ctx context.Context, transport, channel, key, code string, at time.Time) (owneraction.Result, error) {
	work, assessment := presentation.WithAssessment(ctx)
	summary, err := e.confirmer.Confirm(work, transport, channel, key, code, at)
	return independentAssessedResult(assessment, summary), err
}
func (e independentFeatureEffects) RecoverFeature(ctx context.Context, transport, channel, code string, at time.Time) (string, bool, error) {
	if e.confirmer == nil {
		return "", false, nil
	}
	return e.confirmer.RecoverOwnerActionFeature(ctx, transport, channel, code, at)
}
func independentAssessedResult(assessment *presentation.Assessment, summary string) owneraction.Result {
	result := owneraction.Result{State: "succeeded", Summary: summary}
	switch assessment.Status {
	case presentation.Uncertain:
		result.State = "uncertain"
	case presentation.Refusal, presentation.ReportableError:
		result.State = "failed"
	}
	return result
}

// brainstormDraftsPort late-binds the brainstorm service, which is prepared
// after the owner action domain, to the brainstorm task owner actions.
type brainstormDraftsPort struct {
	service atomic.Pointer[brainstorm.Service]
}

// bind publishes the prepared brainstorm service to the brainstorm task actions.
func (b *brainstormDraftsPort) bind(s *brainstorm.Service) { b.service.Store(s) }

func (b *brainstormDraftsPort) ready() bool { return b.service.Load() != nil }

func (b *brainstormDraftsPort) IssueDrafts(ctx context.Context, p owneraction.Principal, brainstormID, draftSetID string, keys []string, generation int) (store.BrainstormDraftSet, []owneractions.BrainstormIssueDraft, error) {
	s := b.service.Load()
	if s == nil {
		return store.BrainstormDraftSet{}, nil, owneraction.ErrUnavailable
	}
	set, drafts, err := s.IssueDrafts(ctx, p, brainstormID, draftSetID, keys, generation)
	if err != nil {
		return set, nil, err
	}
	out := make([]owneractions.BrainstormIssueDraft, len(drafts))
	for i, d := range drafts {
		out[i] = owneractions.BrainstormIssueDraft{DraftID: d.DraftID, Key: d.Key, Title: d.Title, Body: d.Body, Marker: d.Marker, Generation: d.Generation, Ledger: d.Ledger, LedgerDigest: d.LedgerDigest}
	}
	return set, out, nil
}
