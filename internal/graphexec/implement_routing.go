package graphexec

import (
	"errors"
	"slices"
)

func decodeImplementFact(action string, attrs map[string]string) (ResearchCondition, error) {
	c := ResearchCondition{Field: attrs["lf_field"], Condition: attrs["lf_condition"]}
	valid := c.Field == "story.size" && slices.Contains([]string{"S", "M", "L"}, c.Condition) || c.Field == "story.sizing_valid" && slices.Contains([]string{"true", "false"}, c.Condition)
	_, source := attrs["lf_source"]
	_, fallback := attrs["lf_default"]
	if action != ResearchIfAction || !valid || source || fallback {
		return c, errors.New("invalid Implement fact comparison")
	}
	return c, nil
}

// EvaluateImplementFacts compares only the saved fact named by the graph.
// An absent fact never matches, including a comparison against false.
func (c ResearchCondition) EvaluateImplementFacts(facts map[string]string) (string, error) {
	if _, err := decodeImplementFact(ResearchIfAction, map[string]string{"lf_field": c.Field, "lf_condition": c.Condition}); err != nil {
		return "", err
	}
	if value, ok := facts[c.Field]; ok && value == c.Condition {
		return "true", nil
	}
	return "false", nil
}
