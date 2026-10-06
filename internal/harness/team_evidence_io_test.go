package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoricalTeamDeliveryMetadataRemainsReadable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "0001.meta.json")
	raw := []byte(`{"run_ordinal":1,"delivery":{"root":"/retired/bundle","identity":"retained","roles":["reviewer"],"evidence":{"team":"external-team","mode":"validate","source_url":"https://example.com/index.json","source_alias":"corp","version":"1.2.3","manifest_digest":"manifest","snapshot_digest":"snapshot","source_bytes":123,"specialists":[{"id":"reviewer","override":true}],"resource_hashes":{"review.md":"digest"}}}}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta Meta
	if err := json.Unmarshal(stored, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Delivery == nil || meta.Delivery.Evidence.Team != "external-team" || !meta.Delivery.Evidence.Specialists[0].Override {
		t.Fatalf("lost historical delivery: %+v", meta.Delivery)
	}
	displayed, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"external-team", "https://example.com/index.json", "corp", "1.2.3", "manifest", "snapshot", "review.md", "digest"} {
		if !strings.Contains(string(displayed), value) {
			t.Errorf("historical display lost %q", value)
		}
	}
}
