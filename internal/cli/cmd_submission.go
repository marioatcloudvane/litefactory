package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/cloudvane/litefactory/internal/submission"
)

func newSubmissionCommand(method string) *Command {
	return &Command{Name: "submission " + method, Usage: "submission " + method + " --descriptor <absolute-path>", Short: "Call the current attempt's local feedback broker", New: func(flags *flag.FlagSet) Handler {
		descriptor := flags.String("descriptor", "", "factory-supplied absolute attempt descriptor")
		return func(ctx context.Context, env *Env, _ []string) error {
			payload, err := io.ReadAll(io.LimitReader(env.Stdin, submission.MaxFrameBytes+1))
			if err != nil {
				return errors.New("submission: cannot read JSON input")
			}
			response, err := submission.Call(ctx, *descriptor, method, payload)
			if err != nil {
				response.Error = err.Error()
			}
			if outputErr := json.NewEncoder(env.Stdout).Encode(response); outputErr != nil {
				return outputErr
			}
			return err
		}
	}}
}
