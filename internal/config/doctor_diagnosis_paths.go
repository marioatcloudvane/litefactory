package config

import "path/filepath"

// DoctorCredentialPaths names credential-bearing locations hidden from the
// diagnosis shell. Provider-private credential homes are added by each launcher.
func (p Paths) DoctorCredentialPaths(operatorHome string) []string {
	paths := []string{p.ConfigPath(), p.SettingsPath(), p.ClaudeConfigDir(), p.SubmissionDir(), p.DoctorControlDir(), filepath.Join(p.home, "secrets")}
	for _, name := range []string{".ssh", ".claude", ".codex", ".config/gh", ".config/claude", ".config/codex", ".git-credentials", ".netrc", "Library/Keychains", "Library/LaunchAgents"} {
		paths = append(paths, filepath.Join(operatorHome, name))
	}
	return paths
}
