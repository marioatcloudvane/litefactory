package localprojects

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

func webhookView(result webhookreg.Result) contracts.ProjectWebhookView {
	return contracts.ProjectWebhookView{Description: result.Describe(), Reason: result.Reason, Failed: result.Outcome == webhookreg.Failed, Skipped: result.Outcome == webhookreg.Skipped, Insecure: result.Insecure, OK: result.OK()}
}

// RepairWebhooks repairs the selected owned subscriptions while preserving polling fallback.
func (a *Administration) RepairWebhooks(ctx context.Context, all bool, shortcut string, present contracts.ProjectWebhookPresentation) (err error) {
	var targets []store.Repo
	if all {
		if targets, err = a.db.ListRepos(ctx); err != nil {
			return err
		}
		if len(targets) == 0 {
			present.Empty()
			return nil
		}
	} else {
		owner, name, ok := strings.Cut(shortcut, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return usage("%q is not an owner/name", shortcut)
		}
		repos, listErr := a.db.ListRepos(ctx)
		if listErr != nil {
			return listErr
		}
		matches := 0
		for _, candidate := range repos {
			if candidate.Forge == string(naming.ForgeGitHub) && strings.EqualFold(candidate.Owner, owner) && strings.EqualFold(candidate.Name, name) {
				matches++
			}
		}
		if matches > 1 {
			return errors.New("repository shortcut is ambiguous; use project and subscription IDs")
		}
		repo, err := a.db.GetRepoBySlug(ctx, string(naming.ForgeGitHub), owner, name)
		if errors.Is(err, store.ErrNotFound) {
			return unavailable(fmt.Errorf("%s is not a registered repository; add it with `litefactory repo add <path>`", shortcut))
		}
		if err != nil {
			return err
		}
		targets = []store.Repo{repo}
	}

	failed := 0
	for _, repo := range targets {
		res := a.ensureWebhook(ctx, repo)
		present.Result(repositoryView(repo), webhookView(res))
		if res.Outcome == webhookreg.Failed || res.Outcome == webhookreg.Skipped {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d repositories could not be fixed", failed, len(targets))
	}
	return nil
}

func (a *Administration) ensureWebhook(ctx context.Context, repo store.Repo) webhookreg.Result {
	if a.cfg.Webhook.PublicURL == "" {
		return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook.public_url is not set in config.yaml; set it to register a webhook (the repository keeps working on polling)"}
	}
	if err := projectconnection.SeedRegistration(ctx, a.db, repo, *a.cfg); err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket connection could not be loaded"}
	}
	version, err := a.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
	if err != nil || !version.Enabled {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket connection is unavailable or disabled"}
	}
	qualified, err := a.projectSubscriptions(ctx, version)
	if err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket connection authentication failed; inspect connection credentials"}
	}
	subscriptions, err := a.db.ListTicketSubscriptions(ctx, repo.ID)
	if err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "subscription intent could not be read"}
	}
	var owned []store.TicketSubscription
	for _, subscription := range subscriptions {
		if subscription.ConnectionVersionID == version.ID {
			owned = append(owned, subscription)
		}
	}
	if len(owned) > 1 {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "subscription selection is ambiguous; use project and subscription IDs"}
	}
	var saved store.TicketSubscription
	outcome := webhookreg.AlreadyCorrect
	if len(owned) == 1 {
		if owned[0].DesiredEnabled && strings.HasPrefix(owned[0].ID, "legacy-") && owned[0].RemoteHookID == "" {
			containerID, verifyErr := verifySubscriptionContainer(ctx, qualified, version)
			if verifyErr != nil || containerID != owned[0].ContainerID {
				return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "legacy ticket source repository identity changed; polling continues"}
			}
		}
		saved, err = qualified.Repair(ctx, owned[0].ID, owned[0].Revision)
	} else {
		containerID, verifyErr := verifySubscriptionContainer(ctx, qualified, version)
		if verifyErr != nil {
			return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket source repository could not be verified; inspect provider access"}
		}
		// Credential edits retain source disable intent. Ordinary compatibility
		// repair must not create another enabled hook for that immutable destination.
		for _, subscription := range subscriptions {
			if !subscription.DesiredEnabled && subscription.RepoID == repo.ID && subscription.Provider == version.Provider && subscription.Origin == version.Origin && subscription.Container == version.Destination && subscription.ContainerID == containerID {
				return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook is explicitly disabled for this ticket source; polling remains active"}
			}
		}
		saved, err = a.db.ImportLegacyTicketSubscription(ctx, version, containerID)
		if errors.Is(err, store.ErrNotFound) {
			secret, keyErr := qualified.Keychain.Read(ctx, webhookreg.KeychainService, version.Destination)
			if keyErr != nil && !errors.Is(keyErr, secrets.ErrNotFound) {
				return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "webhook signing secret is unavailable; check Keychain access"}
			}
			if keyErr == nil && secret != "" {
				saved, err = a.db.PrepareLegacyTicketSubscription(ctx, version, containerID, qualified.PublicURL)
			}
		}
		if errors.Is(err, store.ErrNotFound) {
			saved, err = qualified.Create(ctx, version, containerID)
			outcome = webhookreg.Created
		} else if err == nil {
			saved, err = qualified.Repair(ctx, saved.ID, saved.Revision)
			outcome = webhookreg.Updated
		}
	}
	if err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: describeSubscriptionError(repo, err)}
	}
	if !saved.DesiredEnabled {
		return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook is explicitly disabled; polling remains active"}
	}
	hookID, _ := strconv.ParseInt(saved.RemoteHookID, 10, 64)
	return webhookreg.Result{Outcome: outcome, HookID: hookID, Insecure: strings.HasPrefix(saved.CallbackURL, "http://")}
}

func describeSubscriptionError(repo store.Repo, err error) string {
	if strings.Contains(err.Error(), "webhook operation") {
		return fmt.Sprintf("the bot account cannot manage webhooks on %s; check admin rights or unresolved operation status. The repository keeps working on polling.", repo.Slug())
	}
	return err.Error()
}
