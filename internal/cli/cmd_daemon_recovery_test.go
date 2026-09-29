package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestDoctorDaemonRecoveryRequiresCompleteIdentityBeforeSetup(t *testing.T) {
	for _, args := range [][]string{{"daemon", "run", "--doctor-repair", "operation"}, {"daemon", "run", "--doctor-token", "token"}} {
		var out, errors bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &errors, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler)}
		code := NewRouter("litefactory", env, []*Command{newDaemonRunCommand()}).Run(t.Context(), args)
		if code == 0 || !strings.Contains(errors.String(), "requires both operation identity and token") {
			t.Fatalf("incomplete recovery started setup: code=%d output=%s", code, errors.String())
		}
	}
}
