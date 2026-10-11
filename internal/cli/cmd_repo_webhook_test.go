package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

const webhookTestPublicURL = "https://hooks.example.com/litefactory/webhook"

// webhookCLI wires a fake GitHub, a fake keychain and a config naming both, so
// no test reaches the real keychain or network.
type webhookCLI struct {
	t        *testing.T
	paths    config.Paths
	router   *Router
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	mu       sync.Mutex
	items    map[string]string
	writeErr error
	status   int // forced status for every hook call
	posts    int
	patches  int
	hookURL  string
	hookSeen bool
}

func newWebhookCLI(t *testing.T, publicURL string) *webhookCLI {
	t.Helper()
	w := &webhookCLI{t: t, items: map[string]string{"litefactory/github-bot": "ghp_bot_token"}}
	srv := httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(srv.Close)

	w.paths = newRepoHome(t)
	raw, err := os.ReadFile(w.paths.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	body = strings.Replace(body, `public_url: ""`, `public_url: "`+publicURL+`"`, 1)
	body = strings.Replace(body, `api_base: "https://api.github.com"`, `api_base: "`+srv.URL+`"`, 1)
	if !strings.Contains(body, srv.URL) {
		t.Fatal("api_base was not rewritten")
	}
	if err := os.WriteFile(w.paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	w.stdout, w.stderr = &bytes.Buffer{}, &bytes.Buffer{}
	env := &Env{
		Stdout: w.stdout, Stderr: w.stderr, Stdin: strings.NewReader(""),
		Logger: slog.New(slog.DiscardHandler), Version: "test",
		Keychain: func(_ context.Context, service, account string) (string, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if v, ok := w.items[service+"/"+account]; ok {
				return v, nil
			}
			return "", secrets.ErrNotFound
		},
		KeychainWrite: func(_ context.Context, service, account, value string) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.writeErr != nil {
				return w.writeErr
			}
			w.items[service+"/"+account] = value
			return nil
		},
	}
	w.router = NewRouter("litefactory", env, Commands())
	return w
}

func (w *webhookCLI) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rw.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/user" {
		_, _ = io.WriteString(rw, `{"id":2,"login":"bot"}`)
		return
	}
	if r.URL.Path == "/repos/acme/does-not-exist" {
		_, _ = io.WriteString(rw, `{"id":101,"full_name":"acme/does-not-exist","has_issues":true}`)
		return
	}
	if w.status != 0 {
		rw.WriteHeader(w.status)
		_, _ = io.WriteString(rw, `{"message":"Resource not accessible"}`)
		return
	}
	hook := func() map[string]any {
		return map[string]any{"id": 55, "active": true, "events": webhookreg.Events,
			"config": map[string]any{"url": w.hookURL, "content_type": "json"}}
	}
	raw, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
		if w.hookSeen {
			_ = json.NewEncoder(rw).Encode([]any{hook()})
			return
		}
		_, _ = io.WriteString(rw, `[]`)
	case r.Method == http.MethodGet:
		_ = json.NewEncoder(rw).Encode(hook())
	case r.Method == http.MethodPost:
		w.posts++
		var in struct {
			Config struct {
				URL string `json:"url"`
			} `json:"config"`
		}
		_ = json.Unmarshal(raw, &in)
		w.hookURL, w.hookSeen = in.Config.URL, true
		rw.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(rw).Encode(hook())
	case r.Method == http.MethodPatch:
		w.patches++
		_ = json.NewEncoder(rw).Encode(hook())
	}
}

func (w *webhookCLI) run(args ...string) int {
	w.stdout.Reset()
	w.stderr.Reset()
	return w.router.Run(context.Background(), args)
}

func (w *webhookCLI) dumpSecrets() string {
	var b strings.Builder
	for k, v := range w.items {
		if strings.HasPrefix(k, webhookreg.KeychainService+"/") {
			b.WriteString(v)
		}
	}
	return b.String()
}

func TestWebhookKeychainServiceMatchesTheDaemon(t *testing.T) {
	if webhookreg.KeychainService != daemon.WebhookKeychainService {
		t.Fatalf("registration writes service %q, the daemon reads %q", webhookreg.KeychainService, daemon.WebhookKeychainService)
	}
}

func TestRepoAddWithoutPublicURLSkipsAndSaysSo(t *testing.T) {
	w := newWebhookCLI(t, "")
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr)
	}
	if !strings.Contains(w.stdout.String(), "skipped: webhook.public_url") || w.stderr.Len() != 0 {
		t.Errorf("want a skipped line naming webhook.public_url on stdout and no warning:\n%s\n%s", w.stdout, w.stderr)
	}
	for _, want := range []string{"webhooks are recommended", "short poll interval", "without a webhook"} {
		if !strings.Contains(w.stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, w.stdout)
		}
	}
	if len(w.items) != 1 || w.posts != 0 {
		t.Errorf("a skipped registration did work: items=%d posts=%d", len(w.items), w.posts)
	}
}

func TestRepoAddRegistersTheWebhookAndIsRepeatable(t *testing.T) {
	w := newWebhookCLI(t, webhookTestPublicURL)
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr)
	}
	if w.posts != 1 || len(w.dumpSecrets()) != 64 {
		t.Fatalf("posts=%d secret len=%d", w.posts, len(w.dumpSecrets()))
	}
	if !strings.Contains(w.stdout.String(), "webhook") || !strings.Contains(w.stdout.String(), "created") {
		t.Errorf("stdout:\n%s", w.stdout)
	}
	if w.stderr.Len() != 0 {
		t.Errorf("stderr = %q", w.stderr)
	}
	secret := w.dumpSecrets()
	if strings.Contains(w.stdout.String()+w.stderr.String(), secret) {
		t.Error("the secret was printed")
	}

	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("second add exit %d: %s", code, w.stderr)
	}
	if w.posts != 1 || w.patches != 0 || !strings.Contains(w.stdout.String(), "already correct") {
		t.Errorf("repeat: posts=%d patches=%d\n%s", w.posts, w.patches, w.stdout)
	}
}

func TestRepoAddKeepsTheRegistrationWhenTheWebhookFails(t *testing.T) {
	w := newWebhookCLI(t, webhookTestPublicURL)
	w.status = 403
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("exit %d, want 0: %s", code, w.stderr)
	}
	for _, want := range []string{"cannot manage webhooks on acme/does-not-exist", "admin rights", "repo webhook acme/does-not-exist"} {
		if !strings.Contains(w.stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, w.stderr)
		}
	}
	if !strings.Contains(w.stdout.String(), "registered") {
		t.Errorf("stdout:\n%s", w.stdout)
	}
	db, err := store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	list, err := db.ListRepos(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("repos = %v, %v", list, err)
	}
	if _, err := db.GetRepoWebhook(t.Context(), list[0].ID); err == nil {
		t.Error("a hook id was recorded for a refused hook")
	}
}

func TestRepoAddWarnsAboutPlainHTTP(t *testing.T) {
	w := newWebhookCLI(t, "http://192.168.1.5:8787/litefactory/webhook")
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr)
	}
	if !strings.Contains(w.stderr.String(), "unencrypted") {
		t.Errorf("stderr lacks the plain http warning:\n%s", w.stderr)
	}
}

func TestRepoAddMakesNoGitHubCallWhenTheSecretCannotBeStored(t *testing.T) {
	w := newWebhookCLI(t, webhookTestPublicURL)
	w.writeErr = secrets.ErrInteractionNotAllowed
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr)
	}
	if w.posts != 0 || !strings.Contains(w.stderr.String(), "Keychain") {
		t.Errorf("posts=%d stderr:\n%s", w.posts, w.stderr)
	}
}

func TestRepoWebhookRepairsOneAndAll(t *testing.T) {
	w := newWebhookCLI(t, "")
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("add: %d %s", code, w.stderr)
	}

	// Without a public URL the command cannot do its job: non-zero.
	if code := w.run("repo", "webhook", "acme/does-not-exist"); code != ExitFailure {
		t.Errorf("exit %d, want %d\n%s", code, ExitFailure, w.stdout)
	}
	if !strings.Contains(w.stdout.String(), "webhook.public_url") {
		t.Errorf("stdout:\n%s", w.stdout)
	}

	raw, _ := os.ReadFile(w.paths.ConfigPath())
	body := strings.Replace(string(raw), `public_url: ""`, `public_url: "`+webhookTestPublicURL+`"`, 1)
	if err := os.WriteFile(w.paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("repo", "webhook", "acme/does-not-exist"); code != ExitOK {
		t.Fatalf("exit %d: %s%s", code, w.stdout, w.stderr)
	}
	if got := w.stdout.String(); got != "acme/does-not-exist: created\n" {
		t.Errorf("stdout = %q", got)
	}
	if code := w.run("repo", "webhook", "--all"); code != ExitOK {
		t.Fatalf("--all exit %d: %s%s", code, w.stdout, w.stderr)
	}
	if got := w.stdout.String(); got != "acme/does-not-exist: already correct\n" {
		t.Errorf("stdout = %q", got)
	}

	w.status = 404
	if code := w.run("repo", "webhook", "--all"); code != ExitFailure {
		t.Errorf("exit %d, want non-zero when a repo could not be fixed", code)
	}
	if !strings.Contains(w.stdout.String(), "acme/does-not-exist: failed") {
		t.Errorf("stdout:\n%s", w.stdout)
	}
}

func TestRepoWebhookArguments(t *testing.T) {
	w := newWebhookCLI(t, webhookTestPublicURL)
	for _, args := range [][]string{
		{"repo", "webhook"},
		{"repo", "webhook", "--all", "acme/x"},
		{"repo", "webhook", "nonsense"},
	} {
		if code := w.run(args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, ExitUsage)
		}
	}
	if code := w.run("repo", "webhook", "acme/unregistered"); code != ExitPrecondition {
		t.Errorf("unregistered repo: exit %d, want %d", code, ExitPrecondition)
	}
}

func TestRepoWebhookDisableSavesIntentBeforeProviderFailure(t *testing.T) {
	w := newWebhookCLI(t, webhookTestPublicURL)
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("add exit %d: %s", code, w.stderr)
	}
	db, err := store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	subscriptions, err := db.ListTicketSubscriptions(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("subscriptions=%v", subscriptions)
	}
	initial := subscriptions[0]
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	w.status = 403
	if code := w.run("repo", "webhook", "disable", "--expected-revision=1", "1", initial.ID); code != ExitFailure {
		t.Fatalf("disable exit %d: %s", code, w.stderr)
	}
	db, err = store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	saved, err := db.GetTicketSubscription(t.Context(), initial.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DesiredEnabled || saved.Revision != 2 {
		t.Fatalf("failed remote disable lost intent: %+v", saved)
	}
	if code := w.run("repo", "webhook", "status", "1", initial.ID); code != ExitOK {
		t.Fatalf("status exit %d: %s", code, w.stderr)
	}
	for _, want := range []string{"desired-enabled=false", "revision 2", "source-version", "completed recovery", "polling remains active"} {
		if !strings.Contains(w.stdout.String(), want) {
			t.Errorf("status lacks %q: %s", want, w.stdout)
		}
	}
}

func TestRepoWebhookCredentialEditPreservesRetainedSourceDisable(t *testing.T) {
	w := newWebhookCLI(t, webhookTestPublicURL)
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatalf("add exit %d: %s", code, w.stderr)
	}
	db, err := store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	subscriptions, err := db.ListTicketSubscriptions(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("subscriptions=%v", subscriptions)
	}
	disabled := subscriptions[0]
	disabled.DesiredEnabled = false
	disabled, err = db.PutTicketSubscription(t.Context(), disabled, disabled.Revision)
	if err != nil {
		t.Fatal(err)
	}
	version, err := db.CurrentConnection(t.Context(), disabled.RepoID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.items["litefactory/replacement-ticket-token"] = "new-ticket-token"
	w.mu.Unlock()
	version.CredentialAccount = "replacement-ticket-token"
	edited, err := db.SetConnection(t.Context(), version, version.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID == disabled.ConnectionVersionID {
		t.Fatal("credential edit did not create a new version")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"repo", "webhook", "acme/does-not-exist"}, {"repo", "add", repo.Dir}} {
		code := w.run(args...)
		if code != ExitOK && code != ExitFailure {
			t.Fatalf("%v exit %d: %s", args, code, w.stderr)
		}
		if !strings.Contains(w.stdout.String(), "skipped: webhook is explicitly disabled for this ticket source") {
			t.Fatalf("retained disable was not reported: %s", w.stdout)
		}
	}
	if w.posts != 1 || w.patches != 0 {
		t.Fatalf("credential edit created or enabled another hook: posts=%d patches=%d", w.posts, w.patches)
	}
	db, err = store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	}()
	retained, err := db.GetTicketSubscription(t.Context(), disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.DesiredEnabled || retained.ConnectionVersionID != disabled.ConnectionVersionID || retained.Revision != disabled.Revision {
		t.Fatalf("retained subscription was changed: %+v", retained)
	}
	subscriptions, err = db.ListTicketSubscriptions(t.Context(), disabled.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("credential edit created a second subscription: %+v", subscriptions)
	}
}
