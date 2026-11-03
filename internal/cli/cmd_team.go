package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/localteams"
)

func newTeamCommand(operation string) *Command {
	args := ArgSpec{Names: []string{"name", "version"}, Min: 2}
	switch operation {
	case "clone":
		args = ArgSpec{Names: []string{"name", "version", "new-name", "new-version"}, Min: 4}
	case "install":
		args.Min = 1
	case "update":
		args = ArgSpec{Names: []string{"name"}, Min: 1}
	case "list":
		args = ArgSpec{}
	}
	usage := "team " + operation
	for i, name := range args.Names {
		if i >= args.Min {
			usage += " [<" + name + ">]"
		} else {
			usage += " <" + name + ">"
		}
	}
	if operation != "list" {
		usage += " [--source catalogue-url|local]"
	}
	cmd := &Command{Name: "team " + operation, Usage: usage, Short: "Manage machine-level installed team versions", Args: args}
	cmd.New = func(flags *flag.FlagSet) Handler {
		source := ""
		if operation != "list" {
			flags.StringVar(&source, "source", "", "exact catalogue URL or local namespace for clone/inspect/remove")
		}
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeam(ctx, env, operation, source, args)
		}
	}
	return cmd
}
func validateTeamPackage(ctx context.Context, source fs.FS, workflows map[string]string) error {
	return localteams.ValidatePackage(ctx, source, workflows)
}
func runTeam(ctx context.Context, env *Env, operation, source string, args []string) error {
	manager, err := localteams.New(localteams.Options{Transport: env.TeamTransport})
	if err != nil {
		return err
	}
	switch operation {
	case "clone":
		entry, err := manager.Clone(ctx, source, args[0], args[1], args[2], args[3])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(env.Stdout, "Cloned %s %s\nEditable folder: %s\nOrigin: %s %s %s\n", entry.Identity.Slug, entry.Identity.Version, entry.Path, entry.Origin.Source, entry.Origin.Slug, entry.Origin.Version)
		return err
	case "install":
		ver := ""
		if len(args) == 2 {
			ver = args[1]
		}
		entry, already, err := manager.Install(ctx, source, args[0], ver)
		if err != nil {
			return err
		}
		status := "Installed"
		if already {
			status = "Already installed"
		}
		_, err = fmt.Fprintf(env.Stdout, "%s %s %s from %s\n", status, entry.Identity.Slug, entry.Identity.Version, entry.Identity.Source)
		return err
	case "update":
		entry, updated, err := manager.Update(ctx, source, args[0])
		if err != nil {
			return err
		}
		status := "No update"
		if updated {
			status = "Installed update"
		}
		_, err = fmt.Fprintf(env.Stdout, "%s: %s %s from %s\n", status, entry.Identity.Slug, entry.Identity.Version, entry.Identity.Source)
		return err
	case "list":
		entries, err := manager.List(ctx)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintf(env.Stdout, "%s %s %s\n", entry.Identity.Source, entry.Identity.Slug, entry.Identity.Version); err != nil {
				return err
			}
		}
		return nil
	case "inspect":
		entry, err := manager.Inspect(ctx, source, args[0], args[1])
		if err != nil {
			return err
		}
		if entry.Editable {
			_, err = fmt.Fprintf(env.Stdout, "Team: %s\nVersion: %s\nSource: local, editable\nPath: %s\nOrigin: %s %s %s\nOrigin checksum: %s\nCloned at: %s\n", entry.Identity.Slug, entry.Identity.Version, entry.Path, entry.Origin.Source, entry.Origin.Slug, entry.Origin.Version, entry.Origin.Checksum, entry.Origin.ClonedAt)
			return err
		}
		modes := entry.Workflows
		slices.Sort(modes)
		_, err = fmt.Fprintf(env.Stdout, "Team: %s\nVersion: %s\nSource: %s\nPath: %s\nChecksum: %s\nWorkflows: %s\nIntegrity: verified\n", entry.Identity.Slug, entry.Identity.Version, entry.Identity.Source, entry.Path, entry.Identity.Digest, strings.Join(modes, ", "))
		return err
	case "remove":
		removed, err := manager.Remove(ctx, source, args[0], args[1])
		if removed {
			_, writeErr := fmt.Fprintf(env.Stdout, "Removed %s %s\n", args[0], args[1])
			return errors.Join(err, writeErr)
		}
		return err
	default:
		return errors.New("unknown team operation")
	}
}
