package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReferenceFrom(t *testing.T) {
	tests := []struct {
		name                     string
		service, account, envVar string
		want                     Reference
		wantOK                   bool
	}{
		{
			name:    "keychain",
			service: "litefactory", account: "github-bot",
			want:   KeychainRef("litefactory", "github-bot"),
			wantOK: true,
		},
		{
			name:   "env",
			envVar: "LITEFACTORY_GITHUB_TOKEN",
			want:   EnvRef("LITEFACTORY_GITHUB_TOKEN"),
			wantOK: true,
		},
		{
			name: "nothing configured",
		},
		{
			// Config validation rejects this combination before it gets here;
			// the tie-break follows the S-5 order rather than guessing.
			name:    "both configured falls back to the S-5 order",
			service: "litefactory", account: "github-bot", envVar: "LITEFACTORY_GITHUB_TOKEN",
			want:   KeychainRef("litefactory", "github-bot"),
			wantOK: true,
		},
		{
			// A half-configured keychain reference must still produce a
			// reference, so that Resolve can name what is missing instead of
			// silently reporting "no credential configured".
			name:    "half-configured keychain",
			service: "litefactory",
			want:    KeychainRef("litefactory", ""),
			wantOK:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReferenceFrom(tc.service, tc.account, tc.envVar)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("Reference = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestResolveFromKeychain(t *testing.T) {
	var gotService, gotAccount string
	resolver := Resolver{
		Keychain: func(_ context.Context, service, account string) (string, error) {
			gotService, gotAccount = service, account
			return secret, nil
		},
	}

	token, err := resolver.Resolve(t.Context(), KeychainRef("litefactory", "github-bot"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if token.Reveal() != secret {
		t.Error("Resolve returned the wrong value")
	}
	if gotService != "litefactory" || gotAccount != "github-bot" {
		t.Errorf("keychain lookup used service %q account %q, want %q and %q",
			gotService, gotAccount, "litefactory", "github-bot")
	}
}

func TestResolveFromEnv(t *testing.T) {
	resolver := Resolver{
		LookupEnv: func(name string) (string, bool) {
			if name != "LITEFACTORY_GITHUB_TOKEN" {
				return "", false
			}
			return secret, true
		},
	}

	token, err := resolver.Resolve(t.Context(), EnvRef("LITEFACTORY_GITHUB_TOKEN"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if token.Reveal() != secret {
		t.Error("Resolve returned the wrong value")
	}
}

// TestResolveFailuresNameTheReference is the S-5 requirement that turns a
// daemon that will not start into a daemon that says why: the message has to
// name the reference it tried.
func TestResolveFailuresNameTheReference(t *testing.T) {
	tests := []struct {
		name      string
		resolver  Resolver
		ref       Reference
		wantCause error
		wantText  []string
	}{
		{
			name:      "nothing configured",
			ref:       Reference{},
			wantCause: ErrNotConfigured,
			wantText:  []string{"github.token_keychain", "github.token_env"},
		},
		{
			name: "keychain item missing",
			resolver: Resolver{Keychain: func(context.Context, string, string) (string, error) {
				return "", ErrNotFound
			}},
			ref:       KeychainRef("litefactory", "github-bot"),
			wantCause: ErrNotFound,
			wantText:  []string{"keychain item", "litefactory", "github-bot"},
		},
		{
			name: "keychain item empty",
			resolver: Resolver{Keychain: func(context.Context, string, string) (string, error) {
				return "", nil
			}},
			ref:       KeychainRef("litefactory", "github-bot"),
			wantCause: ErrEmpty,
			wantText:  []string{"keychain item", "litefactory", "github-bot"},
		},
		{
			name:      "keychain reference half configured",
			ref:       KeychainRef("litefactory", ""),
			wantCause: ErrIncomplete,
			wantText:  []string{"keychain item", "litefactory"},
		},
		{
			name:      "environment variable unset",
			resolver:  Resolver{LookupEnv: func(string) (string, bool) { return "", false }},
			ref:       EnvRef("LITEFACTORY_GITHUB_TOKEN"),
			wantCause: ErrNotFound,
			wantText:  []string{"environment variable $LITEFACTORY_GITHUB_TOKEN"},
		},
		{
			name:      "environment variable empty",
			resolver:  Resolver{LookupEnv: func(string) (string, bool) { return "", true }},
			ref:       EnvRef("LITEFACTORY_GITHUB_TOKEN"),
			wantCause: ErrEmpty,
			wantText:  []string{"environment variable $LITEFACTORY_GITHUB_TOKEN"},
		},
		{
			name:      "environment variable unnamed",
			ref:       Reference{Source: SourceEnv},
			wantCause: ErrIncomplete,
			wantText:  []string{"environment variable"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, err := tc.resolver.Resolve(t.Context(), tc.ref)
			if err == nil {
				t.Fatal("Resolve succeeded, want a failure")
			}
			if !token.Empty() {
				t.Error("Resolve returned a token alongside an error")
			}
			if !errors.Is(err, tc.wantCause) {
				t.Errorf("errors.Is(err, %v) = false (err = %v)", tc.wantCause, err)
			}
			var resolveErr *ResolveError
			if !errors.As(err, &resolveErr) {
				t.Fatalf("errors.As(err, *ResolveError) = false (err = %v)", err)
			}
			for _, want := range tc.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestResolveErrorNeverCarriesTheValue: the failure message is printed on a
// terminal and written to daemon.log, so it must stay free of credential
// material even when the lookup itself misbehaves.
func TestResolveErrorNeverCarriesTheValue(t *testing.T) {
	resolver := Resolver{
		Keychain: func(context.Context, string, string) (string, error) {
			// A lookup that fails *and* hands back a value is exactly the
			// case where a careless implementation would echo it.
			return secret, errors.New("security exited 51")
		},
	}

	_, err := resolver.Resolve(t.Context(), KeychainRef("litefactory", "github-bot"))
	if err == nil {
		t.Fatal("Resolve succeeded, want a failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error message carries the credential: %q", err)
	}
}

// TestResolveBoundsTheKeychainLookup covers the documented wedge risk: under
// launchd, macOS can raise an allow-access dialog nobody can see. The daemon
// has to fail rather than hang there forever.
func TestResolveBoundsTheKeychainLookup(t *testing.T) {
	resolver := Resolver{
		KeychainTimeout: 10 * time.Millisecond,
		Keychain: func(ctx context.Context, _, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(t.Context(), KeychainRef("litefactory", "github-bot"))
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve did not give up on a keychain lookup that never returns")
	}
}

// TestResolveCancelsWithTheCaller keeps the keychain lookup on the one
// cancellation path the process has, so SIGTERM during startup still exits.
func TestResolveCancelsWithTheCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	resolver := Resolver{
		Keychain: func(ctx context.Context, _, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(ctx, KeychainRef("litefactory", "github-bot"))
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve ignored its caller's cancellation")
	}
}

func TestReferenceString(t *testing.T) {
	tests := map[Reference]string{
		KeychainRef("litefactory", "github-bot"): `keychain item (service "litefactory", account "github-bot")`,
		EnvRef("LITEFACTORY_GITHUB_TOKEN"):       "environment variable $LITEFACTORY_GITHUB_TOKEN",
		{}:                                       "no configured token reference",
	}
	for ref, want := range tests {
		if got := ref.String(); got != want {
			t.Errorf("Reference%+v.String() = %q, want %q", ref, got, want)
		}
	}
}
