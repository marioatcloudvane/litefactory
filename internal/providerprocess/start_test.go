package providerprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStartRecovery(t *testing.T) {
	for _, secondFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovered", true: "second failure"}[secondFailure], func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "provider")
			done := make(chan error, 1)
			attempts := 0
			err := Start(t.Context(), binary, dir, func() error {
				attempts++
				cmd := exec.CommandContext(t.Context(), binary)
				cmd.Dir = dir
				if attempts == 2 && secondFailure {
					if err := os.Remove(binary); err != nil {
						t.Fatal(err)
					}
				}
				err := cmd.Start()
				if attempts == 1 {
					go func() {
						time.Sleep(200 * time.Millisecond)
						done <- os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
					}()
				}
				if err == nil {
					return cmd.Wait()
				}
				return err
			})
			if writeErr := <-done; writeErr != nil {
				t.Fatal(writeErr)
			}
			if attempts != 2 || (err != nil) != secondFailure {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestStartDoesNotRetryUnrelatedFailures(t *testing.T) {
	for _, mode := range []string{"directory", "interpreter", "permissions", "exit"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "provider")
			script := "#!/bin/sh\nexit 7\n"
			if mode == "interpreter" {
				script = "#!/missing-interpreter\n"
			}
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if mode == "permissions" {
				if err := os.Chmod(binary, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "directory" {
				dir = filepath.Join(dir, "missing")
			}
			attempts := 0
			err := Start(t.Context(), binary, dir, func() error {
				attempts++
				cmd := exec.CommandContext(t.Context(), binary)
				cmd.Dir = dir
				if err := cmd.Start(); err != nil {
					return err
				}
				return cmd.Wait()
			})
			if attempts != 1 || err == nil {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
		})
	}
}
func TestStartAbsentBoundAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "cancelled"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			binary := filepath.Join(t.TempDir(), "missing")
			attempts := 0
			start := time.Now()
			err := Start(ctx, binary, "", func() error {
				attempts++
				err := exec.CommandContext(ctx, binary).Start()
				if cancelled {
					cancel()
				}
				return err
			})
			want := context.DeadlineExceeded
			if cancelled {
				want = context.Canceled
			}
			if attempts != 1 || !errors.Is(err, os.ErrNotExist) || !errors.Is(err, want) {
				t.Fatalf("%d %v", attempts, err)
			}
			if time.Since(start) > 6*time.Second {
				t.Fatal("recovery exceeded bound")
			}
		})
	}
}
