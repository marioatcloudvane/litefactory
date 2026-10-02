package webhookreg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

func TestSystemKeychainUsesTheSeamsAndKeepsSentinels(t *testing.T) {
	t.Parallel()

	items := map[string]string{}
	k := webhookreg.SystemKeychain{
		Resolver: secrets.Resolver{Keychain: func(_ context.Context, s, a string) (string, error) {
			v, ok := items[s+"/"+a]
			if !ok {
				return "", secrets.ErrNotFound
			}
			return v, nil
		}},
		Writer: secrets.Writer{Keychain: func(_ context.Context, s, a, v string) error {
			items[s+"/"+a] = v
			return nil
		}},
	}
	if _, err := k.Read(t.Context(), "svc", "o/r"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("read missing = %v, want ErrNotFound", err)
	}
	if err := k.Write(t.Context(), "svc", "o/r", "v"); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Read(t.Context(), "svc", "o/r"); err != nil || got != "v" {
		t.Fatalf("read = %q, %v", got, err)
	}
}
