package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factorylib"
)

// newFactoryInstallCommand declares "litefactory factory install --source <dir>":
// install the factory library (teams/source/factory) as one unit.
func newFactoryInstallCommand() *Command {
	cmd := &Command{
		Name:  "factory install",
		Usage: "factory install --source <dir>",
		Short: "Install the factory library of factory agents",
		Long: "Validate a factory library folder (teams/source/factory in the litefactory\n" +
			"repository) against the factory's registry and install it into\n" +
			"<home>/factory/<digest>/. New work uses it from its next capture; work in\n" +
			"flight keeps the bytes it captured. The daemon needs an installed library.",
	}
	cmd.New = func(flags *flag.FlagSet) Handler {
		source := ""
		flags.StringVar(&source, "source", "", "factory library folder, e.g. teams/source/factory")
		return func(ctx context.Context, env *Env, _ []string) error {
			if source == "" {
				return Usagef(cmd, "--source is required")
			}
			paths, err := config.Locate()
			if err != nil {
				return errors.New("cannot resolve the selected factory home")
			}
			lib, already, err := factorylib.Install(ctx, paths, source)
			if err != nil {
				return err
			}
			status := "Installed"
			if already {
				status = "Already installed"
			}
			_, err = fmt.Fprintf(env.Stdout, "%s factory library %s\n", status, lib.Digest())
			return err
		}
	}
	return cmd
}
