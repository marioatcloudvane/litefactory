package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/localprojects"
)

// configuredLabelAgents lists the runners the factory registers: Claude is
// built in, Codex only when configured.
func configuredLabelAgents(cfg *config.Config) []string {
	agents := []string{"claude"}
	if cfg != nil && cfg.Agents.Codex != nil {
		agents = append(agents, "codex")
	}
	return agents
}

// writeLabelSetupHint prints the one-line pointer to label setup. It runs nothing.
func writeLabelSetupHint(env *Env, id string) {
	fmt.Fprintf(env.Stdout, "hint: run `litefactory repo labels setup %s` to create the factory's labels\n", id)
}

func newRepoLabelsSetupCommand() *Command {
	cmd := &Command{
		Name:  "repo labels setup",
		Usage: "repo labels setup <project-id> [--yes]",
		Short: "Create the factory's missing labels on the project's tracker",
		Long: "Show the labels found and the labels that would be created, then create\n" +
			"only the missing Mode, Coding Agent and bug labels. Existing labels are\n" +
			"never renamed, recoloured, deleted or duplicated. Without a terminal and\n" +
			"without --yes nothing is written. GitHub projects only for now.",
		Args: ArgSpec{Names: []string{"project-id"}, Min: 1},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var yes bool
		fs.BoolVar(&yes, "yes", false, "create the missing labels without asking")
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoLabelsSetup(ctx, env, cmd, args[0], yes)
		}
	}
	return cmd
}

func runRepoLabelsSetup(ctx context.Context, env *Env, cmd *Command, rawID string, yes bool) (err error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return Usagef(cmd, "project-id must be a positive registered repository ID")
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	service, err := localprojects.Open(ctx, rt.db, id, localprojects.Options{Logger: env.Logger, Keychain: env.Keychain, LabelAgents: configuredLabelAgents(rt.cfg)})
	if err != nil {
		return projectCommandError(cmd, err)
	}
	setup, err := service.PlanLabelSetup(ctx)
	if err != nil {
		return projectCommandError(cmd, err)
	}
	out := env.Stdout
	fmt.Fprintf(out, "labels found (%d):\n", len(setup.Found))
	for _, name := range setup.Found {
		fmt.Fprintf(out, "  %s\n", name)
	}
	for _, w := range setup.Plan.Warnings {
		fmt.Fprintf(out, "warning: existing label %q cannot be used by the factory: %s; fix it by hand, setup does not rename labels\n", w.Label, w.Reason)
	}
	if len(setup.Plan.Create) == 0 {
		fmt.Fprintln(out, "nothing to create; no labels written")
		return nil
	}
	fmt.Fprintf(out, "labels to create (%d):\n", len(setup.Plan.Create))
	for _, w := range setup.Plan.Create {
		fmt.Fprintf(out, "  %s\n", w.Name)
	}
	if !yes {
		if !env.InputIsTerminal {
			fmt.Fprintln(out, "nothing written, rerun with --yes")
			return nil
		}
		fmt.Fprint(out, "create these labels? [y/N] ")
		answer, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "nothing written")
			return nil
		}
	}
	result := setup.Apply(ctx)
	for _, name := range result.Created {
		fmt.Fprintf(out, "created %s\n", name)
	}
	if result.Failed == "" {
		return nil
	}
	fmt.Fprintf(out, "failed %s: %s\n", result.Failed, result.FailedWhy)
	for _, name := range result.NotTried {
		fmt.Fprintf(out, "not tried %s\n", name)
	}
	return Preconditionf("label setup stopped at %s; rerun to create what is still missing", result.Failed)
}
