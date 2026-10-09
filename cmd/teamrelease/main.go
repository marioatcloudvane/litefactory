// Command teamrelease packages the canonical Go development team for installation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/cloudvane/litefactory/internal/teamrelease"
)

func main() { os.Exit(run()) }

func run() int {
	check := flag.Bool("check", false, "report generated drift without writing files")
	repo := flag.String("repo", ".", "repository root")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := teamrelease.Generate(ctx, *repo, *check); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
