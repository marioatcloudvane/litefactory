package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// ChatOptions supplies immutable resolved chat settings. Only daemon wiring
// imports the adapter; the service and store know no Discord types.
type ChatOptions struct {
	// Answers supplies read-only context and response-only model wiring.
	Answers  *ask.Service
	Settings config.DiscordChat
	Token    secrets.Token
	// APIURL and Client allow daemon tests to use a local fake API.
	APIURL string
	Client *http.Client
}
type chatRuntime struct {
	service   *messaging.Service
	adapter   *discord.Adapter
	heartbeat chan struct{}
}

func prepareChat(ctx context.Context, cfg *config.Config, resolver secrets.Resolver) (*ChatOptions, error) {
	if cfg.Chat == nil || cfg.Chat.Discord == nil || !cfg.Chat.Discord.Enabled {
		return nil, nil
	}
	c := *cfg.Chat.Discord
	c.ChannelIDs = append([]string(nil), c.ChannelIDs...)
	ref, ok := secrets.ReferenceFrom(c.TokenKeychain.Service, c.TokenKeychain.Account, c.TokenEnv)
	if !ok {
		return nil, fmt.Errorf("chat.discord requires a bot token reference")
	}
	token, err := resolver.Resolve(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("chat.discord bot token could not be resolved")
	}
	return &ChatOptions{Settings: c, Token: token}, nil
}
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
	a := discord.New(discord.Options{ApplicationID: o.Settings.ApplicationID, PublicKey: key, Token: o.Token, APIURL: o.APIURL, Client: o.Client})
	s := messaging.New(d.db, "discord", o.Settings.OwnerID, o.Settings.ChannelIDs, a, messaging.FactoryStatus{DB: d.db})
	s.Logger = d.log
	sanitizer := redact.New(redact.Options{Logger: d.log, Token: o.Token})
	s.Protect = func(ctx context.Context, text string) (string, error) {
		body, err := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
		return body.String(), err
	}
	answers := ask.Service{Repositories: d.db, Collector: ask.Collector{Factory: ask.StoreReader{Reader: d.db}}, Redactor: sanitizer}
	if o.Answers != nil {
		answers = *o.Answers
	}
	if answers.Redactor == nil {
		answers.Redactor = sanitizer
	}
	if answers.History == nil {
		answers.History = a
	}
	s.EnableAsk(&answers)
	d.chat = &chatRuntime{service: s, adapter: a, heartbeat: make(chan struct{}, 1)}
	return nil
}
func (d *Daemon) chatWake() {
	if d.chat == nil {
		return
	}
	select {
	case d.chat.heartbeat <- struct{}{}:
	default:
	}
}

// chatServe owns one goroutine, bounded by the daemon's serving context. All
// network work runs here rather than on discovery or heartbeat goroutines.
func (d *Daemon) chatServe(ctx context.Context, wg *sync.WaitGroup) {
	if d.chat == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.db.RecoverChatAnswers(ctx, "discord", time.Now()); err != nil {
			d.log.WarnContext(ctx, "chat answer recovery failed; waiting rows retain their leases")
		}
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		registered := false
		tryRegister := func() {
			if registered {
				return
			}
			if err := d.chat.adapter.Register(ctx, d.chat.service.Declarations()...); err != nil {
				d.log.WarnContext(ctx, "chat commands could not be registered; retrying on a later heartbeat")
			} else {
				registered = true
			}
		}
		tryRegister()
		for {
			if ctx.Err() != nil {
				return
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
			case <-d.chat.heartbeat:
				tryRegister()
			case <-timer.C:
			}
		}
	}()
}
func (d *Daemon) webhookRouter(githubHandler http.Handler) http.Handler {
	if d.chat == nil {
		return githubHandler
	}
	discordHandler := d.chat.adapter.Handler(d.chat.service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == discord.Path {
			discordHandler.ServeHTTP(w, r)
			return
		}
		githubHandler.ServeHTTP(w, r)
	})
}
