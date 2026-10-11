package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// newRepoDeliveryCommand retains a verified code-owner request for daemon recovery.
func newRepoDeliveryCommand() *Command {
	cmd := &Command{Name: "repo delivery request", Usage: "repo delivery request <parent-task> --generation N --request-id ID --credential env:NAME|keychain:SERVICE:ACCOUNT", Short: "Request delivery as the verified GitHub code owner", Args: ArgSpec{Names: []string{"parent-task"}, Min: 1}}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var generation int
		var requestID, credential string
		fs.IntVar(&generation, "generation", 0, "approved feature generation")
		fs.StringVar(&requestID, "request-id", "", "stable request identity; reuse it after an interrupted request")
		fs.StringVar(&credential, "credential", "", "external owner credential reference")
		return func(ctx context.Context, env *Env, args []string) (err error) {
			if generation <= 0 || requestID == "" {
				return Usagef(cmd, "--generation and --request-id are required")
			}
			parts := strings.Split(credential, ":")
			var ref secrets.Reference
			switch {
			case len(parts) == 2 && parts[0] == "env" && parts[1] != "":
				ref = secrets.EnvRef(parts[1])
			case len(parts) == 3 && parts[0] == "keychain" && parts[1] != "" && parts[2] != "":
				ref = secrets.KeychainRef(parts[1], parts[2])
			default:
				return Usagef(cmd, "--credential must reference env:NAME or keychain:SERVICE:ACCOUNT")
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			source, err := daemon.RequestCodeDelivery(ctx, daemon.DeliveryRequestOptions{DB: rt.db, Resolver: secrets.Resolver{Keychain: env.Keychain}, Credential: ref, Parent: args[0], Generation: generation, RequestID: requestID})
			if err != nil {
				return Precondition(err)
			}
			_, err = fmt.Fprintf(env.Stdout, "Delivery request %s retained for generation %d; processing completed=%t.\n", source.ID, source.Generation, source.Completed)
			return err
		}
	}
	return cmd
}
