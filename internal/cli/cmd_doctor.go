package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/cloudvane/litefactory/internal/factory/maintenance"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// newDoctorCommand declares "litefactory doctor <task-ref>": the state of the
// task's Doctor. The Doctor itself starts from the task's issue (`/doctor`) or
// a chat client; this command only reads.
func newDoctorCommand() *Command {
	cmd := &Command{
		Name:  "doctor",
		Usage: "doctor <task-ref>",
		Short: "Show the state of a task's Doctor",
		Long: "Show whether the Doctor ran on a task and where it stands.\n\n" +
			"Start the Doctor by commenting `/doctor [what to look at]` on the task's\n" +
			"issue, or from a chat client. It starts only while the task is stopped.",
		Args: ArgSpec{Names: []string{"task-ref"}, Min: 1},
	}
	cmd.New = func(*flag.FlagSet) Handler {
		return func(ctx context.Context, env *Env, args []string) error {
			return runDoctorStatus(ctx, env, cmd, args[0])
		}
	}
	return cmd
}

// newDoctorStopCommand declares "litefactory doctor stop <task-ref>": stop the
// task's running invocation so the task is stopped and the Doctor may start.
func newDoctorStopCommand() *Command {
	cmd := &Command{
		Name:  "doctor stop",
		Usage: "doctor stop <task-ref>",
		Short: "Stop a task's running work so the Doctor can look at it",
		Long: "Ask the daemon to stop the task's running invocation. Its work so far is\n" +
			"saved as usual. Once nothing of the task runs, `/doctor` can start.",
		Args: ArgSpec{Names: []string{"task-ref"}, Min: 1},
	}
	cmd.New = func(*flag.FlagSet) Handler {
		return func(ctx context.Context, env *Env, args []string) (err error) {
			ref, err := naming.ParseRef(args[0])
			if err != nil {
				return Usagef(cmd, "%q is not a task ref such as github:owner/repo#12: %v", args[0], err)
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			runID, err := maintenance.New(rt.db).StopTask(ctx, ref.String())
			if err != nil {
				return err
			}
			if runID == 0 {
				_, err = fmt.Fprintf(env.Stdout, "%s has no running run; nothing to stop.\n", ref)
				return err
			}
			_, err = fmt.Fprintf(env.Stdout, "Stop requested for run %d of %s; the daemon cancels it on its next tick and saves its work.\n", runID, ref)
			return err
		}
	}
	return cmd
}

func runDoctorStatus(ctx context.Context, env *Env, cmd *Command, arg string) (err error) {
	ref, err := naming.ParseRef(arg)
	if err != nil {
		return Usagef(cmd, "%q is not a task ref such as github:owner/repo#12: %v", arg, err)
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	return renderDoctorStatus(ctx, env.Stdout, rt.db, ref.String())
}

func renderDoctorStatus(ctx context.Context, w io.Writer, db *store.DB, task string) error {
	state, err := maintenance.New(db).DoctorState(ctx, task)
	if err != nil {
		return err
	}
	if !state.Known {
		_, err = fmt.Fprintf(w, "%s has no factory work; there is nothing for the Doctor to look at.\n", task)
		return err
	}
	stopped := "stopped: the Doctor may start"
	if !state.Stopped {
		stopped = "busy: work is in flight; stop it with `litefactory doctor stop " + task + "` or wait"
	}
	if _, err := fmt.Fprintf(w, "%s is %s.\n", task, stopped); err != nil {
		return err
	}
	if !state.Ran {
		_, err = fmt.Fprintln(w, "The Doctor has not run on this task. Comment `/doctor` on its issue to start it.")
		return err
	}
	if _, err := fmt.Fprintf(w, "Doctor instance %d: %s at %s, started %s.\n", state.InstanceID, state.Status, state.Node, state.StartedAt); err != nil {
		return err
	}
	for _, a := range state.Attempts {
		line := fmt.Sprintf("  %s attempt %d: %s", a.Node, a.Attempt, a.Status)
		if a.Outcome != "" {
			line += " (" + a.Outcome + ")"
		}
		if a.RunID != 0 {
			line += fmt.Sprintf(", run %d", a.RunID)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
