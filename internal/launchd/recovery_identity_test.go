package launchd

import (
	"context"
	"strings"
	"testing"
)

type doctorIdentityRunner string

func (r doctorIdentityRunner) Run(context.Context, ...string) (string, error) { return string(r), nil }
func TestDoctorLoadedRecoveryIdentityRefusesMissingOrForeignFields(t *testing.T) {
	good := "\tprogram = /factory\n\targuments = {\n\t\t/factory\n\t\tdaemon\n\t\trun\n\t}\n\tenvironment = {\n\t\tLF_HOME => /private/factory\n\t}\n"
	for _, test := range []struct {
		name, text string
		valid      bool
	}{
		{"matching", good, true},
		{"pid-only", "\tpid = 123\n", false},
		{"foreign-program", strings.ReplaceAll(good, "/factory\n", "/other\n"), false},
		{"foreign-home", strings.ReplaceAll(good, "/private/factory", "/other"), false},
		{"wrong-command", strings.ReplaceAll(good, "\t\trun\n", "\t\tstart\n"), false},
		{"duplicate-home", strings.ReplaceAll(good, "LF_HOME => /private/factory", "LF_HOME => /private/factory\n\t\tLF_HOME => /private/factory"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := Manager{Runner: doctorIdentityRunner(test.text)}
			err := manager.VerifyRecoveryIdentity(t.Context(), "/factory", "/private/factory")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
}
