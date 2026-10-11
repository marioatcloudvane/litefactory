package redact_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestConnectionTokensRegisterDuringPublication(t *testing.T) {
	redactor := redact.New(redact.Options{Environ: []string{}})
	var workers sync.WaitGroup
	for index := range 20 {
		workers.Go(func() {
			token := fmt.Sprintf("opaque-connection-secret-%02d", index)
			redactor.AddToken(secrets.NewToken(token))
			body, err := redactor.Apply(t.Context(), "output "+token, 1000)
			if err != nil || strings.Contains(body.String(), token) {
				t.Errorf("connection credential leaked: %v", err)
			}
		})
	}
	workers.Wait()
	tokens := make([]string, 20)
	for index := range tokens {
		tokens[index] = fmt.Sprintf("opaque-connection-secret-%02d", index)
	}
	body, err := redactor.Apply(t.Context(), strings.Join(tokens, " "), 2000)
	if err != nil || strings.Contains(body.String(), "opaque-connection") {
		t.Errorf("registered credential survived: %v", err)
	}
}
