package codex

import (
	"fmt"
	"strings"
)

const denySupportedVersion = "0.160.0"

func validateDenyVersion(output string) error {
	if strings.TrimSpace(output) != "codex-cli "+denySupportedVersion {
		return fmt.Errorf("selected restrictions require Codex %s; installed runtime capabilities are not supported", denySupportedVersion)
	}
	return nil
}
