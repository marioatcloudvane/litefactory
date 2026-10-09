package bugchecks

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/testutil"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestBugCheckRecoveryProcessObserverRefusesUnverifiableIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if stopped, err := ProcessStopped(ctx, "{}"); stopped || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observer: %v %v", stopped, err)
	}
	for _, identity := range []string{"{}", `{"version":1,"pid":42,"boot":"b","start":"1","namespace":"pid:[1]"}`, "not json"} {
		if stopped, err := ProcessStopped(t.Context(), identity); stopped || err == nil {
			t.Fatalf("unverifiable identity %q certified cleanup: %v %v", identity, stopped, err)
		}
	}
}

// A live group is never certified stopped; once every member is gone it is.
func TestBugCheckRecoveryProcessGroupLifecycle(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "sleep 60 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	identity, err := captureProcessIdentity(cmd.Process.Pid)
	if errors.Is(err, syscall.EPERM) {
		// A sandboxed test run (for example a Bug check of this repository)
		// may not observe other processes; the daemon itself can.
		_ = stopGroup(cmd.Process.Pid)
		_ = cmd.Wait()
		t.Skip("process observation is not permitted here")
	}
	if err != nil {
		t.Fatal(err)
	}
	if stopped, err := ProcessStopped(t.Context(), identity); err != nil || stopped {
		t.Fatalf("live group certified stopped: %v %v", stopped, err)
	}
	// A reused leader PID with another start time still names the live group.
	var reused processIdentity
	if err = json.Unmarshal([]byte(identity), &reused); err != nil {
		t.Fatal(err)
	}
	reused.Start = "0"
	raw, err := json.Marshal(reused)
	if err != nil {
		t.Fatal(err)
	}
	if stopped, err := ProcessStopped(t.Context(), string(raw)); err != nil || stopped {
		t.Fatalf("start mismatch certified a live group stopped: %v %v", stopped, err)
	}
	// The leader stays a zombie group member until reaped, as Run reaps it
	// before confirming cleanup.
	if err = stopGroup(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if !groupEmptyAfterStop(cmd.Process.Pid) {
		t.Fatal("group cleanup not confirmed")
	}
	deadline := time.Now().Add(testutil.Wait)
	for {
		stopped, err := ProcessStopped(t.Context(), identity)
		if err != nil {
			t.Fatal(err)
		}
		if stopped {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("empty group never certified stopped")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
