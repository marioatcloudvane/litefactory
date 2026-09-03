// Command litefactory watches GitHub issues and runs Claude Code in git
// worktrees.
//
// This file is wiring only: umask, signal context, logger construction,
// dispatch and the exit code. Behaviour belongs in internal packages.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/cloudvane/litefactory/internal/cli"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/logging"
)

// programName is the name used in usage text and diagnostics.
const programName = "litefactory"

// version is the build version reported by --version. Override it at build
// time with -ldflags "-X main.version=$(git describe --tags --always)".
var version = "dev"

func main() {
	// First statement, deliberately. Every directory this process creates is
	// 0700 and every file 0600 as a result: $LF_HOME, the SQLite WAL and SHM
	// sidecars, and logs/ — including the unredacted JSONL event stream and
	// the .meta.json holding the prompt exactly as sent. No code anywhere in
	// litefactory calls Chmod; this one line is why it does not have to.
	syscall.Umask(0o077)

	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := rootContext(context.Background(), os.Stderr)
	defer stop()

	sink := openLogSink(os.Stderr)
	if sink != nil {
		defer func() {
			if err := sink.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", programName, err)
			}
		}()
	}

	// Text records go to stdout only when a human is watching; piping
	// litefactory somewhere must not interleave log lines with results.
	var console io.Writer
	if logging.IsTerminal(os.Stdout) {
		console = os.Stdout
	}

	var file io.Writer
	if sink != nil {
		file = sink
	}
	logger := logging.New(logging.Options{
		Level:   configuredLevel(),
		Console: console,
		File:    file,
	}).With(slog.String("version", version))

	env := &cli.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Stdin:   os.Stdin,
		Logger:  logger,
		Version: version,
	}
	logger.Debug("dispatch", slog.Any("args", args))
	return cli.NewRouter(programName, env, cli.Commands()).Run(ctx, args)
}

// openLogSink builds the daemon.log sink. The sink opens the file lazily, so
// a command that logs nothing creates nothing. If $LF_HOME cannot be
// resolved, litefactory says so loudly and carries on with console logging
// only rather than refusing to run.
func openLogSink(stderr io.Writer) *logging.FileSink {
	paths, err := config.Locate()
	if err != nil {
		fmt.Fprintf(stderr, "%s: logging to file disabled: %v\n", programName, err)
		return nil
	}
	return logging.NewFileSink(paths.DaemonLogPath())
}

// configuredLevel reads log_level from config.yaml, falling back to info.
//
// Every failure here is deliberately silent, and this is the one place that is
// correct. The logger has to exist before any command runs — including init,
// which is the command that creates the configuration in the first place, and
// which cannot be made to depend on the configuration existing. A command that
// actually needs the configuration loads it itself and reports the failure
// properly, with the file name and the offending key.
func configuredLevel() slog.Level {
	paths, err := config.Locate()
	if err != nil {
		return slog.LevelInfo
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return slog.LevelInfo
	}
	level, err := logging.ParseLevel(cfg.Settings.LogLevel)
	if err != nil {
		return slog.LevelInfo
	}
	return level
}

// rootContext creates the process-wide cancellation context. It is the only
// cancellation source in litefactory: everything that can be interrupted
// takes its context from here.
//
// signal.NotifyContext only reacts to the first signal, so a shutdown that
// wedges would otherwise be uninterruptible. A second SIGINT or SIGTERM
// therefore hard-exits.
func rootContext(parent context.Context, stderr io.Writer) (context.Context, func()) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)

	// done lets the returned cancel function retire the watcher goroutine on
	// the normal path, so it cannot outlive run().
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		// Only now start listening for the *next* signal: registering
		// earlier would queue the first one and hard-exit immediately.
		second := make(chan os.Signal, 1)
		signal.Notify(second, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(second)
		select {
		case <-done:
			return
		case <-second:
			fmt.Fprintf(stderr, "\n%s: second signal, exiting immediately\n", programName)
			os.Exit(cli.ExitFailure)
		}
	}()

	return ctx, sync.OnceFunc(func() {
		close(done)
		stop()
	})
}
