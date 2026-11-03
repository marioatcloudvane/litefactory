package daemon

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/discordcomponent"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// ChatOptions supplies immutable resolved chat settings. Only daemon wiring
// imports the adapter; the service and store know no Discord types.
type ChatOptions struct {
	// Now supplies the transport lifecycle clock independently of the discovery tick snapshot.
	Now func() time.Time
	// Instructions supplies the shared read-only chat instruction package.
	Instructions           prompts.Resolver
	InstructionDiagnostics prompts.Sink
	// Answers supplies read-only context and response-only model wiring.
	Diagnoser     ChatDiagnoser
	Answers       *ask.Service
	Conversations *messaging.ConversationOptions
	Standards     *ConversationStandards
	Settings      config.DiscordChat
	Token         secrets.Token
	// Resolve, when set, resolves the bot token again. A Token that is empty at
	// startup leaves chat unavailable, and the chat goroutine calls Resolve on
	// later heartbeats until it succeeds.
	Resolve func(context.Context) (secrets.Token, error)
	// TokenSetting names the token reference setting in logs and status.
	TokenSetting string
	// APIURL and Client allow daemon tests to use a local fake API.
	APIURL string
	Client *http.Client
}
type chatRuntime struct {
	diagnosisWorkers sync.WaitGroup
	actions          *owneraction.Service
	service          *messaging.Service
	adapter          *discord.Adapter
	collector        *messaging.NoticeCollector
	diagnoser        ChatDiagnoser
	redactor         *redact.Redactor
	mu               sync.Mutex
	notes            map[string]string
	analyses         *ConversationAnalyses
}

func prepareChat(ctx context.Context, cfg *config.Config, resolver secrets.Resolver) (*ChatOptions, error) {
	credentials, err := discordcomponent.Prepare(ctx, cfg, resolver)
	if err != nil || credentials == nil {
		return nil, err
	}
	return &ChatOptions{Settings: credentials.Settings, Token: credentials.Token, Resolve: credentials.Resolve, TokenSetting: credentials.TokenSetting}, nil
}

func (d *Daemon) initChat(o *ChatOptions) error {
	if o == nil {
		return nil
	}
	copied := *o
	copied.Settings.ChannelIDs = append([]string(nil), o.Settings.ChannelIDs...)
	copied.Settings.Conversations = append([]config.ChannelConversation(nil), o.Settings.Conversations...)
	if o.Conversations != nil {
		options := *o.Conversations
		options.Bindings = append([]messaging.ConversationBinding(nil), options.Bindings...)
		options.Providers = maps.Clone(options.Providers)
		options.Models = maps.Clone(options.Models)
		copied.Conversations = &options
	}
	o = &copied
	if d.webhook == nil {
		return fmt.Errorf("chat requires the webhook listener")
	}
	d.prepareIndependentOwnerActions(o, nil)
	component, err := discordcomponent.New(discordcomponent.Options{
		Credentials: discordcomponent.Credentials{Settings: o.Settings, Token: o.Token, Resolve: o.Resolve, TokenSetting: o.TokenSetting, APIURL: o.APIURL, Client: o.Client},
		Storage:     messagingstorage.New(d.db), Factory: factory.NewMessaging(d.db), Health: factory.NewHealth(d.db), Logger: d.log,
		Conversations: o.Conversations, AlertControls: d.alerts != nil, Redactor: d.ownerActions.sanitizer,
		Configure: func(s *messaging.Service, a *discord.Adapter, sanitizer *redact.Redactor) discordcomponent.Hooks {
			return d.configureChat(o, s, a, sanitizer)
		},
	})
	if err != nil {
		return err
	}
	d.discord = component
	return nil
}

// configureChat installs bounded core operations; transport and workers belong to the component.
func (d *Daemon) configureChat(o *ChatOptions, s *messaging.Service, a *discord.Adapter, sanitizer *redact.Redactor) discordcomponent.Hooks {
	if d.alerts != nil {
		s.SetAlertControls(d.alerts.Command)
		s.SetProviderObserver(func(c context.Context, provider, conversation, turn, class string) {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(c), 2*time.Second)
			defer cancel()
			if err := d.alerts.Provider(cleanup, alerts.ProviderOutcome{ID: "conversation:" + conversation + ":" + turn, Provider: provider, Account: "local-default", Source: "conversation:" + conversation, Class: providerAlertClass(class), At: d.alertClock()}); err != nil {
				d.log.WarnContext(cleanup, "conversation alert evidence unavailable")
			}
		})
		d.alertMu.Lock()
		d.alertSender = a
		d.alertOwner = o.Settings.OwnerID
		d.alertChannel = d.alerts.Settings.ChannelID
		if d.alertChannel == "" {
			d.alertChannel = o.Settings.NoticeChannelID
		}
		d.alertMu.Unlock()
	}
	s.EnableOwnerInbox(ownerInboxReader{daemon: d})
	s.Instructions = o.Instructions
	s.InstructionDiagnostics = o.InstructionDiagnostics
	answers := ask.Service{Repositories: factory.NewMessaging(d.db), Collector: ask.Collector{Factory: factory.NewTaskFacts(d.db)}, Redactor: sanitizer}
	if o.Answers != nil {
		answers = *o.Answers
	}
	if o.Instructions != nil {
		answers.Instructions = o.Instructions
	}
	if o.InstructionDiagnostics != nil {
		answers.InstructionDiagnostics = o.InstructionDiagnostics
	}
	if answers.Redactor == nil {
		answers.Redactor = sanitizer
	}
	history := messaging.AskHistory{DB: d.db, Transport: "discord"}
	if answers.History == nil {
		answers.History = history
	}
	s.EnableAsk(&answers)

	writer, _ := d.forge.(ChatIssueForge)
	s.EnableFeatures(&feature.Drafter{Instructions: answers.Instructions, InstructionDiagnostics: answers.InstructionDiagnostics, Repositories: answers.Repositories, History: answers.History, Model: answers.Model, Redactor: answers.Redactor, Store: d.db, Protect: s.Protect, Now: time.Now}, &ChatIssueCreator{DB: d.db, Forge: writer, BotUserID: d.filter.Identities().BotUserID, Redactor: answers.Redactor, Now: time.Now})
	d.chat = &chatRuntime{service: s, adapter: a, diagnoser: o.Diagnoser, redactor: sanitizer, notes: make(map[string]string)}
	if o.Conversations != nil {
		options := *o.Conversations
		options.Bindings = append([]messaging.ConversationBinding(nil), options.Bindings...)
		options.Providers = maps.Clone(options.Providers)
		options.Models = maps.Clone(options.Models)
		options.CheckAccess = func(ctx context.Context, channel string) error {
			err := a.CheckIntakeCapability(ctx, channel)
			if err == nil {
				return nil
			}
			var failure *discord.IntakeFailure
			if errors.As(err, &failure) {
				return &messaging.ConversationAccessError{Permanent: failure.Capability, RetryAfter: failure.RetryAfter}
			}
			return err
		}
		if o.Standards != nil {
			operations := *o.Standards
			operations.OwnerID = o.Settings.OwnerID
			operations.Authorized = func(ctx context.Context, request messaging.StandardsRequest) error {
				allowed := false
				for _, channel := range o.Settings.ChannelIDs {
					if channel == request.ChannelID {
						allowed = true
					}
				}
				bound := false
				for _, binding := range options.Bindings {
					if binding.Channel == request.ChannelID {
						bound = true
					}
				}
				if !allowed || !bound {
					return errors.New("standards: conversation no longer authorized")
				}
				return options.CheckAccess(ctx, request.ChannelID)
			}
			s.EnableStandards(&operations)
			d.chat.analyses = operations.Analyses
		}
	}
	s.EnableResearch(chatResearch{d}, chatResearch{d})
	d.configureOwnerActions(s, o.Settings.OwnerID, o.Settings.ChannelIDs, a)
	if o.Settings.NoticeChannelID != "" {
		if o.Conversations != nil {
			for _, binding := range o.Conversations.Bindings {
				if binding.Channel == o.Settings.NoticeChannelID {
					d.log.Warn("Discord notices and conversation share a channel; separate notices may interleave with replies", "channel", binding.Channel)
					break
				}
			}
		}
		d.chat.collector = &messaging.NoticeCollector{Factory: factory.NewMessaging(d.db), Transport: "discord", Channel: o.Settings.NoticeChannelID, Logger: d.log, Protect: s.Protect, Issues: noticeIssueReader{d}}
	}
	hooks := discordcomponent.Hooks{
		Start: func(ctx context.Context) func() {
			var owned sync.WaitGroup
			owned.Add(1)
			d.chat.diagnosisWorkers.Add(1)
			if d.chat.analyses != nil {
				d.chat.analyses.serve(ctx, &owned, func(ctx context.Context, channel, key, operation, text string) error {
					return s.Announce(ctx, channel, key, operation, text, time.Now())
				})
			}
			return func() { owned.Done(); owned.Wait(); d.chat.diagnosisWorkers.Done(); d.chat.diagnosisWorkers.Wait() }
		},
		Recover: func(ctx context.Context, at time.Time) {
			if err := d.db.RecoverChatAnswers(ctx, "discord", at); err != nil {
				d.log.WarnContext(ctx, "chat answer recovery failed; waiting rows retain their leases")
			}
			if err := d.db.RecoverChatDiagnoses(ctx, "discord", at); err != nil {
				d.log.WarnContext(ctx, "chat diagnosis recovery failed")
			}
		},
		Reconcile: func(ctx context.Context, at time.Time) {
			if err := d.chatDiagnosisResults(ctx, at); err != nil {
				d.log.WarnContext(ctx, "chat diagnosis result remains unposted; retrying on the next pass")
			}
		},
		Dispatch: d.dispatchChatDiagnosis, Reaction: d.ownerReaction,
	}
	if d.chat.collector != nil {
		hooks.Collect = d.chat.collector.Collect
	}
	return hooks
}
func (d *Daemon) chatWake() {
	if d.discord != nil {
		d.discord.Wake()
	}
}
func (d *Daemon) chatServe(ctx context.Context, wg *sync.WaitGroup) {
	if d.discord == nil {
		return
	}
	wg.Go(func() { d.discord.Serve(ctx) })
}
func (d *Daemon) webhookRouter(githubHandler http.Handler) http.Handler {
	if d.discord == nil {
		return githubHandler
	}
	handler := d.discord.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == discord.Path {
			handler.ServeHTTP(w, r)
			return
		}
		githubHandler.ServeHTTP(w, r)
	})
}

// noticeIssueReader preserves the configured ticket connection and public metadata.
type noticeIssueReader struct{ daemon *Daemon }

func (r noticeIssueReader) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	return r.daemon.readIssue(ctx, repo, number)
}
