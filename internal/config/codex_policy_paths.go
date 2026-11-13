package config

import "path/filepath"

// CodexPolicyDir contains host-owned selected policy homes and session leases.
func (p Paths) CodexPolicyDir() string { return filepath.Join(p.home, "codex-policy") }
