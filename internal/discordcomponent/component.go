// Package discordcomponent owns the in-process Discord transport and worker lifecycle.
package discordcomponent

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
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// Credentials are immutable transport settings with an optional retry resolver.
type Credentials struct {
	Settings     config.DiscordChat
	Token        secrets.Token
	Resolve      func(context.Context) (secrets.Token, error)
	TokenSetting string
	APIURL       string
	Client       *http.Client
}

// Prepare resolves optional credentials without preventing core startup.
func Prepare(ctx context.Context, cfg *config.Config, resolver secrets.Resolver) (*Credentials, error) {
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
	o := &Credentials{Settings: c, TokenSetting: setting, Resolve: func(ctx context.Context) (secrets.Token, error) { return resolver.Resolve(ctx, ref) }}
	if token, err := o.Resolve(ctx); err == nil {
		o.Token = token
	}
	return o, nil
}

// Hooks are bounded core maintenance and delivery capabilities installed by trusted wiring.
// They carry no workflow records, transaction access or coordinator lifecycle.
type Hooks struct {
	// Start binds core background effects to this lifetime and returns their join.
	Start     func(context.Context) func()
	Recover   func(context.Context, time.Time)
	Reconcile func(context.Context, time.Time)
	Dispatch  func(context.Context, time.Time)
	Reaction  func(context.Context, discord.OwnerReaction)
	Collect   func(context.Context, time.Time) error
}

// Options supplies only conversation storage, bounded factory views and composition.
type Options struct {
	Credentials   Credentials
	Storage       messagingstorage.Port
	Factory       *factory.Messaging
	Health        messaging.StatusReader
	Logger        *slog.Logger
	Redactor      *redact.Redactor
	Conversations *messaging.ConversationOptions
	AlertControls bool
	Configure     func(*messaging.Service, *discord.Adapter, *redact.Redactor) Hooks
}

// Component owns one transport, one intake and the independent delivery workers.
type Component struct {
	opts          Options
	log           *slog.Logger
	key           ed25519.PublicKey
	heartbeat     chan struct{}
	handler       atomic.Pointer[http.Handler]
	status        atomic.Pointer[string]
	service       *messaging.Service
	adapter       *discord.Adapter
	conversations *messaging.ConversationOptions
	intake        *messaging.Intake
	reads         *discordReadLog
	hooks         Hooks
	serving       atomic.Bool
	admission     sync.Mutex
	active        sync.WaitGroup
	closed        bool
	lifetime      context.Context
}

const chatAvailable = "available"

// New validates enabled transport settings and prepares immediate or deferred activation.
func New(o Options) (*Component, error) {
	key, err := hex.DecodeString(o.Credentials.Settings.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("chat public key is invalid")
	}
	o.Credentials.Settings.ChannelIDs = append([]string(nil), o.Credentials.Settings.ChannelIDs...)
	o.Credentials.Settings.Conversations = append([]config.ChannelConversation(nil), o.Credentials.Settings.Conversations...)
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Conversations != nil {
		copied := *o.Conversations
		copied.Bindings = append([]messaging.ConversationBinding(nil), copied.Bindings...)
		copied.Providers = maps.Clone(copied.Providers)
		copied.Models = maps.Clone(copied.Models)
		o.Conversations = &copied
	}
	c := &Component{opts: o, log: o.Logger, key: key, heartbeat: make(chan struct{}, 1)}
	if o.Credentials.Token.Empty() && o.Credentials.Resolve != nil {
		c.setStatus("unavailable: bot token (" + o.Credentials.TokenSetting + ") could not be resolved; retrying on later heartbeats")
		c.log.Error("chat.discord bot token could not be resolved; chat unavailable, retrying on a later heartbeat", slog.String("setting", o.Credentials.TokenSetting))
	} else {
		c.build(o.Credentials.Token)
	}
	return c, nil
}
func (c *Component) setStatus(s string) { c.status.Store(&s) }

// Status reports current credential and gateway availability.
func (c *Component) Status() string {
	if s := c.status.Load(); s != nil {
		return *s
	}
	return ""
}

// Wake retries pending credential resolution or command registration on a heartbeat.
func (c *Component) Wake() {
	select {
	case c.heartbeat <- struct{}{}:
	default:
	}
}

// Handler remains unavailable until credential recovery and after admission stops.
func (c *Component) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.admission.Lock()
		handler := c.handler.Load()
		if c.closed || handler == nil {
			c.admission.Unlock()
			http.Error(w, "chat unavailable", http.StatusServiceUnavailable)
			return
		}
		c.active.Add(1)
		lifetime := c.lifetime
		c.admission.Unlock()
		defer c.active.Done()
		if lifetime != nil {
			request, cancel := context.WithCancel(r.Context())
			stop := context.AfterFunc(lifetime, cancel)
			defer stop()
			defer cancel()
			r = r.WithContext(request)
		}
		(*handler).ServeHTTP(w, r)
	})
}
func (c *Component) stopAdmission() {
	c.admission.Lock()
	c.closed = true
	c.handler.Store(nil)
	c.admission.Unlock()
}
func (c *Component) build(token secrets.Token) {
	o := c.opts
	settings := o.Credentials.Settings
	a := discord.New(discord.Options{Logger: c.log, ApplicationID: settings.ApplicationID, PublicKey: c.key, Token: token, APIURL: o.Credentials.APIURL, Client: o.Credentials.Client})
	s := messaging.NewWithServices(o.Storage, o.Factory, "discord", settings.OwnerID, settings.ChannelIDs, a, o.Health)
	s.Logger = c.log
	sanitizer := o.Redactor
	if sanitizer == nil {
		sanitizer = redact.New(redact.Options{Logger: c.log})
	}
	sanitizer.AddToken(token)
	s.Protect = func(ctx context.Context, text string) (string, error) {
		body, err := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
		return body.String(), err
	}
	c.service = s
	c.adapter = a
	c.reads = &discordReadLog{log: c.log}
	if o.Conversations != nil {
		options := *o.Conversations
		options.CheckAccess = func(ctx context.Context, channel string) error {
			err := a.CheckIntakeCapability(ctx, channel)
			c.reads.report(ctx, "conversation access", err)
			var failure *discord.IntakeFailure
			if errors.As(err, &failure) {
				return &messaging.ConversationAccessError{Permanent: failure.Capability, RetryAfter: failure.RetryAfter}
			}
			return err
		}
		c.conversations = &options
	}
	if o.Configure != nil {
		c.hooks = o.Configure(s, a, sanitizer)
	}
	var channels []string
	if c.conversations != nil {
		for _, channel := range settings.ChannelIDs {
			for _, binding := range c.conversations.Bindings {
				if binding.Channel == channel {
					channels = append(channels, channel)
					break
				}
			}
		}
	}
	for _, channel := range settings.ChannelIDs {
		bound := false
		for _, ordinary := range channels {
			if ordinary == channel {
				bound = true
				break
			}
		}
		if !bound {
			c.log.Info("Discord intake", "transport", "discord", "stage", "setup", "channel_id", channel, "reason", "no_conversation_binding", "alert_controls", o.AlertControls)
		}
	}
	if len(channels) > 0 || o.AlertControls {
		c.intake = &messaging.Intake{Logger: c.log, DB: o.Storage, Reader: a, Owner: settings.OwnerID, Channels: channels, Transport: "discord", Protect: s.Protect}
		if o.AlertControls {
			c.intake.OrdinaryChannels = channels
			c.intake.Channels = append([]string(nil), settings.ChannelIDs...)
			c.intake.Controls = s.HandleAlertControl
		}
	}
	handler := a.Handler(s)
	c.handler.Store(&handler)
	c.setStatus(chatAvailable)
}
func (c *Component) awaitToken(ctx context.Context) bool {
	o := c.opts.Credentials
	for {
		select {
		case <-ctx.Done():
			return false
		case <-c.heartbeat:
		}
		token, err := o.Resolve(ctx)
		if ctx.Err() != nil {
			return false
		}
		if err != nil || token.Empty() {
			c.log.WarnContext(ctx, "chat.discord bot token could not be resolved; chat unavailable, retrying on a later heartbeat", slog.String("setting", o.TokenSetting))
			continue
		}
		c.build(token)
		c.log.InfoContext(ctx, "chat.discord bot token resolved; chat started", slog.String("setting", o.TokenSetting))
		return true
	}
}

// Serve stops admission, cancels all owned workers and joins them before returning.
// A component has one serving lifetime; restart constructs a fresh component over saved state.
func (c *Component) Serve(ctx context.Context) {
	if !c.serving.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	c.admission.Lock()
	c.lifetime = ctx
	c.admission.Unlock()
	var wg sync.WaitGroup
	var join func()
	defer func() {
		c.stopAdmission()
		cancel()
		c.active.Wait()
		wg.Wait()
		if join != nil {
			join()
		}
	}()
	if c.service == nil && !c.awaitToken(ctx) {
		return
	}
	// Admission closes as soon as cancellation begins, before worker drain.
	wg.Go(func() { <-ctx.Done(); c.stopAdmission() })
	if c.hooks.Start != nil {
		join = c.hooks.Start(ctx)
	}
	c.startConversations(ctx, &wg, time.Now())
	c.startNaturalIntake(ctx, &wg)
	if c.hooks.Recover != nil {
		c.hooks.Recover(ctx, time.Now())
	}
	if c.hooks.Dispatch != nil {
		c.hooks.Dispatch(ctx, time.Now())
	}
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	registered := false
	register := func() {
		if registered {
			return
		}
		if err := c.adapter.Register(ctx, c.service.SlashCommands()...); err != nil {
			c.log.WarnContext(ctx, "chat commands could not be registered; retrying on a later heartbeat", slog.Any("error", err))
		} else {
			registered = true
		}
	}
	register()
	for {
		if ctx.Err() != nil {
			return
		}
		if c.hooks.Reconcile != nil {
			c.hooks.Reconcile(ctx, time.Now())
		}
		if c.hooks.Collect != nil {
			if err := c.hooks.Collect(ctx, time.Now()); err != nil {
				c.log.WarnContext(ctx, "chat notice collection failed; retrying on the next pass")
			}
		}
		for _, step := range []func(context.Context, time.Time) (bool, error){c.service.NoticeStep, c.service.Step} {
			for range 32 {
				more, err := step(ctx, time.Now())
				if err != nil {
					c.log.WarnContext(ctx, "chat reply processing failed; durable work will be retried")
					break
				}
				if !more {
					break
				}
			}
		}
		if err := c.service.Purge(ctx, time.Now()); err != nil {
			c.log.WarnContext(ctx, "chat reply purge failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-c.service.Wake():
			if c.hooks.Dispatch != nil {
				c.hooks.Dispatch(ctx, time.Now())
			}
		case <-c.heartbeat:
			if c.hooks.Dispatch != nil {
				c.hooks.Dispatch(ctx, time.Now())
			}
			register()
		case <-timer.C:
		}
	}
}
