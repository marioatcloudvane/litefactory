package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/submission"
)

func newMCPServeCommand() *Command {
	return &Command{Name: "mcp serve", Usage: "mcp serve --descriptor <absolute-path>", Short: "Serve the current attempt's tools over stdio", New: func(flags *flag.FlagSet) Handler {
		descriptor := flags.String("descriptor", "", "factory-supplied absolute attempt descriptor")
		return func(ctx context.Context, env *Env, args []string) error {
			if len(args) != 0 || *descriptor == "" {
				return errors.New("mcp serve requires only --descriptor <absolute-path>")
			}
			input, ok := env.Stdin.(io.ReadCloser)
			if !ok {
				return errors.New("mcp serve requires interruptible stdin")
			}
			output, ok := env.Stdout.(io.WriteCloser)
			if !ok {
				return errors.New("mcp serve requires interruptible stdout")
			}
			client := submission.Client{DescriptorPath: *descriptor}
			// Manual serving is independent of the auto-start convenience setting, but
			// it always requires live authority before beginning the MCP handshake.
			result, err := client.List(ctx)
			if err != nil || result.Error != "" {
				return errors.New("mcp serve requires a live authorized descriptor")
			}
			return (mcpadapter.Server{}).Run(ctx, input, output, client)
		}
	}}
}
