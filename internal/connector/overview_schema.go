package connector

// overviewOutputSchema describes the versioned, independently bounded overview
// sections. Source-specific facts remain additive within each section.
func overviewOutputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"version", "generated_at", "window", "health", "activity", "owner_waits", "recent_publications", "coverage", "alerts"},
		"properties": map[string]any{
			"version":      map[string]any{"type": "integer", "const": 1},
			"generated_at": map[string]any{"type": "string", "format": "date-time"},
			"window": map[string]any{"type": "object", "required": []string{"from", "to"}, "properties": map[string]any{
				"from": map[string]any{"type": "string", "format": "date-time"},
				"to":   map[string]any{"type": "string", "format": "date-time"},
			}},
			"alerts": map[string]any{"type": "object", "required": []string{"enabled", "rules", "active", "has_more", "global_mute_until", "delivery_ready", "delivery_reason", "delivery_last_success", "delivery_states"}, "properties": map[string]any{
				"enabled":         map[string]any{"type": []string{"boolean", "null"}},
				"rules":           map[string]any{"type": "array", "maxItems": 6},
				"active":          map[string]any{"type": "array", "maxItems": 50},
				"has_more":        map[string]any{"type": "boolean"},
				"delivery_ready":  map[string]any{"type": "boolean"},
				"delivery_reason": map[string]any{"type": "string"},
				"delivery_states": map[string]any{"type": "object"},
			}},
			"health":              map[string]any{"type": "object", "required": []string{"status"}},
			"activity":            overviewListSchema(),
			"owner_waits":         overviewListSchema(),
			"recent_publications": overviewListSchema(),
			"coverage": map[string]any{"type": "object", "required": []string{"sources", "repositories", "repositories_has_more", "warnings"}, "properties": map[string]any{
				"sources":               map[string]any{"type": "object"},
				"repositories":          map[string]any{"type": "array", "maxItems": 50},
				"repositories_has_more": map[string]any{"type": "boolean"},
				"warnings":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}},
		},
	}
}

func overviewListSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"items", "has_more", "truncated", "warnings"}, "properties": map[string]any{
		"items":     map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object"}},
		"has_more":  map[string]any{"type": "boolean"},
		"truncated": map[string]any{"type": "boolean"},
		"warnings":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}}
}
