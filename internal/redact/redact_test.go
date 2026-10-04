package redact_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// commentLimit is GitHub's comment size limit, and the number the daemon passes
// in production.
const commentLimit = 65536

// newRedactor builds a Redactor with an explicitly empty environment, so a test
// is never affected by whatever the developer happens to have exported. Passing
// a non-nil empty slice is what makes the difference; nil would scan os.Environ.
func newRedactor(t *testing.T, token string) *redact.Redactor {
	t.Helper()
	return redact.New(redact.Options{
		Token:   secrets.NewToken(token),
		Environ: []string{},
	})
}

// apply runs the pass at the production comment limit and fails on error.
func apply(t *testing.T, r *redact.Redactor, text string) redact.Body {
	t.Helper()
	body, err := r.Apply(t.Context(), text, commentLimit)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !body.Valid() {
		t.Fatal("Apply returned a Body that reports Valid() == false")
	}
	return body
}

// TestPatternClasses covers every row of the S-7 table. Each case names the
// class it expects to fire, so a pattern that stops matching fails with the
// class name rather than a diff.
func TestPatternClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		class   redact.Class
		in      string
		leaked  string   // must not appear in the output
		wantSub []string // must appear in the output
	}{
		{
			name:    "github personal access token",
			class:   redact.ClassGitHubToken,
			in:      "cloning with ghp_AbCdEf0123456789AbCdEf0123456789 failed",
			leaked:  "ghp_AbCdEf0123456789AbCdEf0123456789",
			wantSub: []string{"cloning with [redacted] failed"},
		},
		{
			name:   "github oauth token",
			class:  redact.ClassGitHubToken,
			in:     "gho_0123456789abcdefghij0123456789abcdef",
			leaked: "gho_0123456789abcdefghij0123456789abcdef",
		},
		{
			name:   "github user-to-server token",
			class:  redact.ClassGitHubToken,
			in:     "ghu_0123456789abcdefghij0123456789abcdef",
			leaked: "ghu_0123456789abcdefghij0123456789abcdef",
		},
		{
			name:   "github server-to-server token",
			class:  redact.ClassGitHubToken,
			in:     "ghs_0123456789abcdefghij0123456789abcdef",
			leaked: "ghs_0123456789abcdefghij0123456789abcdef",
		},
		{
			name:   "github refresh token",
			class:  redact.ClassGitHubToken,
			in:     "ghr_0123456789abcdefghij0123456789abcdef",
			leaked: "ghr_0123456789abcdefghij0123456789abcdef",
		},
		{
			name:   "github fine-grained pat",
			class:  redact.ClassGitHubToken,
			in:     "token: github_pat_11ABCDEFG0abcdefghijkl_0123456789abcdefghij",
			leaked: "github_pat_11ABCDEFG0abcdefghijkl_0123456789abcdefghij",
		},
		{
			name:    "authorization header keeps its name",
			class:   redact.ClassAuthorization,
			in:      "> Authorization: token 0123456789abcdef\n> Accept: application/json",
			leaked:  "0123456789abcdef",
			wantSub: []string{"> Authorization: [redacted]", "> Accept: application/json"},
		},
		{
			name:    "authorization header is case insensitive",
			class:   redact.ClassAuthorization,
			in:      "authorization: Basic bWFyaW86aHVudGVyMg==",
			leaked:  "bWFyaW86aHVudGVyMg==",
			wantSub: []string{"authorization: [redacted]"},
		},
		{
			name:    "bare bearer token",
			class:   redact.ClassBearerToken,
			in:      `curl -H "X-Auth: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig"`,
			leaked:  "eyJhbGciOiJIUzI1NiJ9.payload.sig",
			wantSub: []string{"Bearer [redacted]"},
		},
		{
			name:    "aws access key id",
			class:   redact.ClassAWSAccessKey,
			in:      "aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
			leaked:  "AKIAIOSFODNN7EXAMPLE",
			wantSub: []string{"aws_access_key_id = [redacted]"},
		},
		{
			name:    "aws temporary access key id",
			class:   redact.ClassAWSAccessKey,
			in:      "using ASIAY34FZKBOKMUTVV7A for the session",
			leaked:  "ASIAY34FZKBOKMUTVV7A",
			wantSub: []string{"using [redacted] for"},
		},
		{
			name:    "url userinfo",
			class:   redact.ClassURLCredential,
			in:      "fatal: could not read https://mario:hunter2secret@github.com/acme/api.git",
			leaked:  "hunter2secret",
			wantSub: []string{"https://[redacted]@github.com/acme/api.git"},
		},
		{
			name:   "url userinfo drops the username too",
			class:  redact.ClassURLCredential,
			in:     "postgres://appuser:swordfishy@db.internal:5432/app",
			leaked: "appuser",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := apply(t, newRedactor(t, ""), tc.in)
			got := body.String()

			if strings.Contains(got, tc.leaked) {
				t.Errorf("output still contains the credential\n got: %q", got)
			}
			for _, want := range tc.wantSub {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n got: %q", want, got)
				}
			}
			if n := body.Counts().Get(tc.class); n < 1 {
				t.Errorf("counter for %s = %d, want at least 1 (counts: %v)",
					tc.class, n, body.Counts().Fired())
			}
		})
	}
}

// TestEveryClassIsCovered guards against a class being added to the enum with
// no pattern behind it — a counter that can never fire is worse than no
// counter, because the log looks clean.
func TestEveryClassIsCovered(t *testing.T) {
	t.Parallel()

	// One input per class, the same shapes the table above exercises.
	inputs := map[redact.Class]string{
		redact.ClassBotToken:      "the bot token is lf-live-bot-token-value",
		redact.ClassEnvValue:      "the env value is env-value-secret",
		redact.ClassPrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----",
		redact.ClassAuthorization: "Authorization: token abcdef0123456789",
		redact.ClassBearerToken:   "Bearer abcdef0123456789",
		redact.ClassGitHubToken:   "ghp_AbCdEf0123456789AbCdEf0123456789",
		redact.ClassAWSAccessKey:  "AKIAIOSFODNN7EXAMPLE",
		redact.ClassURLCredential: "https://user:passwordy@example.com",
	}

	r := redact.New(redact.Options{
		Token:   secrets.NewToken("lf-live-bot-token-value"),
		Environ: []string{"MY_API_KEY=env-value-secret"},
	})

	for _, class := range redact.Classes() {
		in, ok := inputs[class]
		if !ok {
			t.Errorf("class %s has no test input; every class in the enum needs a "+
				"pattern that can fire it, or the counter is decorative", class)
			continue
		}
		body := apply(t, r, in)
		if n := body.Counts().Get(class); n < 1 {
			t.Errorf("class %s did not fire on %q", class, in)
		}
	}
}

// TestBotTokenIsRedactedLiterally is AC-4.6 at its narrowest: the live
// credential, in a shape no pattern would recognise, injected into run output.
func TestBotTokenIsRedactedLiterally(t *testing.T) {
	t.Parallel()

	// Deliberately not a GitHub-shaped token: the literal pass, not the
	// pattern pass, has to be what catches this.
	const token = "zzzz-not-a-recognisable-token-shape-9999"
	r := newRedactor(t, token)

	body := apply(t, r, "the tool printed "+token+" into its own log\nand again: "+token)

	if strings.Contains(body.String(), token) {
		t.Fatalf("the bot token survived redaction: %q", body.String())
	}
	if n := body.Counts().Get(redact.ClassBotToken); n != 2 {
		t.Errorf("bot_token counter = %d, want 2", n)
	}
}

// TestShortEnvValuesAreNotRedacted is AC-4.7, and it is the reason
// minEnvValueLen exists.
//
// Without the floor, MY_KEY=go redacts every "go" in the body: "golang"
// becomes "[redacted]lang", "going" becomes "[redacted]ing", and the comment is
// unreadable. The floor is not a tuning knob, it is what keeps the marker
// meaningful.
func TestShortEnvValuesAreNotRedacted(t *testing.T) {
	t.Parallel()

	r := redact.New(redact.Options{
		Environ: []string{
			"MY_KEY=go",              // 2 chars — must be ignored
			"SHORT_TOKEN=abcdefg",    // 7 chars — one below the floor
			"REAL_SECRET=abcdefgh",   // 8 chars — exactly at the floor
			"LONG_PASSWORD=hunter2s", // 8 chars
		},
	})

	const in = "going to run go build in golang; abcdefg is fine; abcdefgh is not; hunter2s is not"
	body := apply(t, r, in)
	got := body.String()

	for _, keep := range []string{"going", "go build", "golang", "abcdefg is fine"} {
		if !strings.Contains(got, keep) {
			t.Errorf("a short env value caused spurious redaction: %q is missing\n got: %q", keep, got)
		}
	}
	for _, gone := range []string{"abcdefgh is not", "hunter2s"} {
		if strings.Contains(got, gone) {
			t.Errorf("an env value at or above the floor survived: %q\n got: %q", gone, got)
		}
	}
	if n := body.Counts().Get(redact.ClassEnvValue); n != 2 {
		t.Errorf("env_value counter = %d, want 2", n)
	}
}

// TestPrivateKeyBlockIsRedactedWhole checks that the multi-line block goes as a
// unit. Redacting only the BEGIN line would leave the key itself in the
// comment, which is the failure this pattern exists to prevent.
func TestPrivateKeyBlockIsRedactedWhole(t *testing.T) {
	t.Parallel()

	key := strings.Join([]string{
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz",
		"c2gtZWQyNTUxOQAAACDqZ0hV3Xk9RvJ0lQ0Y8sZ9Zx0Q0k9nQ5j8k7l6m5n4o3",
		"-----END OPENSSH PRIVATE KEY-----",
	}, "\n")

	body := apply(t, newRedactor(t, ""),
		"deploy failed, the key was\n"+key+"\nand the run stopped")
	got := body.String()

	for _, line := range strings.Split(key, "\n") {
		if strings.Contains(got, line) {
			t.Errorf("private key line survived: %q\n got: %q", line, got)
		}
	}
	if !strings.Contains(got, "deploy failed, the key was\n[redacted]\nand the run stopped") {
		t.Errorf("surrounding text was not preserved\n got: %q", got)
	}
	if n := body.Counts().Get(redact.ClassPrivateKey); n != 1 {
		t.Errorf("private_key counter = %d, want 1", n)
	}
}

// TestUnterminatedPrivateKeyBlockIsRedacted covers the interaction between the
// two halves of the sequence. Apply truncates before it redacts, so a key block
// can lose its END line on the way through — and a pattern that required END
// would then leave the key material in the body.
func TestUnterminatedPrivateKeyBlockIsRedacted(t *testing.T) {
	t.Parallel()

	const secretLine = "MIIEowIBAAKCAQEAsecretkeymaterialthatmustnotbepostedanywhere"
	in := "log tail\n-----BEGIN PRIVATE KEY-----\n" + secretLine

	body := apply(t, newRedactor(t, ""), in)
	if strings.Contains(body.String(), secretLine) {
		t.Fatalf("unterminated key block survived: %q", body.String())
	}
}

// TestApplyOrdering exercises the truncate → redact → assert → hard-truncate
// sequence, including the case the ordering exists for: redaction shrinking the
// body back under the limit.
func TestApplyOrdering(t *testing.T) {
	t.Parallel()

	t.Run("redaction shrinks the body back under the limit", func(t *testing.T) {
		t.Parallel()

		// A body made almost entirely of AWS key ids: 20 bytes in, 10 out.
		// Sized so it is comfortably under the limit before truncation and
		// still under it after redaction, so nothing should be cut.
		const limit = 4096
		var b strings.Builder
		for i := 0; b.Len() < redact.Budget(limit)-40; i++ {
			fmt.Fprintf(&b, "AKIAIOSFODNN7EXAMPL%d\n", i%10)
		}
		in := b.String()

		r := newRedactor(t, "")
		body, err := r.Apply(t.Context(), in, limit)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if body.Truncated() {
			t.Error("body was truncated even though redaction fits it under the limit")
		}
		if body.Len() >= len(in) {
			t.Errorf("redaction did not shrink the body: %d -> %d", len(in), body.Len())
		}
		if strings.Contains(body.String(), "AKIA") {
			t.Error("an access key id survived")
		}
	})

	t.Run("oversize body is truncated within the limit", func(t *testing.T) {
		t.Parallel()

		const limit = 2048
		in := strings.Repeat("a line of harmless log output\n", 500)

		r := newRedactor(t, "")
		body, err := r.Apply(t.Context(), in, limit)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !body.Truncated() {
			t.Error("Truncated() = false for a body far over the limit")
		}
		if body.Len() > limit {
			t.Errorf("body is %d bytes, over the %d limit", body.Len(), limit)
		}
		// The staging cut aims at Budget, not at limit. That headroom is what
		// keeps the hard-truncate a last resort rather than the usual path,
		// and it is invisible unless something asserts it.
		if body.Len() > redact.Budget(limit) {
			t.Errorf("body is %d bytes; staging should cut to Budget(%d) = %d, leaving the margin free",
				body.Len(), limit, redact.Budget(limit))
		}
		if !strings.Contains(body.String(), "truncated") {
			t.Errorf("no truncation notice in the body: %q", body.String())
		}
	})

	t.Run("an oversize body dense with credentials shrinks well under the limit", func(t *testing.T) {
		t.Parallel()

		// Over the limit going in and, once every 20-byte key id becomes a
		// 10-byte marker, comfortably under it coming out. This is the full
		// sequence in one case: staged cut, then a redaction that shrinks what
		// survived, then the length assert.
		const limit = 4096
		in := strings.Repeat("AKIAIOSFODNN7EXAMPLE ", 600) // ~12 KiB

		body, err := newRedactor(t, "").Apply(t.Context(), in, limit)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !body.Truncated() {
			t.Error("Truncated() = false for a body three times the limit")
		}
		if body.Len() > limit {
			t.Errorf("body is %d bytes, over the %d limit", body.Len(), limit)
		}
		if strings.Contains(body.String(), "AKIA") {
			t.Errorf("an access key id survived: %q", body.String())
		}
		// Redaction ran after the cut, so the result is smaller than the
		// staged size — which is the whole reason the order is not the
		// other way around.
		if body.Len() >= redact.Budget(limit) {
			t.Errorf("body is %d bytes; redaction after a staged cut to %d should have shrunk it",
				body.Len(), redact.Budget(limit))
		}
	})

	t.Run("redaction that grows the body still lands within the limit", func(t *testing.T) {
		t.Parallel()

		// Every match is 8 bytes and becomes 10, so the pass grows the body by
		// 25%. Staging at Budget(limit) is what absorbs that; the assert and
		// the hard-truncate are what catch it when it does not.
		const limit = 4096
		r := redact.New(redact.Options{Environ: []string{"A_SECRET=abcdefgh"}})
		in := strings.Repeat("abcdefgh", limit/8)

		body, err := r.Apply(t.Context(), in, limit)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if body.Len() > limit {
			t.Errorf("body is %d bytes, over the %d limit", body.Len(), limit)
		}
		if strings.Contains(body.String(), "abcdefgh") {
			t.Error("an env value survived")
		}
	})

	t.Run("a credential straddling the size limit is still removed", func(t *testing.T) {
		t.Parallel()

		// The credential sits just under the limit but past the budget, so it
		// is the truncation step that decides its fate. Whatever survives must
		// not be the whole token.
		const limit = 4096
		const token = "ghp_AbCdEf0123456789AbCdEf0123456789"
		in := strings.Repeat("x", redact.Budget(limit)-10) + token + strings.Repeat("y", 200)

		body, err := newRedactor(t, "").Apply(t.Context(), in, limit)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if strings.Contains(body.String(), token) {
			t.Errorf("token straddling the truncation point survived: %q", body.String())
		}
	})

	t.Run("truncation never leaves a fragment of a credential", func(t *testing.T) {
		t.Parallel()

		// The sharp edge of truncate-before-redact. A cut that lands inside a
		// credential leaves a fragment that matches no pattern and equals no
		// literal, so the redaction pass that runs next sees nothing wrong and
		// the fragment goes out in the comment.
		//
		// The bot token is the worst case: it is matched literally, so a
		// fragment of it is invisible to every check this package has. Sweeping
		// the token across the region where the cut falls guarantees the case
		// is actually hit rather than assumed.
		const limit = 8192
		const token = "zzzz-not-a-recognisable-token-shape-9999"

		r := newRedactor(t, token)
		filler := strings.Repeat("filler words here ", 1000)

		for offset := redact.Budget(limit) - 200; offset < redact.Budget(limit); offset += 8 {
			in := filler[:offset] + " " + token + " " + filler[offset:]

			body, err := r.Apply(t.Context(), in, limit)
			if err != nil {
				t.Fatalf("Apply(token at %d): %v", offset, err)
			}
			// Any leading run of the token is a leak; twelve characters is
			// well past coincidence.
			if fragment := token[:12]; strings.Contains(body.String(), fragment) {
				t.Fatalf("a fragment of the bot token survived truncation with the token at offset %d\n"+
					"tail: %q", offset, tail(body.String(), 120))
			}
		}
	})

	t.Run("a non-positive limit is refused", func(t *testing.T) {
		t.Parallel()

		for _, limit := range []int{0, -1} {
			body, err := newRedactor(t, "").Apply(t.Context(), "hello", limit)
			if !errors.Is(err, redact.ErrInvalidLimit) {
				t.Errorf("Apply(limit=%d) error = %v, want ErrInvalidLimit", limit, err)
			}
			if !errors.Is(err, redact.ErrRedactionFailed) {
				t.Errorf("Apply(limit=%d) error does not wrap ErrRedactionFailed", limit)
			}
			if body.Valid() {
				t.Errorf("Apply(limit=%d) returned a valid Body", limit)
			}
		}
	})
}

// TestTruncationKeepsValidUTF8 guards the 422 nobody enjoys diagnosing: a body
// cut mid-rune is not valid UTF-8 and GitHub rejects it.
func TestTruncationKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	// No newlines, so the cut cannot fall back to a line boundary and has to
	// get the rune boundary right on its own.
	in := strings.Repeat("ünïcödé", 4000)

	for limit := 200; limit < 260; limit++ {
		body, err := newRedactor(t, "").Apply(t.Context(), in, limit)
		if err != nil {
			t.Fatalf("Apply(limit=%d): %v", limit, err)
		}
		if body.Len() > limit {
			t.Fatalf("Apply(limit=%d) produced %d bytes", limit, body.Len())
		}
		if !utf8ValidString(body.String()) {
			t.Fatalf("Apply(limit=%d) produced invalid UTF-8: %q", limit, body.String())
		}
	}
}

// TestApplyIsSafeForConcurrentUse pins the property the daemon relies on: one
// Redactor is built at start and used from every tick.
func TestApplyIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	r := redact.New(redact.Options{
		Token:   secrets.NewToken("lf-live-bot-token-value"),
		Environ: []string{"DEPLOY_SECRET=envsecretvalue"},
	})
	const in = "token lf-live-bot-token-value and envsecretvalue and ghp_AbCdEf0123456789AbCdEf0123456789"

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := r.Apply(t.Context(), in, commentLimit)
			if err != nil {
				t.Errorf("Apply: %v", err)
				return
			}
			if strings.Contains(body.String(), "lf-live-bot-token-value") {
				t.Error("the bot token survived")
			}
			if body.Counts().Total() != 3 {
				t.Errorf("Counts().Total() = %d, want 3", body.Counts().Total())
			}
		}()
	}
	wg.Wait()
}

// TestCountsLogValueCarriesClassesOnly is the executable form of "the counter
// logs the class, never the text". Counts is the only thing this package hands
// a logger, and it holds integers keyed by a typed enum, so the matched text
// has no route into daemon.log.
func TestCountsLogValueCarriesClassesOnly(t *testing.T) {
	t.Parallel()

	const token = "lf-live-bot-token-value"
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := redact.New(redact.Options{
		Logger:  log,
		Token:   secrets.NewToken(token),
		Environ: []string{"DEPLOY_SECRET=envsecretvalue"},
	})

	body := apply(t, r, "token "+token+" and envsecretvalue and AKIAIOSFODNN7EXAMPLE")
	if body.Counts().Total() != 3 {
		t.Fatalf("Counts().Total() = %d, want 3", body.Counts().Total())
	}

	logged := buf.String()
	if logged == "" {
		t.Fatal("nothing was logged; the S-7 per-class counter never reached daemon.log")
	}
	for _, leaked := range []string{token, "envsecretvalue", "AKIAIOSFODNN7EXAMPLE"} {
		if strings.Contains(logged, leaked) {
			t.Errorf("the log line contains matched text %q:\n%s", leaked, logged)
		}
	}
	for _, class := range []string{"bot_token", "env_value", "aws_access_key"} {
		if !strings.Contains(logged, class) {
			t.Errorf("the log line does not name the class %q:\n%s", class, logged)
		}
	}
}

// TestZeroRedactorRefusesToRun covers what the type system cannot: a
// redact.Redactor{} literal compiles. It knows no literals, so running it would
// pass the bot token straight through. It must refuse instead.
func TestZeroRedactorRefusesToRun(t *testing.T) {
	t.Parallel()

	var r redact.Redactor
	body, err := r.Apply(t.Context(), "ghp_AbCdEf0123456789AbCdEf0123456789", commentLimit)
	if !errors.Is(err, redact.ErrUninitialized) {
		t.Errorf("zero Redactor.Apply error = %v, want ErrUninitialized", err)
	}
	if body.Valid() || body.String() != "" {
		t.Errorf("zero Redactor.Apply returned a usable Body: %q", body.String())
	}
}

// TestEnvironIsInjectable is the property every other test in this file relies
// on: the environment scanned is a parameter, not a global. Injection is what
// keeps the pattern tests hermetic — a developer with GITHUB_TOKEN exported
// must not change what they assert.
func TestEnvironIsInjectable(t *testing.T) {
	t.Parallel()

	t.Run("an injected environment is scanned", func(t *testing.T) {
		t.Parallel()

		const value = "injected-environment-secret-value"
		r := redact.New(redact.Options{Environ: []string{"SOME_API_KEY=" + value}})
		body := apply(t, r, "the run printed "+value)

		if strings.Contains(body.String(), value) {
			t.Errorf("an injected secret-shaped env value was not redacted: %q", body.String())
		}
	})

	t.Run("an empty environment scans nothing", func(t *testing.T) {
		t.Parallel()

		// Non-nil and empty is the "scan nothing" signal. If it were treated
		// as nil, this body would depend on the developer's own shell.
		r := redact.New(redact.Options{Environ: []string{}})
		body := apply(t, r, "the run printed nothing-secret-here")

		if !strings.Contains(body.String(), "nothing-secret-here") {
			t.Errorf("an empty Environ still redacted something: %q", body.String())
		}
	})
}

// TestNilEnvironScansTheProcessEnvironment pins the default: Options.Environ
// nil means os.Environ, because the daemon's own environment is the most likely
// source of a credential in run output and forgetting to pass it would be a
// silent hole.
//
// This is the one test in the file that cannot be parallel. t.Setenv mutates
// process-global state, and the whole point of the case is that New reads that
// global when nothing is injected; the testing package rejects the combination
// outright. Every other test injects Options.Environ and stays parallel.
func TestNilEnvironScansTheProcessEnvironment(t *testing.T) {
	const value = "process-environment-secret-value"
	t.Setenv("LITEFACTORY_TEST_API_KEY", value)

	r := redact.New(redact.Options{}) // Environ is nil on purpose.
	body := apply(t, r, "the run printed "+value)

	if strings.Contains(body.String(), value) {
		t.Errorf("a secret-shaped process env value was not redacted: %q", body.String())
	}
}

// TestZeroBodyIsNotABypass is the runtime half of the compile-time guarantee.
//
// The compile-time half cannot be written as a test, because the thing it
// asserts is that certain programs do not compile. It is this: Body's fields
// are all unexported and this package exports no Body constructor, so from
// package github the expressions
//
//	redact.Body{text: "post me unredacted"}   // unknown field
//	redact.Body{"post me unredacted"}         // implicit assignment to unexported field
//	redact.NewBody("post me unredacted")      // undefined
//
// are all compile errors. The only Body carrying text in the whole module is
// one returned by Apply. What is left to check at runtime is the escape hatch
// that does compile — `redact.Body{}` — and that it is empty and self-declaring
// rather than a silent bypass.
func TestZeroBodyIsNotABypass(t *testing.T) {
	t.Parallel()

	var zero redact.Body

	if zero.Valid() {
		t.Error("redact.Body{}.Valid() = true; the poster cannot tell it apart from a redacted body")
	}
	if got := zero.String(); got != "" {
		t.Errorf("redact.Body{}.String() = %q, want empty", got)
	}
	if zero.Len() != 0 {
		t.Errorf("redact.Body{}.Len() = %d, want 0", zero.Len())
	}
	if zero.Truncated() {
		t.Error("redact.Body{}.Truncated() = true, want false")
	}
	if n := zero.Counts().Total(); n != 0 {
		t.Errorf("redact.Body{}.Counts().Total() = %d, want 0", n)
	}

	// And a Body that did come from Apply is distinguishable from it, so
	// Valid() is a real signal rather than a constant.
	if !apply(t, newRedactor(t, ""), "hello").Valid() {
		t.Error("a Body from Apply reports Valid() == false")
	}
}

// TestSecretShapedEnvNames pins the S-7 name list. The names select which
// values enter the literal table; getting this wrong is silent, because a
// missing name produces a redactor that simply never fires on that credential.
func TestSecretShapedEnvNames(t *testing.T) {
	t.Parallel()

	// One value per variable, all ≥8 chars so the floor is not what is under
	// test here, and all distinct so a miss is attributable.
	secretish := []string{
		"GITHUB_TOKEN",
		"LF_BOT_TOKEN",
		"CLIENT_SECRET",
		"DB_PASSWORD",
		"PGPASSWD",
		"SSH_KEY",
		"MY_API_KEY",
		"AWS_CREDENTIAL",
		// Lower case, because an environment is not required to shout.
		"service_token",
	}
	harmless := []string{
		"PATH_PREFIX",
		"HOME_DIR",
		"LF_HOME",
		"EDITOR_COMMAND",
	}

	environ := make([]string, 0, len(secretish)+len(harmless))
	values := make(map[string]string, len(secretish)+len(harmless))
	for i, name := range append(append([]string{}, secretish...), harmless...) {
		value := fmt.Sprintf("value-of-%s-%02d", strings.ToLower(name), i)
		values[name] = value
		environ = append(environ, name+"="+value)
	}

	body := apply(t, redact.New(redact.Options{Environ: environ}),
		"dump:\n"+strings.Join(environ, "\n"))
	got := body.String()

	for _, name := range secretish {
		if strings.Contains(got, values[name]) {
			t.Errorf("the value of %s survived; its name matches the S-7 list", name)
		}
	}
	for _, name := range harmless {
		if !strings.Contains(got, values[name]) {
			t.Errorf("the value of %s was redacted; its name is not credential-shaped "+
				"and redacting it makes the comment less readable for nothing", name)
		}
	}
	if n := body.Counts().Get(redact.ClassEnvValue); n != len(secretish) {
		t.Errorf("env_value counter = %d, want %d", n, len(secretish))
	}
}

// TestMalformedEnvironEntriesAreIgnored covers the shapes os.Environ can
// technically hand back. A panic here would be a fail-closed abort of every
// post for the lifetime of the process, so New must not be brittle about it.
func TestMalformedEnvironEntriesAreIgnored(t *testing.T) {
	t.Parallel()

	r := redact.New(redact.Options{Environ: []string{
		"",                            // no name, no value
		"NAKED_TOKEN",                 // no "="
		"EMPTY_SECRET=",               // no value
		"BLANK_PASSWORD=            ", // long enough to clear the floor, but blank
		"=orphan-value-here",          // no name
	}})

	// The body carries a long run of spaces, so a blank env value entering the
	// literal table would show up here rather than passing unnoticed.
	body := apply(t, r, "a body mentioning orphan-value-here            and nothing else")
	if !strings.Contains(body.String(), "orphan-value-here") {
		t.Errorf("a nameless environ entry produced a redaction: %q", body.String())
	}
	if n := body.Counts().Total(); n != 0 {
		t.Errorf("Counts().Total() = %d, want 0", n)
	}
}

// tail returns the last n bytes of s, for failure messages about truncation
// where the interesting part is the end and the rest is filler.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			// The range loop yields U+FFFD for an invalid byte; a literal
			// replacement character in the input would be a false positive,
			// and no test input here contains one.
			return false
		}
	}
	return true
}

// A title-sized limit is cut on one line; GitHub rejects titles with breaks.
func TestApplyShortLimitStaysOnOneLine(t *testing.T) {
	r := redact.New(redact.Options{Environ: []string{}})
	got, err := r.Apply(t.Context(), strings.Repeat("word ", 80), 256)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(got.String(), "\r\n") || got.Len() > 256 || !got.Truncated() || !strings.HasSuffix(got.String(), "…") {
		t.Fatalf("short apply %q (%d bytes, truncated=%t)", got.String(), got.Len(), got.Truncated())
	}
}
