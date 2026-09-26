package bugchecks

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// bootIdentity is the kernel boot ID; it changes on every reboot.
func bootIdentity() (string, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)), nil
}

// processStart is the kernel start time of pid in clock ticks since boot,
// which a reused PID cannot share.
func processStart(pid int) (string, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	closeIndex := strings.LastIndexByte(string(stat), ')')
	if closeIndex < 0 {
		return "", errors.New("malformed process identity")
	}
	fields := strings.Fields(string(stat[closeIndex+1:]))
	if len(fields) < 20 {
		return "", errors.New("missing process start identity")
	}
	if _, err = strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", err
	}
	return fields[19], nil
}
