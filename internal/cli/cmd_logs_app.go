package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

// issueOnlyFlags are the flags that only make sense against a specific
// issue's run log. They are rejected, naming themselves, when "litefactory
// logs" is invoked with no issue (T-1.4).
//
// -n and --all are deliberately not listed here: they are the app log's own
// flags, and validateTailFlags (US-9) already rejects them symmetrically on
// the issue path. --raw is deliberately not listed either: raw pass-through
// applies to the app log view too. -f is deliberately not listed either: the
// app log can be followed exactly as an issue's run log can (US-2).
var issueOnlyFlags = []string{"run", "runs", "only", "grep", "i", "invert", "full", "repo"}

// flagToken renders name the way it was spelled on the command line: a
// single dash for single-letter flags, a double dash otherwise, matching how
// this package's own flag.FlagSet is declared.
func flagToken(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// validateFlagApplicability enforces that issue-only flags are not passed
// when "litefactory logs" is invoked with no issue argument (T-1.4, AC-1.4).
//
// flagSet is the same [flag.FlagSet] passed to logsOptions.bind, still in
// scope after flagSet.Parse has run — that is what lets explicitlySet tell
// "the user passed --run 2" apart from "--run merely holds its default".
func validateFlagApplicability(cmd *Command, flagSet *flag.FlagSet, hasIssue bool) error {
	if hasIssue {
		return nil
	}
	for _, name := range issueOnlyFlags {
		if explicitlySet(flagSet, name) {
			token := flagToken(name)
			return Usagef(cmd, "%s only applies to an issue's log; drop %s", token, token)
		}
	}
	return nil
}

// issueArgError reports that arg is not a usable issue number or task ref,
// naming both what was expected and the bare-invocation alternative (T-1.3,
// AC-1.3): a reader who typed "logs daemon" needs to learn not just that
// "daemon" is not a number, but that dropping it entirely is itself a valid,
// different command.
func issueArgError(cmd *Command, arg string) error {
	return Usagef(cmd, "issue must be a positive number, got %q "+
		"(litefactory logs with no issue shows litefactory's own log)", arg)
}

// runAppLog prints litefactory's own log, daemon.log, when "litefactory
// logs" is invoked with no issue argument (US-1), optionally following it
// live when -f/--follow is set (US-2).
//
// Unlike runLogs, this opens no database and requires no prior "litefactory
// init" (AC-1.2): daemon.log is the only file read, and its path comes
// exclusively from config.Paths.DaemonLogPath().
func runAppLog(ctx context.Context, env *Env, opts logsOptions) error {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	path := paths.DaemonLogPath()

	offset, err := appLogTailOffset(path, opts)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var sink logSink
	if opts.raw {
		sink = &rawSink{out: env.Stdout}
	} else {
		sink = newSlogTextSink(env.Stdout)
	}

	// #nosec G304 -- path is litefactory's own artefact path, obtained from
	// config.Paths.DaemonLogPath(); never user input.
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if !opts.follow {
			// A daemon that has never run yet is a state, not a failure
			// (AC-1.5): there is nothing to show, not an error to report.
			fmt.Fprintf(env.Stdout, "no litefactory log yet at %s\n", path)
			return nil
		}
		// -f was asked for before daemon.log has ever been written. Wait
		// for it to appear rather than failing (AC-2.3, T-2.4).
		return followAppLog(ctx, sink, path, 0)
	}

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return fmt.Errorf("seek %s: %w", path, err)
		}
	}

	reader := bufio.NewReaderSize(f, followReadSize)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := sink.write(line); writeErr != nil {
				f.Close()
				return writeErr
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			f.Close()
			return fmt.Errorf("read %s: %w", path, readErr)
		}
	}

	// pos is where the initial pass stopped reading: bufio.Reader only ever
	// advances the underlying file forward, so the file's current position
	// is exactly the byte offset the follow loop below should resume from.
	pos, posErr := f.Seek(0, io.SeekCurrent)
	closeErr := f.Close()
	if posErr != nil {
		return fmt.Errorf("seek %s: %w", path, posErr)
	}
	if closeErr != nil {
		return closeErr
	}

	if !opts.follow {
		return nil
	}
	return followAppLog(ctx, sink, path, pos)
}

// followAppLog polls path for new content after the initial pass in
// runAppLog has already rendered everything up to offset, feeding each new
// line to sink as it appears (US-2, T-2.1, T-2.2).
//
// Unlike tailLog, which follows a single issue's run log and assumes that
// file is append-only and never rotated, followAppLog owns a stat-based
// truncation check of its own: daemon.log is a live file the daemon itself
// may rotate or clear out from under a reader. Before every read, it
// compares the file's current size against offset, the position it last
// read up to; a smaller size means the file was truncated or replaced, so
// the loop closes its handle and reopens at offset 0 on the next tick
// (AC-2.4), rather than seeking past EOF forever or erroring. A file that
// does not exist yet — whether -f was invoked before daemon.log was ever
// written, or the file was rotated away entirely between ticks — is waited
// for rather than treated as an error (AC-2.3).
//
// The app log has no terminal event of its own (US-3): sink.write's done
// return is never consulted here, so a record whose rendered text happens
// to read msg=result cannot be mistaken for one and cannot stop the loop
// (T-2.3, AC-2.2). The only way out is ctx being cancelled, at which point
// followAppLog returns nil without flushing a trailing partial line — a
// half-written record stays unprinted rather than being emitted as if it
// were complete, matching tailLog's own choice on the issue-log path.
//
// There is no goroutine here, exactly as in tailLog: the loop reads, then
// waits on a ticker and ctx together, on the same goroutine already running
// this command (AC-2.5).
func followAppLog(ctx context.Context, sink logSink, path string, offset int64) (err error) {
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()

	var (
		f       *os.File
		pending []byte
	)
	defer func() {
		if f == nil {
			return
		}
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	buf := make([]byte, followReadSize)
	for {
		if f == nil {
			// #nosec G304 -- path is litefactory's own artefact path,
			// obtained from config.Paths.DaemonLogPath(); never user input.
			opened, openErr := os.Open(path)
			switch {
			case openErr == nil:
				f = opened
				if offset > 0 {
					if _, err := f.Seek(offset, io.SeekStart); err != nil {
						return fmt.Errorf("seek %s: %w", path, err)
					}
				}
			case errors.Is(openErr, fs.ErrNotExist):
				// Not there yet, or rotated away entirely between ticks.
				// Wait for it to (re)appear rather than failing.
			default:
				return openErr
			}
		}

		if f != nil {
			info, statErr := f.Stat()
			if statErr != nil {
				return fmt.Errorf("stat %s: %w", path, statErr)
			}
			if info.Size() < offset {
				// The file shrank under us: rotated or truncated. Drop the
				// handle and start over from the top of what is there now
				// on the next tick (AC-2.4).
				closeErr := f.Close()
				f = nil
				offset = 0
				pending = nil
				if closeErr != nil {
					return closeErr
				}
			} else {
				stop, drainErr := drainAppLog(ctx, f, sink, buf, &pending, &offset)
				if drainErr != nil {
					return drainErr
				}
				if stop {
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			// Interrupted mid-line: the partial stays unprinted rather than
			// being emitted as a truncated record.
			return nil
		case <-ticker.C:
		}
	}
}

// drainAppLog feeds every complete line that has appeared in f since the
// last call to sink, advancing *offset by however many bytes were read so
// the next tick's truncation check in followAppLog compares against where
// this reader actually is. It reports stop only when ctx is cancelled: the
// app log has no terminal event (T-2.3), so unlike drainLog this never
// stops on sink.write's done.
func drainAppLog(ctx context.Context, f io.Reader, sink logSink, buf []byte, pending *[]byte, offset *int64) (stop bool, err error) {
	for {
		// A writer that keeps up with the reader would otherwise hold this
		// loop past a Ctrl-C, because the select on ctx is outside it.
		if ctx.Err() != nil {
			//nolint:nilerr // Deliberate: interrupting a follow is how one
			// ends, not a failure, mirroring drainLog's own choice.
			return true, nil
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			*offset += int64(n)
			*pending = append(*pending, buf[:n]...)
			if err := emitAppLogLines(sink, pending); err != nil {
				return false, err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return false, nil
			}
			return false, fmt.Errorf("read app log: %w", readErr)
		}
	}
}

// emitAppLogLines feeds each newline-terminated line in pending to sink and
// leaves any trailing partial line behind for the next read.
//
// Unlike emitLines, this never stops on sink.write's done: the app log has
// no terminal event of its own (US-3), so a record whose rendered text
// happens to read msg=result must not be mistaken for one and must not stop
// the follow loop (T-2.3, AC-2.2).
func emitAppLogLines(sink logSink, pending *[]byte) error {
	rest := *pending
	defer func() { *pending = rest }()

	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			return nil
		}
		line := rest[:i+1]
		rest = rest[i+1:]
		if _, err := sink.write(line); err != nil {
			return err
		}
	}
}
