package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
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
	service       *messaging.Service
	adapter       *discord.Adapter
	heartbeat     chan struct{}
	collector     *messaging.NoticeCollector
	diagnoser     ChatDiagnoser
	redactor      *redact.Redactor
	mu            sync.Mutex
	notes         map[string]string
	intake        *messaging.Intake
	conversations *messaging.ConversationOptions
	analyses      *ConversationAnalyses
	reads         *discordReadLog
}

func prepareChat(ctx context.Context, cfg *config.Config, resolver secrets.Resolver) (*ChatOptions, error) {
	if cfg.Chat == nil || cfg.Chat.Discord == nil || !cfg.Chat.Discord.Enabled {
		return nil, nil
	}
	c := *cfg.Chat.Discord
	c.ChannelIDs = append([]string(nil), c.ChannelIDs...)
	c.Conversations = append([]config.ChannelConversation(nil), c.Conversations...)
	ref, ok := secrets.ReferenceFrom(c.TokenKeychain.Service, c.TokenKeychain.Account, c.TokenEnv)
	if !ok {
		return nil, fmt.Errorf("chat.discord requires a bot token reference")
	}
	setting := "chat.discord.token_keychain"
	if c.TokenKeychain.Service == "" && c.TokenKeychain.Account == "" {
		setting = "chat.discord.token_env"
	}
	o := &ChatOptions{Settings: c, TokenSetting: setting, Resolve: func(ctx context.Context) (secrets.Token, error) {
		return resolver.Resolve(ctx, ref)
	}}
	// A token that cannot be resolved leaves only chat unavailable; the daemon
	// starts and the chat goroutine retries on later heartbeats.
	if token, err := resolver.Resolve(ctx, ref); err == nil {
		o.Token = token
	}
	return o, nil
}

// chatPending is chat that is configured but waits for its bot token. It is
// written by New and read only by the chat goroutine.
type chatPending struct {
	opts *ChatOptions
	key  ed25519.PublicKey
}

const chatAvailable = "available"

func chatTokenUnavailable(o *ChatOptions) string {
	return "unavailable: bot token (" + o.TokenSetting + ") could not be resolved; retrying on later heartbeats"
}
func (d *Daemon) setChatStatus(status string) { d.chatStatus.Store(&status) }

func (d *Daemon) initChat(o *ChatOptions) error {
	if o == nil {
		return nil
	}
	if d.webhook == nil {
		return fmt.Errorf("chat requires the webhook listener")
	}
	key, err := hex.DecodeString(o.Settings.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("chat public key is invalid")
	}
	d.chatBeat = make(chan struct{}, 1)
	if o.Token.Empty() && o.Resolve != nil {
		d.chatWait = &chatPending{opts: o, key: key}
		d.setChatStatus(chatTokenUnavailable(o))
		d.log.Error("chat.discord bot token could not be resolved; chat unavailable, retrying on a later heartbeat", slog.String("setting", o.TokenSetting))
		return nil
	}
	d.buildChat(o, key, o.Token)
	return nil
}

// buildChat constructs the chat runtime. It runs in New when the token is
// known at startup, and otherwise once in the chat goroutine.
func (d *Daemon) buildChat(o *ChatOptions, key ed25519.PublicKey, token secrets.Token) {
	a := discord.New(discord.Options{ApplicationID: o.Settings.ApplicationID, PublicKey: key, Token: token, APIURL: o.APIURL, Client: o.Client})
	s := messaging.New(d.db, "discord", o.Settings.OwnerID, o.Settings.ChannelIDs, a, messaging.FactoryStatus{DB: d.db})
	s.Logger = d.log
	s.EnableOwnerInbox(ownerInboxReader{daemon: d})
	s.Instructions = o.Instructions
	s.InstructionDiagnostics = o.InstructionDiagnostics
	sanitizer := redact.New(redact.Options{Logger: d.log, Token: token})
	s.Protect = func(ctx context.Context, text string) (string, error) {
		body, err := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
		return body.String(), err
	}
	answers := ask.Service{Repositories: d.db, Collector: ask.Collector{Factory: ask.StoreReader{Reader: d.db}}, Redactor: sanitizer}
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
	reads := &discordReadLog{log: d.log}
	d.chat = &chatRuntime{service: s, adapter: a, heartbeat: d.chatBeat, diagnoser: o.Diagnoser, redactor: sanitizer, notes: make(map[string]string), reads: reads}
	if o.Conversations != nil {
		options := *o.Conversations
		options.Bindings = append([]messaging.ConversationBinding(nil), options.Bindings...)
		options.Providers = maps.Clone(options.Providers)
		options.Models = maps.Clone(options.Models)
		options.CheckAccess = func(ctx context.Context, channel string) error {
			err := a.CheckIntakeCapability(ctx, channel)
			reads.report(ctx, "conversation access", err)
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
		d.chat.conversations = &options
		// Ordinary messages feed only conversations, so intake reads only
		// allowed channels that a conversation is bound to.
		var channels []string
		for _, channel := range o.Settings.ChannelIDs {
			for _, binding := range options.Bindings {
				if binding.Channel == channel {
					channels = append(channels, channel)
					break
				}
			}
		}
		if len(channels) > 0 {
			d.chat.intake = &messaging.Intake{DB: d.db, Reader: a, Owner: o.Settings.OwnerID, Channels: channels, Transport: "discord", Protect: s.Protect}
		}
	}
	s.EnableResearch(chatResearch{d}, chatResearch{d})
	clock := o.Now
	if clock == nil {
		clock = time.Now
	}
	d.configureOwnerActions(s, o.Settings.OwnerID, o.Settings.ChannelIDs, a, clock)
	if o.Settings.NoticeChannelID != "" {
		if o.Conversations != nil {
			for _, binding := range o.Conversations.Bindings {
				if binding.Channel == o.Settings.NoticeChannelID {
					d.log.Warn("Discord notices and conversation share a channel; separate notices may interleave with replies", "channel", binding.Channel)
					break
				}
			}
		}
		d.chat.collector = &messaging.NoticeCollector{DB: d.db, Transport: "discord", Channel: o.Settings.NoticeChannelID, Logger: d.log, Protect: s.Protect, Issues: noticeIssueReader{d}}
	}
	handler := a.Handler(s)
	d.chatHandler.Store(&handler)
	d.setChatStatus(chatAvailable)
}
func (d *Daemon) chatWake() {
	if d.chatBeat == nil {
		return
	}
	select {
	case d.chatBeat <- struct{}{}:
	default:
	}
}

// awaitChatToken retries token resolution on each heartbeat, the same way
// tryRegister retries command registration, and builds the chat runtime once
// the token resolves. It reports false when ctx ends first.
func (d *Daemon) awaitChatToken(ctx context.Context) bool {
	o := d.chatWait.opts
	for {
		select {
		case <-ctx.Done():
			return false
		case <-d.chatBeat:
		}
		token, err := o.Resolve(ctx)
		if ctx.Err() != nil {
			return false
		}
		if err != nil || token.Empty() {
			d.log.WarnContext(ctx, "chat.discord bot token could not be resolved; chat unavailable, retrying on a later heartbeat", slog.String("setting", o.TokenSetting))
			continue
		}
		d.buildChat(o, d.chatWait.key, token)
		d.log.InfoContext(ctx, "chat.discord bot token resolved; chat started", slog.String("setting", o.TokenSetting))
		return true
	}
}

// chatServe owns one goroutine, bounded by the daemon's serving context. All
// network work runs here rather than on discovery or heartbeat goroutines.
func (d *Daemon) chatServe(ctx context.Context, wg *sync.WaitGroup) {
	if d.chatBeat == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if d.chat == nil && !d.awaitChatToken(ctx) {
			return
		}
		d.startConversations(ctx, wg, time.Now())
		d.startOwnerActions(ctx, wg)
		if d.chat.analyses != nil {
			service := d.chat.service
			d.chat.analyses.serve(ctx, wg, func(ctx context.Context, channel, key, operation, text string) error {
				return service.Announce(ctx, channel, key, operation, text, time.Now())
			})
		}
		d.startNaturalIntake(ctx, wg, d.chat.intake)
		if err := d.db.RecoverChatAnswers(ctx, "discord", time.Now()); err != nil {
			d.log.WarnContext(ctx, "chat answer recovery failed; waiting rows retain their leases")
		}
		if err := d.db.RecoverChatDiagnoses(ctx, "discord", time.Now()); err != nil {
			d.log.WarnContext(ctx, "chat diagnosis recovery failed")
		}
		d.dispatchChatDiagnosis(ctx, time.Now())
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		registered := false
		tryRegister := func() {
			if registered {
				return
			}
			if err := d.chat.adapter.Register(ctx, d.chat.service.SlashCommands()...); err != nil {
				d.log.WarnContext(ctx, "chat commands could not be registered; retrying on a later heartbeat", slog.Any("error", err))
			} else {
				registered = true
			}
		}
		tryRegister()
		for {
			if ctx.Err() != nil {
				return
			}
			if err := d.chatResearchNotes(ctx); err != nil {
				d.log.WarnContext(ctx, "chat answer note remains pending")
			}
			if err := d.chatDiagnosisResults(ctx, time.Now()); err != nil {
				d.log.WarnContext(ctx, "chat diagnosis result remains unposted; retrying on the next pass")
			}
			if d.chat.collector != nil {
				if err := d.chat.collector.Collect(ctx, time.Now()); err != nil {
					d.log.WarnContext(ctx, "chat notice collection failed; retrying on the next pass")
				}
			}
			// Notices also carry factory messages for a conversation, such as a
			// finished log analysis, so they are delivered without a collector.
			for range 32 {
				more, err := d.chat.service.NoticeStep(ctx, time.Now())
				if err != nil {
					d.log.WarnContext(ctx, "chat notice processing failed; durable work will be retried")
					break
				}
				if !more {
					break
				}
			}
			for range 32 {
				more, err := d.chat.service.Step(ctx, time.Now())
				if err != nil {
					d.log.WarnContext(ctx, "chat reply processing failed; durable work will be retried")
					break
				}
				if !more {
					break
				}
			}
			if err := d.chat.service.Purge(ctx, time.Now()); err != nil {
				d.log.WarnContext(ctx, "chat reply purge failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-d.chat.service.Wake():
				d.dispatchChatDiagnosis(ctx, time.Now())
			case <-d.chat.heartbeat:
				d.dispatchChatDiagnosis(ctx, time.Now())
				tryRegister()
			case <-timer.C:
			}
		}
	}()
}

// webhookRouter serves the Discord path once chat is built and answers 503
// while chat waits for its bot token.
func (d *Daemon) webhookRouter(githubHandler http.Handler) http.Handler {
	if d.chatBeat == nil {
		return githubHandler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == discord.Path {
			if h := d.chatHandler.Load(); h != nil {
				(*h).ServeHTTP(w, r)
				return
			}
			http.Error(w, "chat unavailable", http.StatusServiceUnavailable)
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
