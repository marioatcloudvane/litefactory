package harness

import (
	"encoding/json"
	"fmt"
	"os"
)

// prepareMainCacheSettings preserves the operator's sandbox policy and adds only
// the selected compiler-cache data directory to its existing write grants.
func (r *Runner) prepareMainCacheSettings(worktree, logPath string) (string, error) {
	if err := r.goBuildCaches.Revalidate(worktree); err != nil {
		return "", err
	}
	// #nosec G304 -- validated operator-owned settings path.
	raw, err := os.ReadFile(r.settingsPath)
	if err != nil {
		return "", fmt.Errorf("read claude cache settings: %w", err)
	}
	document := map[string]any{}
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", err
	}
	if err := addCacheWriteGrant(document, r.goBuildCaches.Main); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	path := logPath + ".settings.json"
	if err := writeFileAtomic(path, encoded, settingsFileMode); err != nil {
		return "", err
	}
	return path, nil
}

func addCacheWriteGrant(document map[string]any, cache string) error {
	sandbox, err := settingsObject(document, "sandbox")
	if err != nil {
		return err
	}
	filesystem, err := settingsObject(sandbox, "filesystem")
	if err != nil {
		return err
	}
	var writes []any
	if current, exists := filesystem["allowWrite"]; exists {
		var ok bool
		writes, ok = current.([]any)
		if !ok {
			return fmt.Errorf("sandbox filesystem allowWrite must be an array")
		}
		for _, write := range writes {
			if _, ok := write.(string); !ok {
				return fmt.Errorf("sandbox filesystem allowWrite entries must be strings")
			}
		}
	}
	filesystem["allowWrite"] = append(writes, cache)
	return nil
}

func settingsObject(parent map[string]any, key string) (map[string]any, error) {
	if parent == nil {
		return nil, fmt.Errorf("settings must be a JSON object")
	}

	if value, exists := parent[key]; exists {
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return nil, fmt.Errorf("settings %s must be an object", key)
		}
		return object, nil
	}
	object := map[string]any{}
	parent[key] = object
	return object, nil
}
