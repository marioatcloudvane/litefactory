package redact_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestPrepareCompleteView(t *testing.T) {
	t.Parallel()
	r := redact.New(redact.Options{Environ: []string{"TEST_SECRET=private-literal"}})
	// The secret is beyond the old whole-file limit and spans a would-be
	// response boundary. Complete preparation must match it before paging.
	prefix := strings.Repeat("ä🙂\r\n", 160000) + strings.Repeat("x", 32763) + " "
	text := prefix + "private-literal" + "\r\nlast line without newline"
	view, err := r.Prepare(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	want := prefix + redact.Replacement + "\r\nlast line without newline"
	if !view.Valid() || view.String() != want {
		t.Fatal("complete preparation lost or changed non-secret evidence")
	}
	digest := sha256.Sum256([]byte(want))
	if view.Digest() != hex.EncodeToString(digest[:]) || view.PolicyID() != r.PolicyID() {
		t.Fatal("view identity does not identify prepared content and policy")
	}
	before := r.PolicyID()
	r.AddToken(secrets.NewToken("another-private-token"))
	if before == r.PolicyID() || view.PolicyID() != before {
		t.Fatal("credential registration failed to invalidate policy or mutated old view")
	}
}

func TestPrepareRefusesIncompleteText(t *testing.T) {
	t.Parallel()
	r := redact.New(redact.Options{Environ: []string{}})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tt := range []struct {
		name string
		ctx  context.Context
		text string
		want error
	}{
		{"cancelled", cancelled, "valid text", context.Canceled},
		{"invalid UTF8", context.Background(), "text\xff", redact.ErrRedactionFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			view, err := r.Prepare(tt.ctx, tt.text)
			if !errors.Is(err, tt.want) || view.Valid() || view.String() != "" {
				t.Fatalf("Prepare returned valid=%v, err=%v", view.Valid(), err)
			}
		})
	}
}
