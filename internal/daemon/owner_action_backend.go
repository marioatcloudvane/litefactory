package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

// ownerActionRuntime owns one executor independently of Discord availability.
// Discord can join later without replacing the execution claims or worker.
type ownerActionRuntime struct {
	discussions *taskdiscussion.Service
	actions     *owneraction.Service
	workflows   *durableworkflow.Service
	notices     atomic.Pointer[messaging.Service]
	once        sync.Once
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

// prepareIndependentOwnerActions runs before serving. A healthy Discord backend
// already owns these same adapters; otherwise composition installs them directly.
func (d *Daemon) prepareIndependentOwnerActions(options *ChatOptions, sanitizer *redact.Redactor) {
	runtime := &ownerActionRuntime{}
	if d.chat != nil {
		runtime.actions = d.chat.service.OwnerActions()
		runtime.notices.Store(d.chat.service)
	} else {
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
		backend := messaging.New(d.db, "mcp", "", nil, nil, messaging.FactoryStatus{DB: d.db})
		backend.Protect = func(ctx context.Context, text string) (string, error) {
			protected, err := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
			return protected.String(), err
		}
		backend.Instructions = options.Instructions
		backend.InstructionDiagnostics = options.InstructionDiagnostics
		var diagnosis ChatDiagnoser
		if options.Diagnoser != nil {
			diagnosis = options.Diagnoser
		} else if d.doctor.service != nil {
			diagnosis = d.doctor.service
		}
		research := ownerActionResearch{daemon: d, sanitizer: sanitizer, diagnoser: diagnosis, replyLimit: backend.ReplyLimit()}
		backend.EnableResearch(research, research)
		allowed := map[string]bool{"answer": d.forge != nil, "diagnose": diagnosis != nil}
		answers := ask.Service{Repositories: d.db, Collector: ask.Collector{Factory: ask.StoreReader{Reader: d.db}}, Redactor: sanitizer}
		if options.Answers != nil {
			answers = *options.Answers
		}
		if answers.Redactor == nil {
			answers.Redactor = sanitizer
		}
		writer, _ := d.forge.(ChatIssueForge)
		var creator *ChatIssueCreator
		if writer != nil && d.filter.Identities().BotUserID > 0 {
			creator = &ChatIssueCreator{DB: d.db, Forge: writer, BotUserID: d.filter.Identities().BotUserID, Redactor: sanitizer, Now: clock}
			allowed["confirm"] = true
		}
		var drafter *feature.Drafter
		if answers.Model != nil {
			drafter = &feature.Drafter{Instructions: options.Instructions, InstructionDiagnostics: options.InstructionDiagnostics, Repositories: answers.Repositories, Model: answers.Model, Redactor: answers.Redactor, Store: d.db, Protect: backend.Protect, Now: clock}
			allowed["draft"] = true
		}
		if creator != nil || drafter != nil {
			backend.EnableFeatures(drafter, creator)
		}
		var standards *ConversationStandards
		if options.Standards != nil {
			operations := *options.Standards
			operations.OwnerID = options.Settings.OwnerID
			operations.Authorized = func(ctx context.Context, request messaging.StandardsRequest) error {
				if d.chat == nil || d.chat.adapter == nil {
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
				return d.chat.adapter.CheckIntakeCapability(ctx, request.ChannelID)
			}
			standards = &operations
			backend.EnableStandards(standards)
		}
		domain := independentOwnerActionDomain{OwnerActionDomain: messaging.OwnerActionDomain{Service: backend}, allowed: allowed, standards: standards}
		runtime.actions = owneraction.New(d.db, domain, clock)
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
	messaging.OwnerActionDomain
	allowed   map[string]bool
	standards *ConversationStandards
}

func (d independentOwnerActionDomain) enabled(inv owneraction.Invocation) bool {
	if d.allowed[inv.Command] {
		return true
	}
	if d.standards == nil || standardsOperationName(inv.Command, inv.Arguments["action"]) == "" {
		return false
	}
	for _, entry := range d.OwnerActionDomain.ActionCatalog() {
		if entry.Command == inv.Command && entry.Subaction == inv.Arguments["action"] {
			return d.standards.capability(standardsOperationName(entry.Command, entry.Subaction)) == nil
		}
	}
	return false
}
func standardsOperationName(command, subaction string) string {
	switch command {
	case "standards-author":
		return "author." + subaction
	case "standards-proposal":
		return "proposal." + subaction
	case "standards-exception":
		return "exception." + subaction
	case "standards-analysis":
		return "analysis." + subaction
	}
	return ""
}
func (d independentOwnerActionDomain) Resolve(ctx context.Context, p owneraction.Principal, inv owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	if !d.enabled(inv) {
		return owneraction.Facts{}, owneraction.ErrUnavailable
	}
	return d.OwnerActionDomain.Resolve(ctx, p, inv, at)
}
func (d independentOwnerActionDomain) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	if !d.enabled(f.Invocation) {
		return owneraction.ErrUnavailable
	}
	return d.OwnerActionDomain.Recheck(ctx, p, f, at)
}

func (d independentOwnerActionDomain) ActionCatalog() []owneraction.ActionDefinition {
	out := []owneraction.ActionDefinition{}
	for _, entry := range d.OwnerActionDomain.ActionCatalog() {
		if d.enabled(owneraction.Invocation{Command: entry.Command, Arguments: map[string]string{"action": entry.Subaction}}) {
			out = append(out, entry)
		}
	}
	return out
}

type ownerActionResearch struct {
	daemon     *Daemon
	sanitizer  *redact.Redactor
	diagnoser  ChatDiagnoser
	replyLimit int
}

func (a ownerActionResearch) Current(ctx context.Context, ref string) (messaging.Question, error) {
	return (chatResearch{a.daemon}).Current(ctx, ref)
}
func (a ownerActionResearch) Answer(ctx context.Context, ref, revision, text string, at time.Time) (messaging.Question, error) {
	return (chatResearch{a.daemon}).answerProtected(ctx, ref, revision, text, at, a.sanitizer)
}
func (ownerActionResearch) Queue(context.Context, messaging.DiagnosisRequest) error {
	return errors.New("owner action diagnosis requires its execution journal")
}
func (a ownerActionResearch) OwnerActionDiagnosis(ctx context.Context, target, note, key string) (string, error) {
	return runOwnerActionDiagnosis(ctx, a.daemon, a.diagnoser, a.sanitizer, a.replyLimit, target, note, key)
}
func (a ownerActionResearch) RecoverOwnerActionDiagnosis(ctx context.Context, target, key string) (owneraction.Result, bool, error) {
	return recoverOwnerActionDiagnosis(ctx, a.daemon.db, target, key, func(ctx context.Context, c doctor.Conversation) (string, error) {
		return renderOwnerActionDiagnosis(ctx, c, a.sanitizer, a.replyLimit)
	})
}
