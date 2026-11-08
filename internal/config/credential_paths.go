package config

import "path/filepath"

// CredentialPaths names credential-bearing locations hidden from every agent
// shell. Provider-private credential homes are added by each launcher.
func (p Paths) CredentialPaths(operatorHome string) []string {
	paths := []string{p.ConfigPath(), p.SettingsPath(), p.ClaudeConfigDir(), p.SubmissionDir(), filepath.Join(p.home, "secrets")}
	for _, name := range []string{".ssh", ".claude", ".codex", ".config/gh", ".config/claude", ".config/codex", ".git-credentials", ".config/git/credentials", ".netrc", ".aws", ".gnupg", ".docker/config.json", ".kube", ".config/gcloud", ".azure", "Library/Keychains", "Library/LaunchAgents"} {
		paths = append(paths, filepath.Join(operatorHome, name))
	}
	return paths
}
