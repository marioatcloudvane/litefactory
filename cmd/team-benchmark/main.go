// Command team-benchmark measures catalog source bytes without invoking an agent.
package main

import (
	"context"
	"encoding/json"
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
	source := flag.String("source", "teams/source", "focused catalog source")
	baseline := flag.String("baseline", "teams/testdata/full-catalog.json", "preserved full-catalog fixture")
	name := flag.String("team", "go-team", "team name")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "team-benchmark accepts flags only")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := teampublish.Measure(ctx, *source, *baseline, *name)
	if err == nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(result)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
