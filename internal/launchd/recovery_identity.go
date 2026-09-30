package launchd

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// VerifyRecoveryIdentity requires the loaded job, not merely the on-disk plist,
// to name the expected factory executable and installation. Unrecognized print
// output refuses recovery; it never relaxes to label or PID-only matching.
func (m *Manager) VerifyRecoveryIdentity(ctx context.Context, executable, home string) error {
	out, err := m.run(ctx, "print", m.ServiceTarget())
	if err != nil {
		return err
	}
	var program, installation, section string
	var args []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "\t\t") {
			value := strings.TrimSpace(line)
			switch section {
			case "arguments":
				args = append(args, value)
			case "environment":
				if key, val, ok := strings.Cut(value, " => "); ok && key == "LF_HOME" {
					if installation != "" {
						return errors.New("duplicate loaded factory home")
					}
					installation = val
				}
			}
			continue
		}
		section = ""
		value, ok := strings.CutPrefix(line, "\t")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(value, "program = "):
			if program != "" {
				return errors.New("duplicate loaded program")
			}
			program = strings.TrimPrefix(value, "program = ")
		case value == "arguments = {":
			section = "arguments"
		case value == "environment = {":
			section = "environment"
		}
	}
	if program != executable || installation != home || (len(args) != 3 && len(args) != 5) {
		return errors.New("loaded service recovery identity unavailable or different")
	}
	if args[0] != executable || args[1] != "daemon" || args[2] != "run" {
		return errors.New("loaded service recovery arguments differ")
	}
	if len(args) == 5 && (args[3] != "--level" || !slices.Contains([]string{"debug", "info", "warn", "error"}, args[4])) {
		return errors.New("loaded service recovery flags differ")
	}
	return nil
}
