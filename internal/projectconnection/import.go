package projectconnection

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Initialize repeats safely: existing explicit pointers and execution snapshots
// are untouched. Incomplete historical evidence blocks affected projects while
// unrelated projects remain usable. No publication ledger bytes are changed.
func Initialize(ctx context.Context, db *store.DB, cfg config.Config, factory *Factory) error {
	origin, err := ticket.CanonicalOrigin(cfg.Providers.GitHub.APIBase)
	if err != nil {
		return ErrInvalidConnection
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]int)
	for _, repo := range repos {
		seen[repo.Forge+":"+strings.ToLower(repo.Slug())]++
	}
	for _, repo := range repos {
		code, tickets := legacyVersions(repo, cfg, origin)
		reason := ""
		if repo.Forge != "github" {
			reason = "unsupported_legacy_provider"
		} else if seen[repo.Forge+":"+strings.ToLower(repo.Slug())] > 1 {
			reason = "duplicate_project_registration"
		}
		if reason == "" && code.CredentialSource == "keychain" && (code.CredentialService == "" || code.CredentialAccount == "") {
			reason = "incomplete_legacy_credential_reference"
		}
		if reason == "" && strings.Contains(code.RemoteURL, "://") {
			remote, parseErr := url.Parse(code.RemoteURL)
			if parseErr != nil || remote.RawQuery != "" || remote.Fragment != "" || ((remote.Scheme == "http" || remote.Scheme == "https") && remote.User != nil) {
				reason = "invalid_legacy_code_remote"
			} else if remote.User != nil {
				if _, password := remote.User.Password(); password {
					reason = "invalid_legacy_code_remote"
				}
			}
		}
		if reason == "" {
			if err := db.ImportLegacyConnections(ctx, code, tickets); err != nil {
				return err
			}
			sessions, err := db.ListRepoSessions(ctx, repo.ID)
			if err != nil {
				return err
			}
			for _, session := range sessions {
				if _, err := db.GetExecutionSnapshot(ctx, session.ID); err == nil {
					continue
				} else if !errors.Is(err, store.ErrNotFound) {
					return err
				}
				if err := reconcileSession(ctx, db, factory, session, code, tickets); err != nil {
					reason = importReason(err)
					break
				}
			}
		}
		status := "complete"
		if reason != "" {
			status = "conflict"
		}
		if err := db.SetProjectImportState(ctx, store.ProjectImportState{RepoID: repo.ID, Status: status, Reason: reason}); err != nil {
			return err
		}
	}
	return nil
}

func legacyVersions(repo store.Repo, cfg config.Config, origin ticket.Origin) (store.ConnectionVersion, store.ConnectionVersion) {
	source := ""
	if cfg.Providers.GitHub.TokenKeychain.Service != "" || cfg.Providers.GitHub.TokenKeychain.Account != "" {
		source = "keychain"
	} else if cfg.Providers.GitHub.TokenEnv != "" {
		source = "env"
	}
	common := store.ConnectionVersion{RepoID: repo.ID, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: source, CredentialService: cfg.Providers.GitHub.TokenKeychain.Service, CredentialAccount: cfg.Providers.GitHub.TokenKeychain.Account, CredentialEnv: cfg.Providers.GitHub.TokenEnv, Enabled: true, Provenance: "legacy"}
	code := common
	code.Role = store.CodeConnection
	code.ClonePath = repo.Path
	code.RemoteURL = repo.RemoteURL
	code.BaseBranch = repo.BaseBranch
	code.FeaturePrefix = repo.FeaturePrefix
	code.WorktreeRoot = repo.WorktreeRoot
	tickets := common
	tickets.Role = store.TicketConnection
	return code, tickets
}

func reconcileSession(ctx context.Context, db *store.DB, factory *Factory, session store.Session, code, tickets store.ConnectionVersion) error {
	alias, err := naming.ParseRef(session.TaskRef)
	if err != nil || alias.Forge != naming.ForgeGitHub || alias.Owner+"/"+alias.Repo != code.Destination || alias.Issue != session.IssueNumber {
		return store.ErrTicketIdentityConflict
	}
	mapping, err := db.GetSessionTicket(ctx, session.ID)
	if errors.Is(err, store.ErrNotFound) {
		mapping, err = db.BindLegacySessionOrigin(ctx, session.ID, tickets.Origin)
	}
	if err != nil {
		return err
	}
	// The prerequisite mapping is stronger evidence than current global origin.
	tickets.Origin = mapping.Locator.Origin
	code.Origin = mapping.Locator.Origin
	if mapping.Locator.Container != tickets.Destination || mapping.TaskRef != session.TaskRef {
		return store.ErrTicketIdentityConflict
	}
	reports, err := db.ListSessionReportDeliveries(ctx, session.ID)
	if err != nil {
		return err
	}
	effects, err := db.ListTicketEffectsForAddress(ctx, mapping.Locator.Container, mapping.Locator.Key)
	if err != nil {
		return err
	}
	expectedActor := int64(0)
	for _, effect := range effects {
		if effect.Locator != mapping.Locator || (mapping.RemoteID != "" && effect.RemoteID != "" && effect.RemoteID != mapping.RemoteID) {
			return store.ErrTicketIdentityConflict
		}
		if effect.Actor != (ticket.Identity{}) {
			if effect.Actor.Provider != mapping.Locator.Provider || effect.Actor.Origin != mapping.Locator.Origin {
				return ErrActorChanged
			}
			actorID, parseErr := strconv.ParseInt(effect.Actor.RemoteID, 10, 64)
			if parseErr != nil || actorID <= 0 || (expectedActor > 0 && expectedActor != actorID) {
				return ErrActorChanged
			}
			expectedActor = actorID
		}
	}
	// Deliveries may address other issues or pull requests of the same
	// repository (story sub-issues, refinement children, code proposals);
	// each is checked and bound against its own destination.
	destinations := make([]ticket.Locator, len(reports))
	for i, report := range reports {
		if _, ok := legacyRepositoryKey(report.Task, code.Destination); !ok {
			return store.ErrTicketIdentityConflict
		}
		key, ok := legacyRepositoryKey(report.Destination, code.Destination)
		if !ok {
			return store.ErrTicketIdentityConflict
		}
		destinations[i] = ticket.Locator{Provider: mapping.Locator.Provider, Origin: mapping.Locator.Origin, Container: mapping.Locator.Container, Key: key}
		if expectedActor > 0 && expectedActor != report.BotUserID {
			return ErrActorChanged
		}
		expectedActor = report.BotUserID
		effect, err := db.GetTicketEffect(ctx, report.Key)
		if err == nil {
			own := destinations[i] == mapping.Locator
			if effect.Locator != destinations[i] || (own && mapping.RemoteID != "" && effect.RemoteID != "" && effect.RemoteID != mapping.RemoteID) {
				return store.ErrTicketIdentityConflict
			}
			if effect.Actor.RemoteID != "" && effect.Actor.RemoteID != strconv.FormatInt(expectedActor, 10) {
				return ErrActorChanged
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	// Retain historical defaults separately when current explicit configuration differs.
	current, err := db.CurrentConnection(ctx, session.RepoID, store.TicketConnection)
	if err != nil {
		return err
	}
	if sameLegacyDestination(current, tickets) {
		tickets = current
	} else {
		tickets, err = db.RetainConnectionVersion(ctx, tickets)
		if err != nil {
			return err
		}
	}
	current, err = db.CurrentConnection(ctx, session.RepoID, store.CodeConnection)
	if err != nil {
		return err
	}
	if sameLegacyDestination(current, code) && current.ClonePath == code.ClonePath && current.RemoteURL == code.RemoteURL && current.BaseBranch == code.BaseBranch && current.FeaturePrefix == code.FeaturePrefix && current.WorktreeRoot == code.WorktreeRoot {
		code = current
	} else {
		code, err = db.RetainConnectionVersion(ctx, code)
		if err != nil {
			return err
		}
	}
	clients, err := factory.Clients(ctx, tickets, expectedActor)
	if err != nil {
		return err
	}
	codeClients, err := factory.Clients(ctx, code, clients.Actor.ID)
	if err != nil {
		return err
	}
	owner, name, _ := strings.Cut(code.Destination, "/")
	access, err := codeClients.Code.InspectRepository(ctx, github.Repo{Owner: owner, Name: name})
	if err != nil {
		return err
	}
	if !strings.EqualFold(access.FullName, code.Destination) || (code.RemoteID != "" && code.RemoteID != strconv.FormatInt(access.ID, 10)) {
		return store.ErrTicketIdentityConflict
	}
	remoteID := strconv.FormatInt(access.ID, 10)
	if code.RemoteID == "" {
		code.RemoteID = remoteID
		code, err = db.RetainConnectionVersion(ctx, code)
		if err != nil {
			return err
		}
	}
	if tickets.RemoteID != "" && tickets.RemoteID != remoteID {
		return store.ErrTicketIdentityConflict
	}
	if tickets.RemoteID == "" {
		tickets.RemoteID = remoteID
		tickets, err = db.RetainConnectionVersion(ctx, tickets)
		if err != nil {
			return err
		}
	}
	var ref ticket.Ref
	if mapping.RemoteID != "" {
		ref, err = mapping.Ref()
		if err != nil {
			return err
		}
		_, err = clients.Tickets.Read(ctx, ref)
	} else {
		var snapshot ticket.Snapshot
		snapshot, err = clients.Tickets.Resolve(ctx, mapping.Locator)
		ref = snapshot.Ref
	}
	if err != nil {
		return err
	}
	for _, effect := range effects {
		if effect.RemoteID != "" && effect.RemoteID != ref.Identity.RemoteID {
			return store.ErrTicketIdentityConflict
		}
	}
	if _, err = db.BindSessionTicket(ctx, session.ID, ref); err != nil {
		return err
	}
	binding, err := db.GetTicketBinding(ctx, ref.Identity)
	if errors.Is(err, store.ErrNotFound) {
		binding, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: session.RepoID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "legacy"})
	}
	if err != nil {
		return err
	}
	if binding.RepoID != session.RepoID {
		return store.ErrBindingConflict
	}
	for _, effect := range effects {
		if _, err := db.ResolveTicketEffect(ctx, effect.Key, ref); err != nil {
			return err
		}
	}
	for i, report := range reports {
		actor := ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: strconv.FormatInt(clients.Actor.ID, 10)}
		if destinations[i] != mapping.Locator {
			// Another issue of the same repository: no remote read during import.
			if _, err := db.BindTicketEffect(ctx, report.Key, destinations[i], actor); err != nil {
				return err
			}
			continue
		}
		if _, err := db.BindTicketEffect(ctx, report.Key, ref.Locator, actor); err != nil {
			return err
		}
		if _, err := db.ResolveTicketEffect(ctx, report.Key, ref); err != nil {
			return err
		}
	}
	return db.AttachExecutionSnapshot(ctx, store.ExecutionSnapshot{SessionID: session.ID, RepoID: session.RepoID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: clients.Actor.ID, ActorLogin: clients.Actor.Login})
}

// legacyRepositoryKey returns the issue or pull request number addressed by a
// historical delivery value ("github:owner/repo#N" or "owner/repo#N") when it
// belongs to the given repository.
func legacyRepositoryKey(value, repository string) (string, bool) {
	if ref, err := naming.ParseRef(value); err == nil {
		if ref.Forge != naming.ForgeGitHub || ref.Owner+"/"+ref.Repo != repository {
			return "", false
		}
		return strconv.Itoa(ref.Issue), true
	}
	slug, number, ok := strings.Cut(value, "#")
	if !ok || slug != repository {
		return "", false
	}
	n, err := strconv.Atoi(number)
	if err != nil || n <= 0 || strconv.Itoa(n) != number {
		return "", false
	}
	return number, true
}

func sameLegacyDestination(a, b store.ConnectionVersion) bool {
	return a.Provider == b.Provider && a.Origin == b.Origin && a.Destination == b.Destination && a.CredentialSource == b.CredentialSource && a.CredentialService == b.CredentialService && a.CredentialAccount == b.CredentialAccount && a.CredentialEnv == b.CredentialEnv
}

func importReason(err error) string {
	switch {
	case errors.Is(err, store.ErrBindingConflict):
		return "historical_binding_conflict"
	case errors.Is(err, store.ErrTicketIdentityConflict):
		return "historical_destination_conflict"
	case errors.Is(err, ErrActorChanged):
		return "historical_actor_conflict"
	default:
		return "historical_connection_unresolved"
	}
}

// SeedRegistration adds missing legacy defaults using local registration alone.
// It performs no credential or network reads and preserves explicit settings.
func SeedRegistration(ctx context.Context, db *store.DB, repo store.Repo, cfg config.Config) error {
	rawOrigin := cfg.Providers.GitHub.APIBase
	if rawOrigin == "" {
		rawOrigin = config.DefaultAPIBase
	}
	origin, err := ticket.CanonicalOrigin(rawOrigin)
	if err != nil {
		return ErrInvalidConnection
	}
	code, tickets := legacyVersions(repo, cfg, origin)
	return db.ImportLegacyConnections(ctx, code, tickets)
}
