package webhookreg_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

const publicURL = "https://hooks.example.com/litefactory/webhook"

// fakeHook is one hook on the fake GitHub.
type fakeHook struct {
	ID          int64    `json:"id"`
	Active      bool     `json:"active"`
	Events      []string `json:"events"`
	URL         string
	ContentType string
	Secret      string
}

func (h fakeHook) wire() map[string]any {
	return map[string]any{
		"id": h.ID, "active": h.Active, "events": h.Events,
		"config": map[string]any{"url": h.URL, "content_type": h.ContentType},
	}
}

// fakeGitHub is a stateful hook API for one repository, acme/api.
type fakeGitHub struct {
	mu     sync.Mutex
	hooks  map[int64]*fakeHook
	nextID int64
	calls  []string
	// listStatus, createStatus and getStatus force an error response.
	listStatus, createStatus, getStatus int
	log                                 *[]string
	srv                                 *httptest.Server
}

func newFakeGitHub(t *testing.T, order *[]string) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{hooks: map[int64]*fakeHook{}, nextID: 100, log: order}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if f.log != nil {
		*f.log = append(*f.log, "github "+r.Method)
	}
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(status int) {
		reply(status, map[string]string{"message": http.StatusText(status)})
	}
	base := "/repos/acme/api/hooks"
	switch {
	case r.URL.Path == base && r.Method == http.MethodGet:
		if f.listStatus != 0 {
			fail(f.listStatus)
			return
		}
		out := []map[string]any{}
		ids := make([]int64, 0, len(f.hooks))
		for id := range f.hooks {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			out = append(out, f.hooks[id].wire())
		}
		reply(200, out)
	case r.URL.Path == base && r.Method == http.MethodPost:
		if f.createStatus != 0 {
			fail(f.createStatus)
			return
		}
		h := &fakeHook{ID: f.nextID}
		f.nextID++
		f.apply(h, r)
		f.hooks[h.ID] = h
		reply(201, h.wire())
	case strings.HasPrefix(r.URL.Path, base+"/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, base+"/"), 10, 64)
		h, ok := f.hooks[id]
		if f.getStatus != 0 && r.Method == http.MethodGet {
			fail(f.getStatus)
			return
		}
		if !ok {
			fail(404)
			return
		}
		if r.Method == http.MethodPatch {
			f.apply(h, r)
		}
		reply(200, h.wire())
	default:
		fail(404)
	}
}

func (f *fakeGitHub) apply(h *fakeHook, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var in struct {
		Active bool     `json:"active"`
		Events []string `json:"events"`
		Config struct {
			URL         string `json:"url"`
			ContentType string `json:"content_type"`
			InsecureSSL string `json:"insecure_ssl"`
			Secret      string `json:"secret"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		panic(err)
	}
	if in.Config.InsecureSSL != "0" {
		panic("insecure_ssl is not 0")
	}
	h.Active, h.Events, h.URL = in.Active, in.Events, in.Config.URL
	h.ContentType, h.Secret = in.Config.ContentType, in.Config.Secret
}

func (f *fakeGitHub) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "GET ") {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeGitHub) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeKeychain keeps secrets in memory; it never touches the real keychain.
type fakeKeychain struct {
	mu       sync.Mutex
	items    map[string]string
	readErr  error
	writeErr error
	log      *[]string
	writes   int
}

func (k *fakeKeychain) Read(_ context.Context, service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.readErr != nil {
		return "", k.readErr
	}
	v, ok := k.items[service+"/"+account]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func (k *fakeKeychain) Write(_ context.Context, service, account, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.log != nil {
		*k.log = append(*k.log, "keychain write")
	}
	if k.writeErr != nil {
		return k.writeErr
	}
	k.writes++
	k.items[service+"/"+account] = value
	return nil
}

type env struct {
	gh    *fakeGitHub
	kc    *fakeKeychain
	db    *store.DB
	repo  store.Repo
	order []string
}

func (e *env) registrar(t *testing.T, url string) webhookreg.Registrar {
	t.Helper()
	client, err := github.New(github.Options{APIBase: e.gh.srv.URL, Token: secrets.NewToken("ghp_token"), HTTPClient: e.gh.srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return webhookreg.Registrar{Forge: client, Keychain: e.kc, Store: e.db, PublicURL: url}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{}
	e.gh = newFakeGitHub(t, &e.order)
	e.kc = &fakeKeychain{items: map[string]string{}, log: &e.order}
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "lf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.db = db
	e.repo, err = db.UpsertRepo(t.Context(), store.Repo{
		Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api",
		RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) secret() string { return e.kc.items[webhookreg.KeychainService+"/acme/api"] }

func TestEnsureSkipsWithoutAPublicURL(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.registrar(t, "").Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Skipped || !strings.Contains(res.Reason, "webhook.public_url") {
		t.Fatalf("res = %+v", res)
	}
	if e.gh.callCount() != 0 || e.kc.writes != 0 || len(e.order) != 0 {
		t.Errorf("a skipped registration did work: %v", e.order)
	}
}

func TestEnsureCreatesTheHookStoresTheSecretFirstAndRecordsTheID(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Created || res.HookID != 100 {
		t.Fatalf("res = %+v", res)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(e.secret()) {
		t.Errorf("secret = %q, want 64 hex characters", e.secret())
	}
	if e.order[0] != "keychain write" {
		t.Errorf("order = %v, want the keychain write before any GitHub call", e.order)
	}
	h := e.gh.hooks[100]
	if !h.Active || h.URL != publicURL || h.ContentType != "json" || h.Secret != e.secret() ||
		!slices.Equal(h.Events, []string{"issues", "issue_comment", "sub_issues"}) {
		t.Errorf("hook = %+v", h)
	}
	rec, err := e.db.GetRepoWebhook(t.Context(), e.repo.ID)
	if err != nil || rec.HookID != 100 || rec.URL != publicURL {
		t.Errorf("record = %+v, %v", rec, err)
	}
	if res.Insecure {
		t.Error("an https URL was flagged insecure")
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	reg := e.registrar(t, publicURL)
	reg.Ensure(t.Context(), e.repo)
	secret := e.secret()
	e.gh.calls = nil

	for range 2 {
		res := reg.Ensure(t.Context(), e.repo)
		if res.Outcome != webhookreg.AlreadyCorrect || res.HookID != 100 {
			t.Fatalf("res = %+v", res)
		}
	}
	if w := e.gh.writes(); len(w) != 0 {
		t.Errorf("repeat runs wrote to GitHub: %v", w)
	}
	if len(e.gh.hooks) != 1 || e.secret() != secret || e.kc.writes != 1 {
		t.Errorf("hooks=%d secret changed=%v keychain writes=%d", len(e.gh.hooks), e.secret() != secret, e.kc.writes)
	}
}

func TestEnsureRepairsADriftedHook(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	reg := e.registrar(t, publicURL)
	reg.Ensure(t.Context(), e.repo)
	h := e.gh.hooks[100]
	h.Active, h.ContentType, h.Events = false, "form", []string{"issues", "push"}

	res := reg.Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Updated {
		t.Fatalf("res = %+v", res)
	}
	slices.Sort(res.Fields)
	if !slices.Equal(res.Fields, []string{"active", "content_type", "events"}) {
		t.Errorf("fields = %v", res.Fields)
	}
	if !h.Active || h.ContentType != "json" || !slices.Equal(h.Events, []string{"issues", "issue_comment", "sub_issues"}) {
		t.Errorf("hook not repaired: %+v", h)
	}
	if again := reg.Ensure(t.Context(), e.repo); again.Outcome != webhookreg.AlreadyCorrect {
		t.Errorf("second run = %+v", again)
	}
}

func TestEnsureTreatsEventOrderAsIrrelevant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	reg := e.registrar(t, publicURL)
	reg.Ensure(t.Context(), e.repo)
	e.gh.hooks[100].Events = []string{"sub_issues", "issues", "issue_comment"}
	if res := reg.Ensure(t.Context(), e.repo); res.Outcome != webhookreg.AlreadyCorrect {
		t.Fatalf("res = %+v", res)
	}
}

func TestEnsureRewritesTheSecretWhenTheKeychainItemWasLost(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	reg := e.registrar(t, publicURL)
	reg.Ensure(t.Context(), e.repo)
	old := e.secret()
	delete(e.kc.items, webhookreg.KeychainService+"/acme/api")

	res := reg.Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Updated || !slices.Equal(res.Fields, []string{"secret"}) {
		t.Fatalf("res = %+v", res)
	}
	if e.secret() == old || e.gh.hooks[100].Secret != e.secret() {
		t.Error("GitHub and the keychain disagree about the secret")
	}
}

func TestEnsureAdoptsAHookAlreadyPointingAtThePublicURLAndLeavesOthersAlone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.gh.hooks[7] = &fakeHook{ID: 7, Active: true, Events: []string{"push"}, URL: "https://ci.example.com/hook", ContentType: "json", Secret: "theirs"}
	e.gh.hooks[8] = &fakeHook{ID: 8, Active: false, Events: []string{"issues"}, URL: publicURL, ContentType: "form"}
	e.gh.nextID = 200

	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Updated || res.HookID != 8 {
		t.Fatalf("res = %+v", res)
	}
	for _, want := range []string{"content_type", "events", "active", "secret"} {
		if !slices.Contains(res.Fields, want) {
			t.Errorf("fields %v lack %q", res.Fields, want)
		}
	}
	if w := e.gh.writes(); !slices.Equal(w, []string{"PATCH /repos/acme/api/hooks/8"}) {
		t.Errorf("writes = %v, want one PATCH of the adopted hook", w)
	}
	if h := e.gh.hooks[7]; h.Secret != "theirs" || h.URL != "https://ci.example.com/hook" || !slices.Equal(h.Events, []string{"push"}) {
		t.Errorf("another hook was changed: %+v", h)
	}
	if rec, err := e.db.GetRepoWebhook(t.Context(), e.repo.ID); err != nil || rec.HookID != 8 {
		t.Errorf("record = %+v, %v", rec, err)
	}
	if again := e.registrar(t, publicURL).Ensure(t.Context(), e.repo); again.Outcome != webhookreg.AlreadyCorrect {
		t.Errorf("repeat = %+v", again)
	}
}

func TestEnsureCreatesNewWhenOnlyOtherHooksExist(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.gh.hooks[7] = &fakeHook{ID: 7, Active: true, URL: "https://ci.example.com/hook", ContentType: "json"}
	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Created || len(e.gh.hooks) != 2 {
		t.Fatalf("res = %+v hooks=%d", res, len(e.gh.hooks))
	}
}

func TestEnsureClearsTheRecordedIDOn404AndCreatesANewHook(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	reg := e.registrar(t, publicURL)
	reg.Ensure(t.Context(), e.repo)
	delete(e.gh.hooks, 100)

	res := reg.Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Created || res.HookID != 100+1 {
		t.Fatalf("res = %+v", res)
	}
	if rec, err := e.db.GetRepoWebhook(t.Context(), e.repo.ID); err != nil || rec.HookID != 101 {
		t.Errorf("record = %+v, %v", rec, err)
	}
}

func TestEnsureClearsTheRecordWhenTheReplacementFails(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	reg := e.registrar(t, publicURL)
	reg.Ensure(t.Context(), e.repo)
	delete(e.gh.hooks, 100)
	e.gh.createStatus = 403

	res := reg.Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Failed {
		t.Fatalf("res = %+v", res)
	}
	if _, err := e.db.GetRepoWebhook(t.Context(), e.repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the dead hook id is still recorded: %v", err)
	}
}

func TestEnsureStoresNothingWhenGitHubRefuses(t *testing.T) {
	t.Parallel()
	for _, status := range []int{403, 404} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.gh.listStatus = status
			res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
			if res.Outcome != webhookreg.Failed {
				t.Fatalf("res = %+v", res)
			}
			for _, want := range []string{"bot account cannot manage webhooks on acme/api", "admin rights", "admin:repo_hook", "Webhooks read and write", "polling"} {
				if !strings.Contains(res.Reason, want) {
					t.Errorf("reason lacks %q: %s", want, res.Reason)
				}
			}
			if _, err := e.db.GetRepoWebhook(t.Context(), e.repo.ID); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("a hook id was recorded: %v", err)
			}
			if len(e.gh.hooks) != 0 {
				t.Error("a hook was created")
			}
		})
	}
}

func TestEnsureShowsTheStatusOfOtherForgeErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.gh.listStatus = 500
	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Failed || !strings.Contains(res.Reason, "500") || strings.Contains(res.Reason, "bot account cannot") {
		t.Fatalf("res = %+v", res)
	}
}

func TestEnsureMakesNoGitHubCallWhenTheSecretCannotBeStored(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.kc.writeErr = secrets.ErrInteractionNotAllowed
	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Failed || !strings.Contains(res.Reason, "Keychain") {
		t.Fatalf("res = %+v", res)
	}
	if e.gh.callCount() != 0 {
		t.Errorf("GitHub was called: %v", e.gh.calls)
	}
}

func TestEnsureMakesNoGitHubCallWhenTheKeychainCannotBeRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.kc.readErr = secrets.ErrInteractionNotAllowed
	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Failed || !strings.Contains(res.Reason, "locked") {
		t.Fatalf("res = %+v", res)
	}
	if e.gh.callCount() != 0 || e.kc.writes != 0 {
		t.Error("work was done after a failed read")
	}
}

func TestEnsureFlagsAPlainHTTPURL(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.registrar(t, "http://192.168.1.5:8787/litefactory/webhook").Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Created || !res.Insecure {
		t.Fatalf("res = %+v", res)
	}
}

func TestEnsureRefusesAMalformedPublicURL(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.registrar(t, "hooks.example.com").Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Failed || e.gh.callCount() != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	cases := map[string]webhookreg.Result{
		"created":               {Outcome: webhookreg.Created},
		"already correct":       {Outcome: webhookreg.AlreadyCorrect},
		"updated (url, events)": {Outcome: webhookreg.Updated, Fields: []string{"url", "events"}},
		"skipped: why":          {Outcome: webhookreg.Skipped, Reason: "why"},
		"failed: broken":        {Outcome: webhookreg.Failed, Reason: "broken"},
	}
	for want, res := range cases {
		if got := res.Describe(); got != want {
			t.Errorf("Describe() = %q, want %q", got, want)
		}
	}
}

func TestSecretNeverAppearsInReasons(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.gh.createStatus = 422
	res := e.registrar(t, publicURL).Ensure(t.Context(), e.repo)
	if res.Outcome != webhookreg.Failed {
		t.Fatalf("res = %+v", res)
	}
	if strings.Contains(fmt.Sprintf("%+v", res), e.secret()) {
		t.Error("the secret appears in the result")
	}
}
