package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
)

func TestDiscordLateTokenKeepsCoordinatorAndProtectsCoreRequests(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote-%t", remote), func(t *testing.T) {
			setup := remoteSetupFixture(t)
			d := &Daemon{db: setup.DB, filter: independentActionFilter{}, log: setup.Logger, webhook: &webhookRuntime{}}
			if remote {
				server, err := connector.New(t.Context(), connector.Options{Guidance: factorylibtest.Connector(), Config: setup.Config.RemoteConnector, Storage: connectorstorage.New(setup.DB), Access: connectorstorage.New(setup.DB), Logger: setup.Logger, LocalControl: setup.ConnectorControl, GitHubSecret: "fixture", CookieKey: bytes.Repeat([]byte{7}, 32)})
				if err != nil {
					t.Fatal(err)
				}
				d.remoteConnector = &remoteConnectorRuntime{server: server}
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `[]`) }))
			defer api.Close()
			const token = "deferred-credential-fixture-secret"
			var restored atomic.Bool
			opts := &ChatOptions{Settings: config.DiscordChat{ApplicationID: "1", PublicKey: strings.Repeat("ab", 32), OwnerID: "7", ChannelIDs: []string{"3"}}, TokenSetting: "chat.discord.token_env", APIURL: api.URL, Client: api.Client(), Resolve: func(context.Context) (secrets.Token, error) {
				if restored.Load() {
					return secrets.NewToken(token), nil
				}
				return secrets.Token{}, errors.New("unavailable")
			}, Standards: &ConversationStandards{DB: setup.DB, Book: &standards.Service{Store: setup.DB, OwnerID: 42}, Authoring: &standards.Authoring{Instructions: factorylibtest.Library(t), Store: setup.DB, Book: &standards.Service{Store: setup.DB, OwnerID: 42}, Runner: &plainJourneyAuthor{}}}}
			if err := d.initChat(opts); err != nil {
				t.Fatal(err)
			}
			runtime := d.ownerActions
			actions := runtime.actions
			if !strings.Contains(d.discord.Status(), "unavailable") {
				t.Fatal("missing token did not leave Discord unavailable")
			}
			run, cancel := context.WithCancel(t.Context())
			var joined sync.WaitGroup
			d.chatServe(run, &joined)
			defer func() { cancel(); joined.Wait() }()
			restored.Store(true)
			d.chatWake()
			componentJourneyAwait(t, "late token availability", func() bool { return d.discord.Status() == "available" })
			if d.ownerActions != runtime || d.ownerActions.actions != actions {
				t.Fatal("late token replaced the core coordinator")
			}
			if remote && d.remoteConnector.actions != runtime {
				t.Fatal("late token split remote and Discord coordinators")
			}
			current, err := setup.DB.ConnectorOwner(t.Context(), 42)
			if err != nil {
				t.Fatal(err)
			}
			principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: current.Generation, Transport: "discord", DiscordOwnerID: "7", ChannelID: "3"}
			_, err = actions.Propose(t.Context(), principal, owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "start", "intent": "Discuss " + token, "category_ids": "quality"}}, "late-token-protection")
			if err == nil || !strings.Contains(err.Error(), "protection changed the exact request") || strings.Contains(err.Error(), token) {
				t.Fatalf("core did not protect recovered credential: %v", err)
			}
			chat := runtime.notices.Load()
			if chat == nil {
				t.Fatal("late token did not attach Discord delivery")
			}
			protected, err := chat.Protect(t.Context(), "Discuss "+token)
			if err != nil || strings.Contains(protected, token) {
				t.Fatalf("conversation did not protect recovered credential: %q %v", protected, err)
			}
			cancel()
			done := make(chan struct{})
			go func() { joined.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("late-token component did not join")
			}
		})
	}
}
