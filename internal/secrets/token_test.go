package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// secret is a token-shaped value. Every assertion below is "this string does
// not appear", so it is deliberately distinctive.
const secret = "ghp_0123456789abcdefghijklmnopqrstuvwx"

// TestTokenNeverFormatsItsValue is AC-0.11, taken literally: a token must not
// appear under %v, %s or slog.Any. The verbs beyond those three are here
// because fmt.Stringer alone would not have covered them — %d on a struct
// prints its fields, and that is exactly how a token leaks.
func TestTokenNeverFormatsItsValue(t *testing.T) {
	token := NewToken(secret)

	formats := []string{"%v", "%s", "%q", "%d", "%x", "%#v", "%+v", "%20s", "%-20v"}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			got := fmt.Sprintf(format, token)
			if strings.Contains(got, secret) {
				t.Fatalf("fmt.Sprintf(%q, token) = %q, which contains the token", format, got)
			}
			if !strings.Contains(got, Redacted) {
				t.Errorf("fmt.Sprintf(%q, token) = %q, want it to contain %q", format, got, Redacted)
			}
		})
	}

	// A pointer receiver goes down a different path inside fmt.
	if got := fmt.Sprintf("%v", &token); strings.Contains(got, secret) {
		t.Errorf("printing *Token leaked: %q", got)
	}
	if got := token.String(); got != Redacted {
		t.Errorf("String() = %q, want %q", got, Redacted)
	}
	if got := token.GoString(); got != Redacted {
		t.Errorf("GoString() = %q, want %q", got, Redacted)
	}
}

// TestTokenNeverFormatsWhenNested covers the realistic leak: nobody prints a
// bare token, they print the struct that happens to hold one.
func TestTokenNeverFormatsWhenNested(t *testing.T) {
	type client struct {
		BaseURL string
		Token   Token
	}
	type request struct {
		Client  client
		Retries int
	}

	values := map[string]any{
		"struct":            client{BaseURL: "https://api.github.com", Token: NewToken(secret)},
		"nested struct":     request{Client: client{Token: NewToken(secret)}, Retries: 3},
		"pointer to struct": &client{Token: NewToken(secret)},
		"slice":             []Token{NewToken(secret)},
		"map value":         map[string]Token{"bot": NewToken(secret)},
		"any":               any(NewToken(secret)),
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, value); strings.Contains(got, secret) {
					t.Errorf("fmt.Sprintf(%q, %s) leaked the token: %q", format, name, got)
				}
			}
		})
	}
}

// TestTokenNeverReachesTheLog is AC-0.11 against the real logger, both
// handlers, at every level — because the criterion says "any log line, at any
// level".
func TestTokenNeverReachesTheLog(t *testing.T) {
	type run struct {
		Task  string
		Token Token
	}
	token := NewToken(secret)

	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"json": func(buf *bytes.Buffer) slog.Handler {
			return slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
		},
		"text": func(buf *bytes.Buffer) slog.Handler {
			return slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
		},
	}

	for name, newHandler := range handlers {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(newHandler(&buf))

			for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				logger.Log(t.Context(), level, "attr", slog.Any("token", token))
				logger.Log(t.Context(), level, "pointer", slog.Any("token", &token))
				logger.Log(t.Context(), level, "group", slog.Group("github", slog.Any("token", token)))
				logger.Log(t.Context(), level, "nested", slog.Any("run", run{Task: "github:o/r#1", Token: token}))
				logger.Log(t.Context(), level, "interpolated", slog.String("msg", fmt.Sprintf("using %v", token)))
				logger.With(slog.Any("token", token)).Log(t.Context(), level, "with")
			}

			if strings.Contains(buf.String(), secret) {
				t.Fatalf("the token reached the %s log:\n%s", name, buf.String())
			}
			if !strings.Contains(buf.String(), Redacted) {
				t.Errorf("the %s log contains no %q marker, so the assertion above proves nothing:\n%s", name, Redacted, buf.String())
			}
		})
	}
}

// TestTokenNeverSerialises covers the JSON paths: the JSON log handler, and
// any metadata structure that happens to carry a token.
func TestTokenNeverSerialises(t *testing.T) {
	type meta struct {
		Task  string `json:"task"`
		Token Token  `json:"token"`
	}

	encoded, err := json.Marshal(meta{Task: "github:o/r#1", Token: NewToken(secret)})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("json.Marshal leaked the token: %s", encoded)
	}
	if !strings.Contains(string(encoded), Redacted) {
		t.Errorf("json.Marshal = %s, want it to contain %q", encoded, Redacted)
	}

	text, err := NewToken(secret).MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(text) != Redacted {
		t.Errorf("MarshalText() = %q, want %q", text, Redacted)
	}
}

// TestTokenLogValue pins the slog.LogValuer contract directly, since that is
// what slog.Any resolves through.
func TestTokenLogValue(t *testing.T) {
	value := NewToken(secret).LogValue()
	if value.Kind() != slog.KindString {
		t.Errorf("LogValue().Kind() = %v, want %v", value.Kind(), slog.KindString)
	}
	if value.String() != Redacted {
		t.Errorf("LogValue() = %q, want %q", value.String(), Redacted)
	}
}

// TestRevealIsTheOnlyWayOut: the value has to be reachable, deliberately and
// greppably, or the type would be useless.
func TestRevealReturnsTheValue(t *testing.T) {
	if got := NewToken(secret).Reveal(); got != secret {
		t.Errorf("Reveal() = %q, want the original value", got)
	}
}

func TestTokenEmpty(t *testing.T) {
	if !NewToken("").Empty() {
		t.Error("Empty() = false for an empty token")
	}
	if NewToken(secret).Empty() {
		t.Error("Empty() = true for a token with a value")
	}
	// The zero value must also be safe to print.
	var zero Token
	if got := fmt.Sprintf("%v", zero); got != Redacted {
		t.Errorf("printing the zero Token = %q, want %q", got, Redacted)
	}
}
