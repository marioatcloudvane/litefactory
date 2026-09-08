package harness

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryArgsUseIsolatedPluginAndFileBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	req := agent.Request{Delivery: &agent.Delivery{Root: root, Roles: []string{"reviewer"}}, Permissions: agent.Permissions{}}
	runner := &Runner{}
	args := strings.Join(runner.args(req), " ")
	for _, want := range []string{"--setting-sources  ", "--plugin-dir " + filepath.Join(root, "plugin"), "--append-system-prompt-file " + filepath.Join(root, "bootstrap.md")} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, "SPECIALIST_BODY") {
		t.Fatal("specialist prompt leaked into argv")
	}
}

func TestDeliveryMetaPersistsEvidenceWithoutBundle(t *testing.T) {
	t.Parallel()
	req := metaRequest("/supplied/worktree")
	req.Delivery = &agent.Delivery{Root: "/removed/bundle", Identity: "effective-identity", Roles: []string{"reviewer"}, Evidence: agent.DeliveryEvidence{Team: "go-team", Mode: "validate", SourceURL: "https://example.com/teams.json", Version: "1.2.3", ManifestDigest: "manifest-digest", SnapshotDigest: "snapshot-digest", SourceBytes: 123, Specialists: []agent.SpecialistEvidence{{ID: "reviewer", Tier: "balanced", Model: "local-model", SourceHash: "agent-hash"}}, ResourceHashes: map[string]string{"skills/review/SKILL.md": "skill-hash"}}}
	meta := NewMeta(req, "ordinary prompt", nil)
	req.Delivery.Evidence.ResourceHashes["skills/review/SKILL.md"] = "mutated"
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"effective-identity", "go-team", "validate", "https://example.com/teams.json", "1.2.3", "manifest-digest", "snapshot-digest", "123", "reviewer", "local-model", "agent-hash", "skill-hash"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("durable provenance missing %s", want)
		}
	}
	if strings.Contains(string(data), "mutated") {
		t.Fatal("persisted provenance aliases caller request")
	}
}
