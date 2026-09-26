package bugchecks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type processIdentity struct {
	Version   int    `json:"version"`
	PID       int    `json:"pid"`
	Boot      string `json:"boot"`
	Start     string `json:"start"`
	Namespace string `json:"namespace"`
}

func readProcessIdentity(pid int) (processIdentity, error) {
	out := processIdentity{Version: 1, PID: pid}
	if runtime.GOOS != "linux" || pid <= 0 {
		return out, errors.New("process recovery requires Linux identity")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return out, err
	}
	out.Boot = strings.TrimSpace(string(boot))
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return out, err
	}
	closeIndex := strings.LastIndexByte(string(stat), ')')
	if closeIndex < 0 {
		return out, errors.New("malformed process identity")
	}
	fields := strings.Fields(string(stat[closeIndex+1:]))
	if len(fields) < 20 {
		return out, errors.New("missing process start identity")
	}
	out.Start = fields[19]
	if _, err = strconv.ParseUint(out.Start, 10, 64); err != nil {
		return out, err
	}
	out.Namespace, err = os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
	if err != nil {
		return out, err
	}
	if out.Boot == "" || !strings.HasPrefix(out.Namespace, "pid:[") {
		return out, errors.New("invalid process kernel identity")
	}
	return out, nil
}
func captureProcessIdentity(pid int) (string, error) {
	before, err := readProcessIdentity(pid)
	if err != nil {
		return "", err
	}
	after, err := readProcessIdentity(pid)
	if err != nil {
		return "", err
	}
	if before != after {
		return "", errors.New("process changed during identity observation")
	}
	raw, err := json.Marshal(before)
	return string(raw), err
}

// ProcessStopped proves the recorded PID namespace has no surviving processes.
// It never signals a PID. Missing or legacy launch identity, unreadable process
// observations and a live namespace remain unresolved. A different boot proves
// no process from the original kernel survived. PID reuse does not imply stop.
func ProcessStopped(ctx context.Context, identity string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if runtime.GOOS != "linux" {
		return false, errors.New("check process reconciliation requires Linux")
	}
	var expected processIdentity
	if len(identity) > 4096 || json.Unmarshal([]byte(identity), &expected) != nil || expected.Version != 1 || expected.PID <= 0 || expected.Start == "" || expected.Boot == "" || !strings.HasPrefix(expected.Namespace, "pid:[") {
		return false, errors.New("missing verified check process start identity")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(boot)) != expected.Boot {
		return true, nil
	}
	current, err := readProcessIdentity(expected.PID)
	if err == nil && current == expected {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	// A reused PID is never signalled and cannot certify cleanup. Inspect the
	// retained namespace itself; its descendants may outlive the original PID.
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		if _, err = strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		namespace, readErr := os.Readlink(filepath.Join("/proc", entry.Name(), "ns/pid"))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return false, readErr
		}
		if namespace == expected.Namespace {
			return false, nil
		}
	}
	return true, nil
}
