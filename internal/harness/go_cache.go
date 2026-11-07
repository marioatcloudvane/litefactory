package harness

import (
	"fmt"
)

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
