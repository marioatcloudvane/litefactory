package providerprocess

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CancelConversationTree enumerates only descendants of the owned provider PID
// before killing their process groups. No name/pattern-based shutdown is used.
func CancelConversationTree(ctx context.Context, pid int) error {
	if pid <= 1 {
		return errors.New("invalid owned provider pid")
	}
	self, err := syscall.Getpgid(0)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Fixed system process metadata command, with no owner-supplied text.
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,pgid=")
	data, err := cmd.Output()
	if err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return errors.New("conversation process tree could not be established")
	}
	type process struct{ pid, parent, group int }
	var processes []process
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		p, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		group, e3 := strconv.Atoi(fields[2])
		if e1 == nil && e2 == nil && e3 == nil {
			processes = append(processes, process{p, parent, group})
		}
	}
	owned := map[int]bool{pid: true}
	groups := map[int]bool{pid: true}
	for changed := true; changed; {
		changed = false
		for _, p := range processes {
			if owned[p.parent] && !owned[p.pid] {
				owned[p.pid] = true
				changed = true
			}
			if owned[p.pid] {
				groups[p.group] = true
			}
		}
	}
	var result error
	for group := range groups {
		if group <= 1 || group == self {
			continue
		}
		e := syscall.Kill(-group, syscall.SIGKILL)
		if !errors.Is(e, syscall.ESRCH) {
			result = errors.Join(result, e)
		}
	}
	return result
}
