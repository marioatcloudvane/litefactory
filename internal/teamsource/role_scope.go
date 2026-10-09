package teamsource

import (
	"bytes"
	"errors"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// TaskOwnerRoles reads explicit owner declarations from captured release resources.
// Submitted Plan content and arbitrary filenames cannot create owner authority.
func TaskOwnerRoles(definition *WorkflowDefinition) ([]string, error) {
	if definition == nil {
		return nil, errors.New("task owners: missing admitted definition")
	}
	roles := []string{}
	for name, raw := range definition.Files {
		if !strings.HasSuffix(name, ".md") || !bytes.HasPrefix(raw, []byte("---\n")) {
			continue
		}
		header, _, ok := strings.Cut(string(raw[4:]), "\n---\n")
		if !ok {
			if bytes.Contains(raw, []byte("task_owner:")) {
				return nil, errors.New("task owners: unclosed retained owner declaration")
			}
			continue
		}
		if !strings.Contains(header, "task_owner:") {
			continue
		}
		var declaration struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
			TaskOwner   bool   `yaml:"task_owner"`
		}
		decoder := yaml.NewDecoder(strings.NewReader(header))
		decoder.KnownFields(true)
		if err := decoder.Decode(&declaration); err != nil {
			return nil, errors.New("task owners: invalid retained owner declaration")
		}
		if !declaration.TaskOwner {
			continue
		}
		if declaration.Name == "" || declaration.Name != strings.TrimSuffix(path.Base(name), ".md") || strings.TrimSpace(declaration.Description) == "" {
			return nil, errors.New("task owners: declaration identity does not match retained resource")
		}
		for _, r := range declaration.Name {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return nil, errors.New("task owners: invalid role identity")
			}
		}
		if !slices.Contains(roles, declaration.Name) {
			roles = append(roles, declaration.Name)
		}
	}
	slices.Sort(roles)
	return roles, nil
}
