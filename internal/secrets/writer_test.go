package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInteractiveAddCommandKeepsTheValueOutOfArgv(t *testing.T) {
	t.Parallel()

	line, err := interactiveAddCommand("litefactory-webhook", "owner/name", "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	want := `add-generic-password -U -s "litefactory-webhook" -a "owner/name" -w "deadbeef"`
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestInteractiveAddCommandQuotesAndRefuses(t *testing.T) {
	t.Parallel()

	line, err := interactiveAddCommand("s", "a", `x"y\z`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(line, `-w "x\"y\\z"`) {
		t.Errorf("line = %q, want escaped quote and backslash", line)
	}
	for _, bad := range [][3]string{{"", "a", "v"}, {"s", "", "v"}, {"s", "a", ""}, {"s", "a", "v\nquit"}, {"s\r", "a", "v"}} {
		if _, err := interactiveAddCommand(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("interactiveAddCommand(%q) = nil error, want refusal", bad)
		}
	}
}

func TestWriterWritesThroughTheSeam(t *testing.T) {
	t.Parallel()

	var service, account, value string
	var deadline bool
	w := Writer{
		KeychainTimeout: time.Minute,
		Keychain: func(ctx context.Context, s, a, v string) error {
			service, account, value = s, a, v
			_, deadline = ctx.Deadline()
			return nil
		},
	}
	if err := w.Write(t.Context(), KeychainRef("litefactory-webhook", "o/r"), "sekret"); err != nil {
		t.Fatal(err)
	}
	if service != "litefactory-webhook" || account != "o/r" || value != "sekret" || !deadline {
		t.Errorf("got %q %q %q deadline=%v", service, account, value, deadline)
	}
}

func TestWriterErrors(t *testing.T) {
	t.Parallel()

	locked := Writer{Keychain: func(context.Context, string, string, string) error { return ErrInteractionNotAllowed }}
	err := locked.Write(t.Context(), KeychainRef("s", "a"), "sekret")
	var we *WriteError
	if !errors.As(err, &we) || !errors.Is(err, ErrInteractionNotAllowed) {
		t.Fatalf("err = %v, want a WriteError wrapping ErrInteractionNotAllowed", err)
	}
	if strings.Contains(err.Error(), "sekret") {
		t.Errorf("error leaks the value: %v", err)
	}

	called := false
	w := Writer{Keychain: func(context.Context, string, string, string) error { called = true; return nil }}
	for _, ref := range []Reference{EnvRef("X"), KeychainRef("", "a"), KeychainRef("s", ""), {}} {
		if err := w.Write(t.Context(), ref, "v"); !errors.Is(err, ErrIncomplete) {
			t.Errorf("Write(%v) = %v, want ErrIncomplete", ref, err)
		}
	}
	if called {
		t.Error("the seam was called for an incomplete reference")
	}
}
