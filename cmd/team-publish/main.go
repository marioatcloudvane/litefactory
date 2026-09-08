// Command team-publish produces web-hostable team content without building the app.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudvane/litefactory/internal/teampublish"
)

func main() {
	syscall.Umask(0o077)
	os.Exit(run())
}

func run() int {
	source := flag.String("source", "teams/source", "single-team source package")
	output := flag.String("output", "teams", "web publication directory")
	name := flag.String("team", "go-team", "team name")
	version := flag.String("version", "", "new exact stable semantic version")
	flag.Parse()
	if flag.NArg() != 0 || *version == "" {
		fmt.Fprintln(os.Stderr, "usage: team-publish --version x.y.z [--source path] [--output path] [--team name]")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := teampublish.Publish(ctx, *source, *output, *name, *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
