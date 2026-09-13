package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestMeterRecoveryPreparedApplyAndReplay(t *testing.T) {
	f, _, attempt := meterTestHome(t)
	// Deliberately synthetic current window: authentic historical windows and exact
	// values are covered by store/testdata/meter_recovery_history.json.
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	files := map[string][]any{
		"root": {map[string]any{"timestamp": at, "type": "session_meta", "payload": map[string]any{"id": "root", "cli_version": "0.153.4"}}},
		"child": {
			map[string]any{"timestamp": at, "type": "session_meta", "payload": map[string]any{"id": "child", "parent_thread_id": "root", "cli_version": "0.153.4"}},
			map[string]any{"timestamp": at, "type": "turn_context", "payload": map[string]any{"turn_id": "turn", "model": "gpt-6-astra"}},
			map[string]any{"timestamp": at, "type": "token_usage_record", "payload": map[string]any{"thread_id": "child", "turn_id": "turn", "response_id": "response", "usage": map[string]any{"input_tokens": 100, "cached_input_tokens": 20, "output_tokens": 5, "cache_write_input_tokens": 0}}},
		},
	}
	for id, records := range files {
		path := filepath.Join(dir, "rollout-"+id+".jsonl")
		var raw []byte
		for _, record := range records {
			line, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, line...)
			raw = append(raw, '\n')
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Store terminal timestamps use millisecond precision. Ensure the synthetic
	// response precedes the terminal boundary without relying on disk speed.
	time.Sleep(2 * time.Millisecond)
	if err := f.db.FinishMeterAttempt(t.Context(), attempt.ID, "success", "partial", "0.153.4", "exec-jsonl+rollout-token-usage-record-v1", nil); err != nil {
		t.Fatal(err)
	}
	reader, err := store.OpenMeterReader(t.Context(), config.NewPaths(f.home).DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	prepared, err := prepareMeterRecovery(t.Context(), reader, home, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.unresolved) != 0 || len(prepared.batch.Observations) != 1 || len(prepared.batch.Invocations) != 1 {
		t.Fatalf("prepared %+v", prepared)
	}
	preview, err := reader.RecoverMeterUsage(t.Context(), prepared.batch, false, reconcileRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || preview.AddedObservations != 1 {
		t.Fatalf("preview %+v", preview)
	}
	if err = prepared.evidence.Revalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.RecoverMeterUsage(t.Context(), prepared.batch, true, reconcileRecovery); err != nil {
		t.Fatal(err)
	}
	fresh, err := prepareMeterRecovery(t.Context(), reader, home, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.db.RecoverMeterUsage(t.Context(), fresh.batch, true, reconcileRecovery)
	if err != nil || replay.AddedObservations != 0 {
		t.Fatalf("replay %+v: %v", replay, err)
	}
	for _, apply := range []bool{false, true} {
		args := []string{"meter", "recover", "acme/api", "73", "--attempt", attempt.ID}
		if apply {
			args = append(args, "--apply")
		}
		code, out, diagnostics := f.run(args...)
		if code != 0 {
			t.Fatalf("CLI: %d %s", code, diagnostics)
		}
		var result struct {
			Applied  bool    `json:"applied"`
			Added    int     `json:"added_observations"`
			Estimate *string `json:"estimated_usd"`
		}
		if err = json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if result.Applied != apply || result.Added != 0 || result.Estimate != nil {
			t.Fatalf("CLI replay %s", out)
		}
	}
}

func TestMeterRecoveryCommandRejectsSelectors(t *testing.T) {
	f, _, attempt := meterTestHome(t)
	for _, args := range [][]string{
		{"meter", "recover", "acme/api", "73"},
		{"meter", "recover", "acme/api", "0", "--attempt", attempt.ID, "--apply"},
		{"meter", "recover", "acme/api", "74", "--attempt", attempt.ID, "--apply"},
		{"meter", "recover", "acme/api", "73", "--attempt", "missing", "--apply"},
		{"meter", "recover", "acme/api", "73", "--attempt", attempt.ID, "--apply"},
		{"meter", "recover", "acme/api", "73", "extra", "--attempt", attempt.ID, "--apply"},
	} {
		code, out, _ := f.run(args...)
		if code == 0 || out != "" {
			t.Fatalf("invalid selector accepted: %v: %d %s", args, code, out)
		}
	}
}
