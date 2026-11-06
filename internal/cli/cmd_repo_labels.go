package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/localprojects"
	"github.com/cloudvane/litefactory/internal/projectconnection"
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
			"without --yes nothing is written. On Linear the found and created label\n" +
			"IDs are stored on the ticket connection.",
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
	if setup.Linear != nil {
		return runLinearLabelSetup(ctx, env, setup, yes)
	}
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
	if !confirmLabelSetup(env, yes, "create these labels? [y/N] ") {
		return nil
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

// confirmLabelSetup applies story-003's gate: --yes, or a terminal and a y answer.
func confirmLabelSetup(env *Env, yes bool, question string) bool {
	if yes {
		return true
	}
	out := env.Stdout
	if !env.InputIsTerminal {
		fmt.Fprintln(out, "nothing written, rerun with --yes")
		return false
	}
	fmt.Fprint(out, question)
	answer, _ := bufio.NewReader(env.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Fprintln(out, "nothing written")
		return false
	}
	return true
}

func runLinearLabelSetup(ctx context.Context, env *Env, setup *localprojects.LabelSetup, yes bool) error {
	out := env.Stdout
	l := setup.Linear
	fmt.Fprintf(out, "Linear labels found in the team and workspace: %d\n", l.Found)
	writeLinearLabelPlan(out, l.Plan)
	if l.Invalid != "" {
		fmt.Fprintf(out, "blocking: the connection setup would save is refused: %s\n", l.Invalid)
	}
	if !l.Applicable() {
		fmt.Fprintln(out, "nothing written: fix the blocking findings above, then rerun")
		return Preconditionf("label setup cannot apply this plan; nothing was changed")
	}
	if !l.Plan.Changed {
		fmt.Fprintln(out, "nothing to create or store; no labels written and no connection saved")
		return nil
	}
	if !confirmLabelSetup(env, yes, "create these labels and store their IDs on the connection? [y/N] ") {
		return nil
	}
	result := setup.Apply(ctx)
	for _, name := range result.AppearedAdopted {
		fmt.Fprintf(out, "appeared since the dry run, adopted instead of created: %s\n", name)
	}
	if result.Refused != "" {
		if result.Replanned != nil {
			fmt.Fprintln(out, "plan after reading Linear again:")
			writeLinearLabelPlan(out, *result.Replanned)
		}
		return Preconditionf("label setup refused: %s", result.Refused)
	}
	for _, c := range result.LinearCreated {
		fmt.Fprintf(out, "created %s id=%s\n", c.Name, c.ID)
	}
	if result.Failed != "" {
		fmt.Fprintf(out, "failed %s: %s\n", result.Failed, result.FailedWhy)
		for _, name := range result.NotTried {
			fmt.Fprintf(out, "not tried %s\n", name)
		}
		fmt.Fprintln(out, "nothing saved on the connection")
		return Preconditionf("label setup stopped at %s; rerun to adopt what was created and finish", result.Failed)
	}
	if result.SaveRefused != "" {
		return Preconditionf("label setup did not save: %s", result.SaveRefused)
	}
	if result.NothingToSave {
		fmt.Fprintln(out, "nothing to create or store; no connection saved")
		return nil
	}
	fmt.Fprintf(out, "saved label IDs on the connection, revision %d\n", result.SavedRevision)
	return nil
}

func writeLinearLabelPlan(out io.Writer, plan projectconnection.LinearLabelPlan) {
	for _, e := range plan.Entries {
		switch {
		case e.Chosen != nil:
			fmt.Fprintf(out, "  %s: %s, %s %s, %q, id %s\n", e.Wanted.Name, e.Status, e.Chosen.Scope, e.Chosen.Form, e.Chosen.ReadName(), e.Chosen.ID)
		case e.Create != nil:
			where := "flat in the team"
			switch {
			case e.Create.ParentID != "":
				where = "in the team's existing group, id " + e.Create.ParentID
			case e.Create.ParentPlaceholder != "":
				where = "in a new team group"
			}
			fmt.Fprintf(out, "  %s: %s, team %s, %s\n", e.Wanted.Name, e.Status, e.Create.Form, where)
		default:
			fmt.Fprintf(out, "  %s: %s\n", e.Wanted.Name, e.Status)
		}
		for _, c := range e.Candidates {
			fmt.Fprintf(out, "    candidate: %s %s, %q, id %s\n", c.Scope, c.Form, c.ReadName(), c.ID)
		}
		for _, c := range e.Mismatch {
			fmt.Fprintf(out, "    also matches by name, not used (stored ID wins): %s %s, %q, id %s\n", c.Scope, c.Form, c.ReadName(), c.ID)
		}
		if e.Severity != "" {
			fmt.Fprintf(out, "    %s: %s\n", e.Severity, e.Reason)
		}
	}
	for _, c := range plan.Creates {
		if c.IsGroup {
			fmt.Fprintf(out, "  group %q: would create in the team\n", c.Name)
		}
	}
}
