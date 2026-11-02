package process

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOwnedBarrierRequiresAcknowledgedPID(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(strconv.FormatBool(refuse), func(t *testing.T) {
			directory := t.TempDir()
			lock, err := os.Create(filepath.Join(directory, "lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Close() }()
			marker := filepath.Join(directory, "executed")
			binary := filepath.Join(directory, "provider")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\necho executed > '"+marker+"'\necho '{}'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			denied := errors.New("PID acknowledgment refused")
			acknowledged := false
			exited := false
			phase, started, err := RunOwned(t.Context(), binary, nil, []string{}, OwnedOptions{
				Directory: directory, Timeout: time.Second * 5, Lock: lock,
				OnStarted: func(pid int) error {
					if pid <= 1 {
						t.Errorf("invalid owned PID %d", pid)
					}
					if _, e := os.Stat(marker); !errors.Is(e, os.ErrNotExist) {
						t.Errorf("provider ran before PID acknowledgment: %v", e)
					}
					acknowledged = true
					if refuse {
						return denied
					}
					return nil
				},
				OnExited: func(bool) { exited = true },
			}, func([]byte) {})
			if !acknowledged || !exited || !started {
				t.Fatalf("missing lifecycle facts: %v %v %v", acknowledged, exited, started)
			}
			if refuse {
				if !errors.Is(err, denied) || phase != "ownership_persistence" {
					t.Fatalf("lost refusal: %s %v", phase, err)
				}
				if _, e := os.Stat(marker); !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("refused provider ran: %v", e)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, e := os.ReadFile(marker)
				if e != nil || strings.TrimSpace(string(data)) != "executed" {
					t.Fatalf("provider did not run: %q %v", data, e)
				}
			}
		})
	}
}
