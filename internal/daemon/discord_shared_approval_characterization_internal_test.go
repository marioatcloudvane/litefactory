package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
)

// Both deployments must use the same confirmation fences and persist the same
// domain result. Remote enablement cannot grant Discord any extra authority.
func TestDiscordSharedApprovalWithRemoteEnabledOrDisabled(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "remote-disabled"
		if remote {
			name = "remote-enabled"
		}
		t.Run(name, func(t *testing.T) {
			setup := remoteSetupFixture(t)
			db, ctx := setup.DB, t.Context()
			at := time.Now().UTC().Truncate(time.Millisecond)
			book := &standards.Service{Store: db, OwnerID: 42}
			seed, err := book.Create(ctx, "shared-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = book.Approve(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
				t.Fatal(err)
			}
			if _, err = book.Publish(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
				t.Fatal(err)
			}
			author := &plainJourneyAuthor{}
			operations := &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Instructions: factorylibtest.Library(t), Store: db, Book: book, Runner: author}, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/applications/@me" {
					fmt.Fprint(w, `{"flags":262144}`)
				} else {
					fmt.Fprint(w, `[]`)
				}
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{Token: secrets.NewToken("fixture"), APIURL: api.URL, Client: api.Client()})
			d := &Daemon{db: db, filter: independentActionFilter{}, log: setup.Logger}
			if remote {
				server, e := connector.New(ctx, connector.Options{Guidance: factorylibtest.Connector(), Config: setup.Config.RemoteConnector, Storage: connectorstorage.New(db), Access: connectorstorage.New(db), Logger: setup.Logger, LocalControl: setup.ConnectorControl, GitHubSecret: "fixture", CookieKey: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return at }})
				if e != nil {
					t.Fatal(e)
				}
				d.remoteConnector = &remoteConnectorRuntime{server: server}
			}
			d.prepareIndependentOwnerActions(&ChatOptions{Settings: config.DiscordChat{OwnerID: "7", ChannelIDs: []string{"3"}}, Conversations: &messaging.ConversationOptions{Instructions: factorylibtest.Library(t), Bindings: []messaging.ConversationBinding{{Channel: "3"}}}, Now: func() time.Time { return at }, Standards: operations}, redact.New(redact.Options{Environ: []string{}}))
			chat := messaging.NewWithServices(messagingstorage.New(db), factory.NewMessaging(db), "discord", "7", []string{"3"}, nil, factory.NewHealth(db))
			chat.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
			chat.EnableStandards(operations)
			d.chat = &chatRuntime{service: chat, adapter: adapter}
			d.configureOwnerActions(chat, "7", []string{"3"}, adapter)
			actions := d.chat.actions
			if remote && actions != d.remoteConnector.actions.actions {
				t.Fatal("Discord did not join the remote coordinator")
			}
			owner, err := db.ConnectorOwner(ctx, 42)
			if err != nil {
				t.Fatal(err)
			}
			principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "7", ChannelID: "3"}
			proposal, err := actions.Propose(ctx, principal, owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "start", "id": "shared-author", "intent": "bounded checks", "category_ids": "quality"}}, "shared-start")
			if err != nil {
				t.Fatal(err)
			}
			if err = actions.Available(ctx, proposal.ID, "501"); err != nil {
				t.Fatal(err)
			}
			early, err := actions.Confirm(ctx, principal, proposal.ID)
			if err != nil || early.State != "awaiting_confirmation" || early.Reason != "too_early" {
				t.Fatalf("early confirmation: %+v %v", early, err)
			}
			if author.calls != 0 {
				t.Fatal("unconfirmed action executed")
			}
			available, err := actions.Inspect(ctx, principal, proposal.ID)
			if err != nil {
				t.Fatal(err)
			}
			at = available.ConfirmNotBefore
			admitted, err := actions.Confirm(ctx, principal, proposal.ID)
			if err != nil || admitted.State != "admitted" {
				t.Fatalf("confirmation: %+v %v", admitted, err)
			}
			completed, err := actions.Step(ctx, proposal.ID)
			if err != nil || completed.State != "succeeded" {
				t.Fatalf("execution: %+v %v", completed, err)
			}
			replay, err := actions.Step(ctx, proposal.ID)
			if err != nil || replay.State != "succeeded" || author.calls != 1 {
				t.Fatalf("saved result repeated effect: %+v calls=%d %v", replay, author.calls, err)
			}
			saved, err := db.ReadOwnerAction(ctx, proposal.ID)
			if err != nil || saved.State != "succeeded" || saved.ExecutionID == "" {
				t.Fatalf("outcome not durable: %+v %v", saved, err)
			}
		})
	}
}
