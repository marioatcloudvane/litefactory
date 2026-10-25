// Command teamrelease packages the selected canonical teams for installation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/cloudvane/litefactory/internal/teamrelease"
)

type sourceFlags []string

func (s *sourceFlags) String() string         { return fmt.Sprint([]string(*s)) }
func (s *sourceFlags) Set(value string) error { *s = append(*s, value); return nil }

func main() { os.Exit(run()) }

func run() int {
	check := flag.Bool("check", false, "report generated drift without writing files")
	repo := flag.String("repo", ".", "repository root")
	var sources sourceFlags
	flag.Var(&sources, "source", "repository-relative canonical source directory; repeat for multiple teams")
	flag.Parse()
	if len(sources) == 0 {
		sources = append(sources, "teams/source/go-development")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := teamrelease.GenerateSources(ctx, *repo, *check, sources); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
