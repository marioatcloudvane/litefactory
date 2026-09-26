package bugchecks

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// bootIdentity is the kernel boot time; it changes on every reboot.
func bootIdentity() (string, error) {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%06d", tv.Sec, tv.Usec), nil
}

// processStart is the kernel start time of pid, which a reused PID cannot share.
func processStart(pid int) (string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if int(kp.Proc.P_pid) != pid {
		return "", os.ErrNotExist
	}
	start := kp.Proc.P_starttime
	if start.Sec == 0 && start.Usec == 0 {
		return "", errors.New("missing process start time")
	}
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), nil
}
