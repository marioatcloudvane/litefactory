package turn

import (
	"errors"
	"slices"

	"github.com/cloudvane/litefactory/internal/config"
)

// Pinned recipes keep command and phase authority. Only the same admitted safe
// environment supplied to providers is added to their environment references.
func pinnedStackCommand(profile *config.ResolvedStackProfile, command config.BugCheckCommand) (config.BugCheckCommand, error) {
	if profile == nil {
		return command, nil
	}
	if err := profile.ValidateRetained(); err != nil {
		return config.BugCheckCommand{}, errors.New("stack.environment: retained configuration is invalid")
	}
	command.Env = slices.Clone(command.Env)
	for _, entry := range profile.Effective.Environment {
		if !slices.Contains(command.Env, entry.Name) {
			command.Env = append(command.Env, entry.Name)
		}
	}
	if len(command.Env) > 64 {
		return config.BugCheckCommand{}, errors.New("stack.environment: pinned command environment exceeds 64 entries")
	}
	return command, nil
}
