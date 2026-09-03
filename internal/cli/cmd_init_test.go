package cli

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// initTokenEnvVar is the environment variable the tests point github.token_env
// at, and initBotToken is what it holds. The token is only ever referenced by
// name in config.yaml — a literal in the file would be a startup error, which
// is itself one of the tests below.
const (
	initTokenEnvVar = "LITEFACTORY_TEST_TOKEN"
	initBotToken    = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"
)

// initGH describes the fake `gh` a test puts on PATH.
//
// It is a shell script rather than an injected function, so these tests
// exercise the real exec.LookPath, the real exit codes and the real streams.
// The environment it is run with is asserted in internal/identity, which owns
// that boundary.
type initGH struct {
	host       string
	login      string
	userID     int64
	authStderr string
	authExit   int
}

// newInitGH is a gh that is installed and logged in to github.com as the owner.
func newInitGH() initGH {
	return initGH{host: "github.com", login: "marioatcloudvane", userID: 4242}
}

func (g initGH) install(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	script := strings.Join([]string{
		"#!/bin/sh",
		`if [ "$1" = "auth" ]; then`,
		"  printf '%s\\n' " + initQuote(g.host),
		"  printf '%s' " + initQuote(g.authStderr) + " >&2",
		"  exit " + strconv.Itoa(g.authExit),
		"fi",
		"printf '%s' " + initQuote(fmt.Sprintf(`{"login":%q,"id":%d,"type":"User"}`, g.login, g.userID)),
		"",
	}, "\n")

	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	// The real PATH stays behind the fake so the script can still resolve the
	// utilities it uses; LookPath finds the fake because it is first.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// initQuote wraps a value in single quotes so the generated script cannot
// interpret it.
func initQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// newInitHome points $LF_HOME at a directory `init` has never run in. Unlike
// newRepoHome it deliberately creates nothing: creating $LF_HOME, config.yaml
// and the database is what is under test.
func newInitHome(t *testing.T) config.Paths {
	t.Helper()

	home := filepath.Join(t.TempDir(), "home")
	t.Setenv(config.EnvHome, home)
	return config.NewPaths(home)
}

// botAPI is a fake GitHub API serving GET /user as the account the bot token
// belongs to. The account it reports can be changed between runs, so an
// idempotency test can replace the bot without rewriting config.yaml — which
// would defeat the point of asserting that config.yaml was left alone.
type botAPI struct {
	url string

	mu     sync.Mutex
	login  string
	userID int64
}

func newBotAPI(t *testing.T, login string, userID int64) *botAPI {
	t.Helper()

	api := &botAPI{login: login, userID: userID}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		api.mu.Lock()
		login, userID := api.login, api.userID
		api.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"login":%q,"id":%d,"type":"User"}`, login, userID)
	}))
	t.Cleanup(server.Close)
	api.url = server.URL
	return api
}

// setAccount changes the account the fake API answers as.
func (a *botAPI) setAccount(login string, userID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.login, a.userID = login, userID
}

// writeInitConfig writes a config.yaml before `init` runs, which is how a test
// points litefactory at a fake API and a token. `init` never overwrites it.
func writeInitConfig(t *testing.T, paths config.Paths, body string) {
	t.Helper()

	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("create %s: %v", paths.Home(), err)
	}
	if err := os.WriteFile(paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// initBotConfig is a config.yaml naming a fake API base and the token variable.
func initBotConfig(apiBase string) string {
	return fmt.Sprintf("# written by the test\ngithub:\n  api_base: %q\n  token_env: %q\n", apiBase, initTokenEnvVar)
}

// listIdentities reads the identities table back, which is the only honest way
// to check what `init` stored — and, on the failure paths, that it stored
// nothing.
func listIdentities(t *testing.T, paths config.Paths) []store.Identity {
	t.Helper()

	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	list, err := db.ListIdentities(t.Context(), store.ForgeGitHub)
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	return list
}

// newLoggingRouter is newTestRouter with a logger that can be read back, for
// the tests that assert on what was logged.
func newLoggingRouter(t *testing.T) (router *Router, stdout, stderr, logs *bytes.Buffer) {
	t.Helper()

	stdout, stderr, logs = &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	env := &Env{
		Stdout:  stdout,
		Stderr:  stderr,
		Stdin:   strings.NewReader(""),
		Logger:  slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Version: "1.2.3-test",
	}
	return NewRouter("litefactory", env, Commands()), stdout, stderr, logs
}

// TestInitCreatesTheHomeConfigAndDatabase is AC-0.1, AC-0.2 and AC-0.3 on a
// clean machine.
//
// The umask is installed here because it is main's first statement in
// production and the test binary has its own: the modes below are the whole
// point of that line (AC-B.5), and asserting them requires reproducing it. It
// is process-wide state, so this test must not run in parallel.
func TestInitCreatesTheHomeConfigAndDatabase(t *testing.T) {
	previous := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(previous) })

	paths := newInitHome(t)
	newInitGH().install(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"init"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	modes := []struct {
		path string
		want os.FileMode
	}{
		{paths.Home(), 0o700},
		{paths.ConfigPath(), 0o600},
		{paths.DBPath(), 0o600},
	}
	for _, m := range modes {
		info, err := os.Stat(m.path)
		if err != nil {
			t.Fatalf("stat %s: %v", m.path, err)
		}
		if got := info.Mode().Perm(); got != m.want {
			t.Errorf("%s mode = %04o, want %04o", m.path, got, m.want)
		}
	}

	// The configuration has to be loadable by everything else, and it has to
	// carry the S-4 defaults rather than an empty file.
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatalf("load the config init wrote: %v", err)
	}
	if cfg.Settings.PollIntervalSeconds != config.DefaultPollIntervalSeconds {
		t.Errorf("poll_interval_seconds = %d, want %d", cfg.Settings.PollIntervalSeconds, config.DefaultPollIntervalSeconds)
	}
	if cfg.Providers.GitHub.TokenEnv != "" || cfg.Providers.GitHub.TokenKeychain.Service != "" {
		t.Error("the default config.yaml names a credential source; it must reference nothing until the operator says so")
	}

	list := listIdentities(t, paths)
	if len(list) != 1 {
		t.Fatalf("stored %d identities, want 1 (the owner)", len(list))
	}
	owner := list[0]
	switch {
	case owner.Role != store.RoleOwner:
		t.Errorf("role = %q, want %q", owner.Role, store.RoleOwner)
	case owner.Handle != "marioatcloudvane":
		t.Errorf("handle = %q, want marioatcloudvane", owner.Handle)
	case owner.UserID != 4242:
		t.Errorf("user_id = %d, want 4242 — the numeric id is what the self-filter compares", owner.UserID)
	case owner.Source != store.SourceGH:
		t.Errorf("source = %q, want %q", owner.Source, store.SourceGH)
	case owner.DerivedAt.IsZero():
		t.Error("derived_at was not recorded")
	}

	// AC-0.3.
	if !strings.Contains(stdout.String(), "github: marioatcloudvane") {
		t.Errorf("stdout does not echo `github: <handle>`:\n%s", stdout)
	}
	// AC-0.5: no bot configured is a warning, not a failure, and it names what
	// is still needed.
	for _, want := range []string{"warning", "token_keychain", "security add-generic-password"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the missing-bot warning does not mention %q:\n%s", want, stderr)
		}
	}
}

// TestInitStoresTheBotIdentity is AC-0.5: a configured credential is resolved
// through the GitHub API — not through gh — and stored with its numeric id.
func TestInitStoresTheBotIdentity(t *testing.T) {
	paths := newInitHome(t)
	writeInitConfig(t, paths, initBotConfig(newBotAPI(t, "litefactory-bot", 99).url))
	t.Setenv(initTokenEnvVar, initBotToken)
	newInitGH().install(t)
	router, stdout, stderr := newLoggingRouterStreams(t)

	if code := router.Run(t.Context(), []string{"init"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	list := listIdentities(t, paths)
	if len(list) != 2 {
		t.Fatalf("stored %d identities, want 2\n%+v", len(list), list)
	}
	bot := list[0] // ordered by role: bot, then owner.
	switch {
	case bot.Role != store.RoleBot:
		t.Fatalf("first row is %q, want the bot", bot.Role)
	case bot.Handle != "litefactory-bot":
		t.Errorf("bot handle = %q, want litefactory-bot", bot.Handle)
	case bot.UserID != 99:
		t.Errorf("bot user_id = %d, want 99", bot.UserID)
	case bot.Source != store.SourceForgeAPI:
		t.Errorf("bot source = %q, want %q", bot.Source, store.SourceForgeAPI)
	}
	if !strings.Contains(stdout.String(), "litefactory-bot") {
		t.Errorf("stdout does not report the bot identity:\n%s", stdout)
	}
	if strings.Contains(stderr.String(), "warning") {
		t.Errorf("init warned about a bot it resolved:\n%s", stderr)
	}
}

// newLoggingRouterStreams is newLoggingRouter without the log buffer, for tests
// that only care about the command's own output.
func newLoggingRouterStreams(t *testing.T) (*Router, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	router, stdout, stderr, _ := newLoggingRouter(t)
	return router, stdout, stderr
}

// TestInitNeverPrintsTheBotToken is AC-0.11, asserted on this command's own
// code path rather than on the type in isolation: the token travels through
// init, through the GitHub client and into its request logging, and none of
// those may print it under %v, %s or slog.Any.
func TestInitNeverPrintsTheBotToken(t *testing.T) {
	paths := newInitHome(t)
	writeInitConfig(t, paths, initBotConfig(newBotAPI(t, "litefactory-bot", 99).url))
	t.Setenv(initTokenEnvVar, initBotToken)
	newInitGH().install(t)
	router, stdout, stderr, logs := newLoggingRouter(t)

	if code := router.Run(t.Context(), []string{"init"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	streams := map[string]*bytes.Buffer{"stdout": stdout, "stderr": stderr, "the log": logs}
	for name, buf := range streams {
		if strings.Contains(buf.String(), initBotToken) {
			t.Errorf("the bot token appears in %s:\n%s", name, buf)
		}
	}
	if logs.Len() == 0 {
		t.Error("nothing was logged at debug level, so this proves nothing; check the logger is wired through")
	}

	// The token must not have been written to disk either: config.yaml holds
	// the variable's name and the database holds identities, never credentials.
	for _, path := range []string{paths.ConfigPath(), paths.DBPath()} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytes.Contains(data, []byte(initBotToken)) {
			t.Errorf("the bot token was written to %s", path)
		}
	}
}

// TestInitRefusesWhenTheBotIsTheOwner is AC-0.6, the check the whole story
// turns on. The comparison is by numeric id, and neither row may be stored.
func TestInitRefusesWhenTheBotIsTheOwner(t *testing.T) {
	paths := newInitHome(t)
	// Same id, different handle: a rename must not hide a self-loop, which is
	// exactly why the comparison is not on the handle.
	writeInitConfig(t, paths, initBotConfig(newBotAPI(t, "mario-renamed", 4242).url))
	t.Setenv(initTokenEnvVar, initBotToken)
	newInitGH().install(t)
	router, _, stderr := newLoggingRouterStreams(t)

	if code := router.Run(t.Context(), []string{"init"}); code != ExitPrecondition {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}
	for _, want := range []string{"same account", "loop", "Neither identity was stored"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the self-loop refusal does not mention %q:\n%s", want, stderr)
		}
	}
	if list := listIdentities(t, paths); len(list) != 0 {
		t.Errorf("stored %d identities after refusing; want none\n%+v", len(list), list)
	}
}

// TestInitFailsWhenGHCannotAnswer is AC-0.4: every way gh can fail is exit 3,
// prints the command and gh's stderr, and leaves no partial identity row.
func TestInitFailsWhenGHCannotAnswer(t *testing.T) {
	tests := []struct {
		name     string
		absent   bool
		gh       initGH
		wantHas  []string
		wantSame string
	}{
		{
			name:    "gh is not installed",
			absent:  true,
			wantHas: []string{"gh", "PATH="},
		},
		{
			name: "gh is not logged in",
			gh: initGH{
				authExit:   1,
				authStderr: "You are not logged into any GitHub hosts. To log in, run: gh auth login",
			},
			wantHas: []string{"gh auth status", "not logged into any GitHub hosts"},
		},
		{
			name: "gh exits non-zero",
			gh: initGH{
				host:       "github.com",
				authExit:   2,
				authStderr: "error connecting to github.com: no such host",
			},
			wantHas: []string{"gh auth status", "no such host"},
		},
		{
			name:    "gh is logged in elsewhere",
			gh:      initGH{host: "ghe.acme.example", login: "mario", userID: 7},
			wantHas: []string{"only github.com is supported", "ghe.acme.example"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths := newInitHome(t)
			if tc.absent {
				t.Setenv("PATH", t.TempDir())
			} else {
				tc.gh.install(t)
			}
			router, stdout, stderr := newLoggingRouterStreams(t)

			if code := router.Run(t.Context(), []string{"init"}); code != ExitPrecondition {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
			}
			for _, want := range tc.wantHas {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("the failure does not mention %q:\n%s", want, stderr)
				}
			}
			if stdout.Len() != 0 {
				t.Errorf("init reported success on stdout while failing:\n%s", stdout)
			}
			if list := listIdentities(t, paths); len(list) != 0 {
				t.Errorf("wrote %d identity rows on a failed init; want none\n%+v", len(list), list)
			}
		})
	}
}

// TestInitIsIdempotent is AC-0.7: a re-run refreshes both handles and ids in
// place, creates no duplicate row, and leaves an existing config.yaml exactly
// as the operator left it.
func TestInitIsIdempotent(t *testing.T) {
	paths := newInitHome(t)
	api := newBotAPI(t, "litefactory-bot", 99)
	writeInitConfig(t, paths, initBotConfig(api.url))
	t.Setenv(initTokenEnvVar, initBotToken)
	newInitGH().install(t)
	router, stdout, stderr := newLoggingRouterStreams(t)

	if code := router.Run(t.Context(), []string{"init"}); code != ExitOK {
		t.Fatalf("first run: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	before, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}

	// The owner renamed their account and the token was moved to a different
	// bot, which is the realistic reason to run init twice. config.yaml is left
	// untouched by the test, so the comparison below means something.
	api.setAccount("litefactory-bot-2", 100)
	initGH{host: "github.com", login: "mario-renamed", userID: 4242}.install(t)
	stdout.Reset()
	stderr.Reset()

	if code := router.Run(t.Context(), []string{"init"}); code != ExitOK {
		t.Fatalf("second run: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	list := listIdentities(t, paths)
	if len(list) != 2 {
		t.Fatalf("stored %d identities after two runs, want 2\n%+v", len(list), list)
	}
	byRole := map[store.IdentityRole]store.Identity{}
	for _, id := range list {
		byRole[id.Role] = id
	}
	if got := byRole[store.RoleOwner]; got.Handle != "mario-renamed" || got.UserID != 4242 {
		t.Errorf("owner = %s (id %d), want mario-renamed (id 4242): the re-run did not refresh it", got.Handle, got.UserID)
	}
	if got := byRole[store.RoleBot]; got.Handle != "litefactory-bot-2" || got.UserID != 100 {
		t.Errorf("bot = %s (id %d), want litefactory-bot-2 (id 100): the re-run did not refresh it", got.Handle, got.UserID)
	}

	after, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("init rewrote an existing config.yaml:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(stdout.String(), "kept") {
		t.Errorf("the re-run does not say the existing config.yaml was kept:\n%s", stdout)
	}
}

// TestInitRejectsABrokenConfig covers AC-0.9 and AC-0.10 through `init`: a
// literal credential is exit 3, and an unknown key is a startup error that
// names the key rather than a line that is quietly ignored.
func TestInitRejectsABrokenConfig(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
		wantHas  []string
	}{
		{
			name:     "a literal credential",
			body:     "github:\n  token_env: \"LF_TOKEN\"\ndefault_base_branch: \"" + initBotToken + "\"\n",
			wantCode: ExitPrecondition,
			wantHas:  []string{"default_base_branch"},
		},
		{
			name:     "an unknown key",
			body:     "poll_intervall_seconds: 30\n",
			wantCode: ExitFailure,
			wantHas:  []string{"poll_intervall_seconds"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths := newInitHome(t)
			writeInitConfig(t, paths, tc.body)
			newInitGH().install(t)
			router, _, stderr := newLoggingRouterStreams(t)

			if code := router.Run(t.Context(), []string{"init"}); code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, tc.wantCode, stderr)
			}
			for _, want := range tc.wantHas {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("the failure does not name %q:\n%s", want, stderr)
				}
			}
		})
	}
}

// TestInitWarnsWhenTheBotTokenCannotBeResolved covers the other half of
// AC-0.5. A reference that points at nothing is not a reason to leave the
// machine without an owner identity — but it must say what it tried.
func TestInitWarnsWhenTheBotTokenCannotBeResolved(t *testing.T) {
	paths := newInitHome(t)
	writeInitConfig(t, paths, initBotConfig("https://api.github.com"))
	newInitGH().install(t)
	router, stdout, stderr := newLoggingRouterStreams(t)

	// initTokenEnvVar is deliberately not set.
	if code := router.Run(t.Context(), []string{"init"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stderr.String(), initTokenEnvVar) {
		t.Errorf("the warning does not name the reference it tried:\n%s", stderr)
	}
	if !strings.Contains(stdout.String(), "github: marioatcloudvane") {
		t.Errorf("stdout does not echo the owner:\n%s", stdout)
	}
	list := listIdentities(t, paths)
	if len(list) != 1 || list[0].Role != store.RoleOwner {
		t.Fatalf("stored %+v, want the owner alone", list)
	}
}
