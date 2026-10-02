package webhookreg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// KeychainService is the keychain service every webhook secret is stored
// under; the account is "<owner>/<name>". The daemon reads the same item.
const KeychainService = "litefactory-webhook"

// Events are the GitHub events the listener understands, and exactly the ones
// a registered hook subscribes to.
var Events = []string{"issues", "issue_comment", "sub_issues"}

// secretBytes is the length of a generated secret before hex encoding.
const secretBytes = 32

// Forge is the hook API of the forge. *github.Client satisfies it.
type Forge interface {
	ListHooks(ctx context.Context, repo github.Repo) ([]github.Hook, error)
	GetHook(ctx context.Context, repo github.Repo, id int64) (github.Hook, error)
	CreateHook(ctx context.Context, repo github.Repo, spec github.HookSpec) (github.Hook, error)
	EditHook(ctx context.Context, repo github.Repo, id int64, spec github.HookSpec) (github.Hook, error)
}

// Keychain reads and writes webhook secrets.
type Keychain interface {
	// Read returns the secret, or an error wrapping [secrets.ErrNotFound] when
	// there is none.
	Read(ctx context.Context, service, account string) (string, error)
	// Write stores the secret, replacing any earlier one.
	Write(ctx context.Context, service, account, value string) error
}

// Store records which hook belongs to which repository.
type Store interface {
	GetRepoWebhook(ctx context.Context, repoID int64) (store.RepoWebhook, error)
	SetRepoWebhook(ctx context.Context, repoID, hookID int64, url string, at time.Time) error
	ClearRepoWebhook(ctx context.Context, repoID int64) error
}

// SystemKeychain is the [Keychain] backed by the macOS keychain. Zero seams use
// /usr/bin/security.
type SystemKeychain struct {
	Resolver secrets.Resolver
	Writer   secrets.Writer
}

// Read implements [Keychain].
func (k SystemKeychain) Read(ctx context.Context, service, account string) (string, error) {
	token, err := k.Resolver.Resolve(ctx, secrets.KeychainRef(service, account))
	if err != nil {
		// Unwrap the "bot token:" framing: this is not the bot token.
		var re *secrets.ResolveError
		if errors.As(err, &re) {
			return "", re.Err
		}
		return "", err
	}
	return token.Reveal(), nil
}

// Write implements [Keychain].
func (k SystemKeychain) Write(ctx context.Context, service, account, value string) error {
	return k.Writer.Write(ctx, secrets.KeychainRef(service, account), value)
}

// Outcome is what [Registrar.Ensure] did.
type Outcome string

// The outcomes of Ensure.
const (
	// Created means a new hook was registered.
	Created Outcome = "created"
	// Updated means an existing hook was edited; see [Result.Fields].
	Updated Outcome = "updated"
	// AlreadyCorrect means nothing needed changing.
	AlreadyCorrect Outcome = "already correct"
	// Skipped means registration was not attempted; see [Result.Reason].
	Skipped Outcome = "skipped"
	// Failed means registration was attempted and did not complete; see
	// [Result.Reason].
	Failed Outcome = "failed"
)

// Result describes one Ensure. A Failed or Skipped result is not an error: the
// repository keeps working on polling.
type Result struct {
	// Outcome is the headline.
	Outcome Outcome
	// Fields names what an Updated hook had wrong: url, content_type, events,
	// active, secret.
	Fields []string
	// Reason explains a Skipped or Failed outcome, as a message for the owner.
	Reason string
	// HookID is the hook on GitHub, when there is one.
	HookID int64
	// Insecure is true when the public URL is plain http, so payloads travel
	// unencrypted. Callers print it as a warning.
	Insecure bool
}

// OK reports whether the repository now has a correct webhook.
func (r Result) OK() bool {
	return r.Outcome == Created || r.Outcome == Updated || r.Outcome == AlreadyCorrect
}

// Describe renders the result as one line for the owner.
func (r Result) Describe() string {
	switch r.Outcome {
	case Updated:
		return fmt.Sprintf("updated (%s)", strings.Join(r.Fields, ", "))
	case Skipped, Failed:
		return fmt.Sprintf("%s: %s", r.Outcome, r.Reason)
	default:
		return string(r.Outcome)
	}
}

// Registrar registers webhooks. All fields are required except Now and Random.
type Registrar struct {
	Forge    Forge
	Keychain Keychain
	Store    Store
	// PublicURL is webhook.public_url.
	PublicURL string
	// Now is the clock for the recorded timestamp. Nil means time.Now.
	Now func() time.Time
	// Random supplies secret bytes. Nil means crypto/rand.
	Random io.Reader
}

// Ensure brings repo's webhook to the desired state. It is idempotent.
func (r Registrar) Ensure(ctx context.Context, repo store.Repo) Result {
	if strings.TrimSpace(r.PublicURL) == "" {
		return Result{Outcome: Skipped, Reason: "webhook.public_url is not set in config.yaml; set it to register a webhook (the repository keeps working on polling)"}
	}
	u, err := url.Parse(r.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Result{Outcome: Failed, Reason: fmt.Sprintf("webhook.public_url %q is not an absolute http or https URL", r.PublicURL)}
	}
	insecure := u.Scheme == "http"
	finish := func(res Result) Result { res.Insecure = insecure; return res }

	forgeRepo := github.Repo{Owner: repo.Owner, Name: repo.Name}
	account := repo.Slug()

	secret, generated, failure := r.secretFor(ctx, account)
	if failure != "" {
		return finish(Result{Outcome: Failed, Reason: failure})
	}
	spec := github.HookSpec{URL: r.PublicURL, Events: Events, Active: true, Secret: github.NewHookSecret(secret)}

	hook, found, trusted, res := r.find(ctx, repo, forgeRepo)
	if res != nil {
		return finish(*res)
	}

	if !found {
		created, err := r.Forge.CreateHook(ctx, forgeRepo, spec)
		if err != nil {
			return finish(Result{Outcome: Failed, Reason: describeForgeError(forgeRepo, "create the webhook", err)})
		}
		if failure := r.record(ctx, repo, created.ID); failure != "" {
			return finish(Result{Outcome: Failed, Reason: failure, HookID: created.ID})
		}
		return finish(Result{Outcome: Created, HookID: created.ID})
	}

	fields := wrongFields(hook, r.PublicURL)
	// GitHub never returns a secret, so it can only be taken as correct when
	// this installation recorded the hook and the keychain secret is the one it
	// registered. An adopted hook, or a regenerated secret, is rewritten.
	if generated || !trusted {
		fields = append(fields, "secret")
	}
	if len(fields) == 0 {
		return finish(Result{Outcome: AlreadyCorrect, HookID: hook.ID})
	}
	if _, err := r.Forge.EditHook(ctx, forgeRepo, hook.ID, spec); err != nil {
		return finish(Result{Outcome: Failed, Reason: describeForgeError(forgeRepo, "update the webhook", err), HookID: hook.ID})
	}
	if failure := r.record(ctx, repo, hook.ID); failure != "" {
		return finish(Result{Outcome: Failed, Reason: failure, HookID: hook.ID})
	}
	return finish(Result{Outcome: Updated, Fields: fields, HookID: hook.ID})
}

// secretFor reads the repository's secret, generating and storing a new one
// when there is none. failure is a message when it could not be done.
func (r Registrar) secretFor(ctx context.Context, account string) (secret string, generated bool, failure string) {
	value, err := r.Keychain.Read(ctx, KeychainService, account)
	switch {
	case err == nil && value != "":
		return value, false, ""
	case err != nil && !errors.Is(err, secrets.ErrNotFound):
		return "", false, fmt.Sprintf("could not read the webhook secret for %s from the Keychain: %v", account, err)
	}

	random := r.Random
	if random == nil {
		random = rand.Reader
	}
	raw := make([]byte, secretBytes)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", false, fmt.Sprintf("could not generate a webhook secret: %v", err)
	}
	value = hex.EncodeToString(raw)
	if err := r.Keychain.Write(ctx, KeychainService, account, value); err != nil {
		return "", false, fmt.Sprintf("could not store the webhook secret for %s in the Keychain: %v", account, err)
	}
	return value, true, ""
}

// find locates litefactory's hook: the recorded id first, then a hook already
// pointing at the public URL. trusted is true only for the recorded hook. A
// non-nil res ends Ensure.
func (r Registrar) find(ctx context.Context, repo store.Repo, forgeRepo github.Repo) (hook github.Hook, found, trusted bool, res *Result) {
	recorded, err := r.Store.GetRepoWebhook(ctx, repo.ID)
	switch {
	case err == nil:
		got, err := r.Forge.GetHook(ctx, forgeRepo, recorded.HookID)
		switch {
		case err == nil:
			return got, true, true, nil
		case errors.Is(err, github.ErrNotFound):
			// The hook was deleted on GitHub: forget it and register a new one.
			if err := r.Store.ClearRepoWebhook(ctx, repo.ID); err != nil {
				return github.Hook{}, false, false, &Result{Outcome: Failed, Reason: fmt.Sprintf("could not clear the recorded webhook: %v", err)}
			}
			return github.Hook{}, false, false, nil
		default:
			return github.Hook{}, false, false, &Result{Outcome: Failed, Reason: describeForgeError(forgeRepo, "read the webhook", err)}
		}
	case !errors.Is(err, store.ErrNotFound):
		return github.Hook{}, false, false, &Result{Outcome: Failed, Reason: fmt.Sprintf("could not read the recorded webhook: %v", err)}
	}

	hooks, err := r.Forge.ListHooks(ctx, forgeRepo)
	if err != nil {
		return github.Hook{}, false, false, &Result{Outcome: Failed, Reason: describeForgeError(forgeRepo, "list webhooks", err)}
	}
	for _, h := range hooks {
		if h.URL == r.PublicURL {
			return h, true, false, nil
		}
	}
	return github.Hook{}, false, false, nil
}

func (r Registrar) record(ctx context.Context, repo store.Repo, hookID int64) string {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	if err := r.Store.SetRepoWebhook(ctx, repo.ID, hookID, r.PublicURL, now()); err != nil {
		return fmt.Sprintf("the webhook exists on GitHub but its id could not be recorded: %v", err)
	}
	return ""
}

// wrongFields compares a hook with the desired one. The secret is not
// comparable and is handled by the caller.
func wrongFields(h github.Hook, publicURL string) []string {
	var fields []string
	if h.URL != publicURL {
		fields = append(fields, "url")
	}
	if h.ContentType != github.HookContentTypeJSON {
		fields = append(fields, "content_type")
	}
	got := slices.Clone(h.Events)
	want := slices.Clone(Events)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		fields = append(fields, "events")
	}
	if !h.Active {
		fields = append(fields, "active")
	}
	return fields
}

// describeForgeError turns a forge failure into the message the owner reads.
func describeForgeError(repo github.Repo, doing string, err error) string {
	if errors.Is(err, github.ErrForbidden) || errors.Is(err, github.ErrNotFound) {
		return fmt.Sprintf("the bot account cannot manage webhooks on %s; it needs admin rights on the repository "+
			"(admin:repo_hook for a classic token, Webhooks read and write for a fine-grained token). "+
			"The repository keeps working on polling.", repo)
	}
	var apiErr *github.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.Message
		if msg == "" {
			msg = "no message"
		}
		return fmt.Sprintf("could not %s on %s: GitHub answered %d: %s", doing, repo, apiErr.StatusCode, msg)
	}
	return fmt.Sprintf("could not %s on %s: %v", doing, repo, err)
}
