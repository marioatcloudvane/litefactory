package bugchecks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/cloudvane/litefactory/internal/config"
)

// WithStackProfile adds admitted environment values without changing the operator's
// executable, network or filesystem authority.
func (p Isolation) WithStackProfile(profile *config.ResolvedStackProfile) (Isolation, error) {
	if profile == nil {
		return p, nil
	}
	if err := profile.ValidateRetained(); err != nil {
		return Isolation{}, errors.New("stack-profile: retained configuration is invalid")
	}
	p.Environment = maps.Clone(p.Environment)
	if p.Environment == nil {
		p.Environment = map[string]string{}
	}
	for _, entry := range profile.Effective.Environment {
		if err := config.ValidateStackEnvironment(entry); err != nil {
			return Isolation{}, errors.New("stack-profile.environment: invalid setting")
		}
		p.Environment[entry.Name] = entry.Value
	}
	return p, nil
}

// ObserveStackCommands validates every declared recipe without starting a process.
func (p Isolation) ObserveStackCommands(worktree string, profile *config.ResolvedStackProfile) error {
	isolation, err := p.WithStackProfile(profile)
	if err != nil {
		return err
	}
	if profile == nil {
		return nil
	}
	commands := append(slices.Clone(profile.Effective.Setup), profile.Effective.Checks...)
	if len(commands) == 0 {
		return nil
	}
	if err := isolation.Validate(); err != nil {
		return errors.New("stack-profile.commands: operator isolation unavailable")
	}
	for i, command := range commands {
		c := stackCommandEnvironment(command.BugCheckCommand, profile)
		if _, err := isolation.ObserveInputs(worktree, c); err != nil {
			return fmt.Errorf("stack-profile.commands[%d]: command inputs unavailable or outside admitted paths", i)
		}
	}
	return nil
}

func stackCommandEnvironment(command config.BugCheckCommand, profile *config.ResolvedStackProfile) config.BugCheckCommand {
	command.Env = slices.Clone(command.Env)
	for _, entry := range profile.Effective.Environment {
		if !slices.Contains(command.Env, entry.Name) {
			command.Env = append(command.Env, entry.Name)
		}
	}
	return command
}

// RunStackCommands executes an admitted recipe through the existing contained
// command runner. Every command is checked before the first command starts.
// The observation callback receives both started and completed process evidence.
func (p Isolation) RunStackCommands(ctx context.Context, worktree string, profile *config.ResolvedStackProfile, phase string, started func(CommandResult) error) ([]CommandResult, error) {
	if profile == nil {
		return nil, nil
	}
	isolation, err := p.WithStackProfile(profile)
	if err != nil {
		return nil, err
	}
	commands := profile.Effective.Setup
	if phase == "checks" {
		commands = profile.Effective.Checks
	} else if phase != "setup" {
		return nil, errors.New("stack-profile: unsupported command phase")
	}
	if len(commands) == 0 {
		return nil, nil
	}
	if err := isolation.Validate(); err != nil {
		return nil, fmt.Errorf("stack-profile.%s: operator isolation unavailable", phase)
	}
	return runStackCommands(ctx, worktree, profile, phase, started, isolation)
}

// stackCommandExecutor separates orchestration from the host process boundary.
// Production always supplies the already validated operator Isolation value.
type stackCommandExecutor interface {
	ObserveInputs(string, config.BugCheckCommand) (string, error)
	Run(context.Context, string, config.BugCheckCommand, func(CommandResult) error) CommandResult
}

func runStackCommands(ctx context.Context, worktree string, profile *config.ResolvedStackProfile, phase string, started func(CommandResult) error, executor stackCommandExecutor) ([]CommandResult, error) {
	if profile == nil {
		return nil, nil
	}
	if err := profile.ValidateRetained(); err != nil {
		return nil, errors.New("stack-profile: retained configuration is invalid")
	}
	commands := profile.Effective.Setup
	if phase == "checks" {
		commands = profile.Effective.Checks
	} else if phase != "setup" {
		return nil, errors.New("stack-profile: unsupported command phase")
	}
	if executor == nil {
		return nil, errors.New("stack-profile: command executor unavailable")
	}
	if len(commands) > 0 && started == nil {
		return nil, errors.New("stack-profile: command observation callback unavailable")
	}
	prepared := make([]config.BugCheckCommand, len(commands))
	for i, command := range commands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prepared[i] = stackCommandEnvironment(command.BugCheckCommand, profile)
		if _, err := executor.ObserveInputs(worktree, prepared[i]); err != nil {
			return nil, fmt.Errorf("stack-profile.%s[%d]: command inputs unavailable or outside admitted paths", phase, i)
		}
	}
	results := make([]CommandResult, 0, len(prepared))
	for i, command := range prepared {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		result := executor.Run(ctx, worktree, command, started)
		if ctx.Err() != nil && result.Outcome == "passed" {
			result.Outcome = "cancelled"
			result.Reason = ctx.Err().Error()
		}
		results = append(results, result)
		if started != nil {
			if err := started(result); err != nil {
				return results, errors.New("stack-profile: command evidence could not be retained")
			}
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if result.Outcome != "passed" {
			return results, fmt.Errorf("stack-profile.%s[%d]: command %s", phase, i, result.Outcome)
		}
	}
	return results, nil
}
