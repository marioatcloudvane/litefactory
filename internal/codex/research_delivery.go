package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"
)

// mainInstructionLimit bounds both the retained source and its encoded argv
// value. Only public, fixed application instructions use this transport. Task
// input, saved findings, replies and helper bindings are delivered on stdin.
// The CLI supports additive developer_instructions, but no arbitrary config-file
// selector for the main session; using its supported override preserves HOME,
// authentication, provider session storage and ordinary project discovery.
const mainInstructionLimit = 64 << 10

func mainInstructionOverride(bundle string) (string, error) {
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return "", fmt.Errorf("open retained main instructions: %w", err)
	}
	defer root.Close()
	const name = "main-instructions.md"
	// No symlinks or special files may substitute another source for the
	// factory-retained instruction file. Nonblocking open avoids FIFO hangs.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("open retained main instructions: %w", err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat retained main instructions: %w", err)
	}
	if !before.Mode().IsRegular() || before.Size() > mainInstructionLimit {
		return "", fmt.Errorf("retained main instructions must be a regular file within 64 KiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, mainInstructionLimit+1))
	if err != nil {
		return "", fmt.Errorf("read retained main instructions: %w", err)
	}
	after, err := root.Lstat(name)
	if err != nil {
		return "", fmt.Errorf("recheck retained main instructions: %w", err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(raw)) != before.Size() {
		return "", fmt.Errorf("retained main instructions changed during read")
	}
	if len(raw) > mainInstructionLimit || !utf8.Valid(raw) || strings.TrimSpace(string(raw)) == "" {
		return "", fmt.Errorf("retained main instructions must be nonempty UTF-8 within 64 KiB")
	}
	// JSON escaping is valid for the TOML basic string consumed by -c. In
	// contrast, Go's strconv.Quote may emit unsupported TOML \x escapes.
	encoded, err := json.Marshal(string(raw))
	if err != nil {
		return "", fmt.Errorf("encode retained main instructions: %w", err)
	}
	value := "developer_instructions=" + string(encoded)
	if len(value) > mainInstructionLimit {
		return "", fmt.Errorf("encoded main instructions exceed the 64 KiB argument limit")
	}
	return value, nil
}
