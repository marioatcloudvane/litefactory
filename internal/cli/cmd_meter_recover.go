package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/localusage"
	"github.com/cloudvane/litefactory/internal/meterview"
	"strings"
)

func newMeterRecoverCommand() *Command {
	return &Command{Name: "meter recover", Usage: "meter recover <repository> <task-id> --attempt ID [--apply]", Short: "Preview missing historical Codex descendant usage; apply explicitly", Args: ArgSpec{Names: []string{"repository", "task-id"}, Min: 2}, New: func(fs *flag.FlagSet) Handler {
		var attempt string
		var apply bool
		fs.StringVar(&attempt, "attempt", "", "existing finalized attempt ID")
		fs.BoolVar(&apply, "apply", false, "atomically append evidenced missing descendants")
		return func(ctx context.Context, env *Env, args []string) error {
			filter, project, err := meterFilter(args, meterOptions{}, false)
			if err != nil {
				return err
			}
			if len(args) != 2 || attempt == "" || len(attempt) > 256 || strings.ContainsAny(attempt, "\x00\r\n\t") {
				return Usagef(nil, "recovery requires repository, positive task-id and --attempt")
			}
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			output, err := localusage.NewRecovery(paths.DBPath()).Recover(ctx, localusage.RecoveryRequest{Project: project, Issue: filter.Issue, Attempt: attempt, Apply: apply})
			var viewErr *meterview.Error
			if errors.As(err, &viewErr) {
				return Usagef(nil, "%s", viewErr)
			}
			if err != nil {
				return err
			}
			return json.NewEncoder(env.Stdout).Encode(output)
		}
	}}
}
