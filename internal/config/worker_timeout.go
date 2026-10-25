package config

import (
	"errors"
	"fmt"
	"strings"
)

// WorkerTimeoutSource identifies how the default attempt allowance was resolved.
func (c *Config) WorkerTimeoutSource() string {
	if c.workerTimeoutSource == "" {
		return "default"
	}
	return c.workerTimeoutSource
}

// WorkerTimeoutDiagnostics returns migration warnings for the operator.
func (c *Config) WorkerTimeoutDiagnostics() []string {
	return append([]string(nil), c.workerTimeoutDiagnostics...)
}

// resolveWorkerTimeout uses the raw document: constructor defaults are never
// evidence that an operator explicitly selected a provider timeout.
func (c *Config) resolveWorkerTimeout(data []byte) error {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil
	}
	root := doc.Content[0]
	explicit := yamlField(yamlField(root, "settings"), "worker_timeout_seconds")
	var faults []error
	if explicit != nil && workerTimeoutScalarTag(explicit) != "!!int" {
		faults = append(faults, fmt.Errorf("settings.worker_timeout_seconds: must be an integer number of seconds"))
	}
	type legacyValue struct {
		path    string
		seconds int
	}
	var legacy []legacyValue
	for _, name := range []string{"claude", "codex"} {
		node := yamlField(yamlField(yamlField(root, "agents"), name), "timeout_seconds")
		if node == nil {
			continue
		}
		path := "agents." + name + ".timeout_seconds"
		if workerTimeoutScalarTag(node) != "!!int" {
			faults = append(faults, fmt.Errorf("%s: must be an integer number of seconds", path))
			continue
		}
		var seconds int
		if err := node.Decode(&seconds); err != nil {
			faults = append(faults, fmt.Errorf("%s: %w", path, err))
			continue
		}
		legacy = append(legacy, legacyValue{path, seconds})
		c.workerTimeoutDiagnostics = append(c.workerTimeoutDiagnostics, path+" is deprecated; use settings.worker_timeout_seconds (node lf_budget overrides it)")
	}
	if len(faults) > 0 {
		return errors.Join(faults...)
	}
	if explicit != nil {
		c.workerTimeoutSource = "settings.worker_timeout_seconds"
		return nil
	}
	if len(legacy) == 0 {
		c.workerTimeoutSource = "default"
		return nil
	}
	for _, value := range legacy[1:] {
		if value.seconds != legacy[0].seconds {
			var values []string
			for _, entry := range legacy {
				values = append(values, fmt.Sprintf("%s=%d", entry.path, entry.seconds))
			}
			return fmt.Errorf("conflicting legacy worker timeouts (%s); set settings.worker_timeout_seconds explicitly to select one default", strings.Join(values, ", "))
		}
	}
	c.Settings.WorkerTimeoutSeconds = legacy[0].seconds
	c.workerTimeoutSource = legacy[0].path
	return nil
}
