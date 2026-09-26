package bugchecks

import (
	"context"
	"encoding/json"
	"errors"
	"syscall"
)

// identityVersion marks a process-group identity taken under the sandbox
// launcher. Earlier namespace identities are refused as unverifiable.
const identityVersion = 2

type processIdentity struct {
	Version int    `json:"version"`
	PID     int    `json:"pid"`
	Group   int    `json:"group"`
	Boot    string `json:"boot"`
	Start   string `json:"start"`
}

func readProcessIdentity(pid int) (processIdentity, error) {
	out := processIdentity{Version: identityVersion, PID: pid, Group: pid}
	if pid <= 0 {
		return out, errors.New("invalid check process")
	}
	boot, err := bootIdentity()
	if err != nil {
		return out, err
	}
	start, err := processStart(pid)
	if err != nil {
		return out, err
	}
	if boot == "" || start == "" {
		return out, errors.New("invalid process kernel identity")
	}
	out.Boot, out.Start = boot, start
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

// ProcessStopped proves the recorded process group has no surviving member. It
// only probes the group and never signals it. Missing or earlier identities,
// unreadable observations and a live group remain unresolved. A different boot
// proves no process from the original kernel survived. A reused leader PID does
// not imply stop: the group itself must be empty.
func ProcessStopped(ctx context.Context, identity string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var expected processIdentity
	if len(identity) > 4096 || json.Unmarshal([]byte(identity), &expected) != nil || expected.Version != identityVersion || expected.PID <= 0 || expected.Group <= 0 || expected.Start == "" || expected.Boot == "" {
		return false, errors.New("missing verified check process start identity")
	}
	boot, err := bootIdentity()
	if err != nil {
		return false, err
	}
	if boot != expected.Boot {
		return true, nil
	}
	switch err := syscall.Kill(-expected.Group, 0); {
	case errors.Is(err, syscall.ESRCH):
		return true, nil
	case err == nil, errors.Is(err, syscall.EPERM):
		return false, nil
	default:
		return false, err
	}
}
