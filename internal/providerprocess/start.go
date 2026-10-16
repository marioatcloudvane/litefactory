package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Start retries only a demonstrably absent configured executable, before any
// process starts. Each attempt must construct fresh commands and pipes, clean
// up failed attempts, and recheck delivery authorization.
func Start(ctx context.Context, binary, directory string, attempt func() error) error {
	err := attempt()
	if err == nil || !missingExecutable(err, binary, directory) {
		return err
	}
	recovery, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if waitErr := waitForRetry(recovery, 100*time.Millisecond); waitErr != nil {
			return errors.Join(err, waitErr)
		}
		if _, statErr := os.Stat(binary); statErr == nil {
			if ctx.Err() != nil {
				return errors.Join(err, ctx.Err())
			}
			return attempt()
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return errors.Join(err, statErr)
		}
	}
}

func missingExecutable(err error, binary, directory string) bool {
	var launchErr *os.PathError
	if !errors.As(err, &launchErr) || launchErr.Op != "fork/exec" || launchErr.Path != binary || !errors.Is(err, os.ErrNotExist) || !filepath.IsAbs(binary) {
		return false
	}
	// A missing working directory or interpreter is not an update race.
	if directory != "" {
		info, dirErr := os.Stat(directory)
		if dirErr != nil || !info.IsDir() {
			return false
		}
	}
	_, statErr := os.Stat(binary)
	return errors.Is(statErr, os.ErrNotExist)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
