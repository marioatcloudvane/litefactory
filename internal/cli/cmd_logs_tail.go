package cli

import (
	"errors"
	"flag"
	"io"
	"io/fs"
	"os"

	"github.com/cloudvane/litefactory/internal/logging"
)

// defaultTailLines is how many records the app log view shows when neither
// -n nor --all is given (AC-9.1).
const defaultTailLines = 200

// tailScanChunkSize is how many bytes tailOffset reads per backward step
// while it hunts for the Nth-from-last newline. It is sized like
// followReadSize: comfortably larger than a handful of log lines, so a
// typical tail resolves in one or two reads even though daemon.log is
// unrotated and can grow arbitrarily large (AC-9.4).
const tailScanChunkSize = 64 << 10

// validateTailFlags enforces the -n/--all rules for the app log's tail
// window (US-9).
//
// The two flags apply only to the app log view: hasIssue is true on the
// per-issue run log path, where a run's log is always shown in full and
// either flag being passed is a usage error naming the offending flag. On
// the app log path the flags are mutually exclusive, and -n must be at
// least 1.
//
// fs is the same [flag.FlagSet] passed to logsOptions.bind, still in scope
// after fs.Parse has run — that is what lets explicitlySet tell "the user
// passed -n 200" apart from "-n merely holds its default of 200".
func validateTailFlags(cmd *Command, fs *flag.FlagSet, opts logsOptions, hasIssue bool) error {
	nSet := explicitlySet(fs, "n")
	allSet := explicitlySet(fs, "all")
	if hasIssue {
		switch {
		case nSet:
			return Usagef(cmd, "-n only applies to the app log view; drop -n")
		case allSet:
			return Usagef(cmd, "--all only applies to the app log view; drop --all")
		}
		return nil
	}
	if nSet && allSet {
		return Usagef(cmd, "-n and --all are mutually exclusive; drop one")
	}
	if opts.tailN < 1 {
		return Usagef(cmd, "-n takes a line count of 1 or more, got %d", opts.tailN)
	}
	return nil
}

// validateLevelFlag enforces the --level rules for the app log view (US-5,
// AC-5.4).
//
// --level only applies to the app log view, exactly like -n and --all:
// hasIssue is true on the per-issue run log path, where passing --level is a
// usage error naming the flag, mirroring validateTailFlags's treatment of -n
// and --all. On the app log path, a --level value that was explicitly set
// must parse via logging.ParseLevel; a value outside that vocabulary is a
// usage error that names the vocabulary explicitly (AC-5.2) rather than
// propagating logging.ParseLevel's own wrapped error text.
func validateLevelFlag(cmd *Command, fs *flag.FlagSet, opts logsOptions, hasIssue bool) error {
	if !explicitlySet(fs, "level") {
		return nil
	}
	if hasIssue {
		return Usagef(cmd, "--level only applies to the app log view; drop --level")
	}
	if _, err := logging.ParseLevel(opts.level); err != nil {
		return Usagef(cmd, "--level must be one of debug, info, warn, error, got %q", opts.level)
	}
	return nil
}

// explicitlySet reports whether the flag named name was passed on the
// command line, as opposed to merely holding its default value.
func explicitlySet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// appLogTailOffset resolves the byte offset the app log view should start
// reading from.
//
// --all starts at the beginning of the file (T-9.3); otherwise the offset is
// the start of the last opts.tailN lines on disk, computed before any
// --level-style filtering is applied — the tail window is over lines on
// disk, not over lines that survive a later filter.
//
// The returned offset is a plain byte position into path: a later follow
// loop (US-2) can open path itself and seek there, rather than this
// function owning the open file handle or the streaming that comes after
// it (T-9.4).
func appLogTailOffset(path string, opts logsOptions) (int64, error) {
	if opts.all {
		return 0, nil
	}
	return tailOffset(path, opts.tailN)
}

// tailOffset returns the byte offset at which the last n lines of the file
// at path begin. It scans backward from the end of the file in fixed-size
// chunks, counting newlines, rather than reading the file forward from the
// start to count lines — so a months-old, unrotated daemon.log is never
// loaded into memory to find its tail (AC-9.4).
//
// A file with fewer than n lines starts at offset 0; this is not an error.
// A file that does not exist resolves to offset 0 and a nil error, and
// opening it here never creates the file or its parent directory — the
// tail computation is read-only by construction (os.Open never creates).
func tailOffset(path string, n int) (int64, error) {
	// #nosec G304 -- path is litefactory's own artefact path, obtained by
	// the caller from config.Paths.DaemonLogPath(); never user input.
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size == 0 || n < 1 {
		return 0, nil
	}

	trailing, err := endsWithNewline(f, size)
	if err != nil {
		return 0, err
	}
	// crossing is how many newline bytes the backward scan must pass
	// before the remaining, unconsumed prefix holds exactly n lines. A
	// file ending in a newline has one newline per line, including the
	// last, so that final byte has to be crossed too, along with one per
	// earlier line: n+1 in all. A file whose last line is unterminated (a
	// partial write in progress) has no such trailing newline, so that
	// last line is already "open" and only n-1 further newlines need
	// crossing to reach the start of the window: n in all.
	crossing := n
	if trailing {
		crossing = n + 1
	}

	buf := make([]byte, tailScanChunkSize)
	pos := size
	crossed := 0
	for pos > 0 {
		chunkLen := int64(len(buf))
		if chunkLen > pos {
			chunkLen = pos
		}
		start := pos - chunkLen
		chunk := buf[:chunkLen]
		if _, err := f.ReadAt(chunk, start); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		for i := int(chunkLen) - 1; i >= 0; i-- {
			if chunk[i] == '\n' {
				crossed++
				if crossed == crossing {
					return start + int64(i) + 1, nil
				}
			}
		}
		pos = start
	}
	// Fewer than n lines in the whole file: start at the top.
	return 0, nil
}

// endsWithNewline reports whether the last byte of the already-open file f
// is a newline. size is the caller's already-known file size, so the one
// Stat call in tailOffset is the only one needed.
func endsWithNewline(f *os.File, size int64) (bool, error) {
	var last [1]byte
	if _, err := f.ReadAt(last[:], size-1); err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return last[0] == '\n', nil
}
