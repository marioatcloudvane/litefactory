// Package logging builds litefactory's process logger.
//
// There are two sinks. A JSON handler writes every record to daemon.log,
// always. A text handler writes to the console, but only when the console is a
// terminal — piping litefactory into a file or another process must not
// interleave human-readable log lines with command output. When both apply,
// both receive every record.
//
// The logger is passed around as a struct field. There is no package-level
// logger and no default logger installation.
package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
)

// Options configures [New].
type Options struct {
	// Level is the minimum level both sinks emit. The zero value is
	// slog.LevelInfo.
	Level slog.Level
	// Console receives human-readable text records. Nil disables the text
	// sink; main sets it only when stdout is a terminal (see [IsTerminal]).
	Console io.Writer
	// File receives JSON records; this is daemon.log. Nil disables the JSON
	// sink.
	File io.Writer
}

// New builds a logger fanning every record out to the configured sinks. With
// no sinks configured the logger discards everything, which is what tests
// want by default.
func New(opts Options) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var handlers []slog.Handler
	if opts.Console != nil {
		handlers = append(handlers, slog.NewTextHandler(opts.Console, handlerOpts))
	}
	if opts.File != nil {
		handlers = append(handlers, slog.NewJSONHandler(opts.File, handlerOpts))
	}
	if len(handlers) == 1 {
		return slog.New(handlers[0])
	}
	return slog.New(fanout(handlers))
}

// fanout delivers every record to each of its handlers. A nil or empty fanout
// is a valid discard handler.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, rec slog.Record) error {
	var errs []error
	for _, h := range f {
		if !h.Enabled(ctx, rec.Level) {
			continue
		}
		// Each handler gets its own copy: Handle may retain or mutate the
		// record's attribute storage.
		if err := h.Handle(ctx, rec.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}

// ParseLevel converts a config value ("debug", "info", "warn", "error") to a
// slog level. An empty string yields slog.LevelInfo.
func ParseLevel(s string) (slog.Level, error) {
	if s == "" {
		return slog.LevelInfo, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo, err
	}
	return level, nil
}

// IsTerminal reports whether f is attached to a character device, which is the
// dependency-free way to ask "is this a tty".
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
