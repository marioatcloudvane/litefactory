package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/launchd"
)

type doctorLaunchctlFixture struct {
	calls            []string
	pid              int
	lost             bool
	executable, home string
}

func (r *doctorLaunchctlFixture) Run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	switch args[0] {
	case "print":
		return fmt.Sprintf("\tstate = running\n\tpid = %d\n\tprogram = %s\n\targuments = {\n\t\t%s\n\t\tdaemon\n\t\trun\n\t}\n\tenvironment = {\n\t\tLF_HOME => %s\n\t}\n", r.pid, r.executable, r.executable, r.home), nil
	case "kickstart":
		if r.lost {
			return "", errors.New("fixture lost restart reply")
		}
		return "", nil
	default:
		return "", errors.New("unexpected lifecycle command")
	}
}

func TestDoctorHostLaunchdTokenSurvivesLostReplyWithoutReplay(t *testing.T) {
	d, paths, _, operation := doctorHostFixture(t)
	intent := doctorHostVerifiedIntent(t, d, operation, "old-launchd")
	if err := claimDoctorHostDispatch(paths, intent); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(paths.Home(), "fixture-bin")
	if err := os.WriteFile(executable, []byte("harmless fixture, never executed"), 0700); err != nil {
		t.Fatal(err)
	}
	replacement, err := NewDoctorServiceReplacement(d.db, executable, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &doctorLaunchctlFixture{pid: os.Getpid(), lost: true, executable: executable, home: paths.Home()}
	manager := &launchd.Manager{Home: paths.Home(), Runner: runner}
	replacement.Manager = manager
	agent := manager.NewAgent([]string{executable, "daemon", "run", "--level", "debug"}, paths.LogsDir(), map[string]string{launchd.PathEnvVar: "/usr/bin", config.EnvHome: paths.Home()})
	data, err := agent.Render()
	if err != nil {
		t.Fatal(err)
	}
	plist, err := manager.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(plist), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(plist, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = replacement.PreflightDoctorReplacement(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = replacement.StartDoctorReplacement(t.Context(), intent); err == nil || !strings.Contains(err.Error(), "lost restart") {
		t.Fatalf("expected lost reply: %v", err)
	}
	pending, err := ReadDoctorLaunchdRecovery(t.Context(), d.db, paths)
	if err != nil || pending == nil || pending.Token != intent.Token {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if err = replacement.StartDoctorReplacement(t.Context(), intent); err == nil {
		t.Fatal("duplicate dispatch allowed")
	}
	count := 0
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "kickstart ") {
			count++
		}
		if strings.HasPrefix(call, "bootstrap ") {
			t.Fatal("rewrote installation")
		}
	}
	if count != 1 {
		t.Fatalf("restart calls=%d", count)
	}
	if err = d.db.AcknowledgeDoctorRestart(t.Context(), intent.RepairID, intent.Token, "new-singleton"); err != nil {
		t.Fatal(err)
	}
	pending, err = ReadDoctorLaunchdRecovery(t.Context(), d.db, paths)
	if err != nil || pending != nil {
		t.Fatalf("historical token reused: %+v %v", pending, err)
	}
}

func TestDoctorHostLaunchdRefusesForeignInstallation(t *testing.T) {
	d, paths, _, operation := doctorHostFixture(t)
	intent := doctorHostVerifiedIntent(t, d, operation, "old-launchd")
	if err := claimDoctorHostDispatch(paths, intent); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(paths.Home(), "fixture-bin")
	if err := os.WriteFile(executable, []byte("never execute"), 0700); err != nil {
		t.Fatal(err)
	}
	replacement, err := NewDoctorServiceReplacement(d.db, executable, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &doctorLaunchctlFixture{pid: os.Getpid(), executable: executable, home: paths.Home()}
	manager := &launchd.Manager{Home: paths.Home(), Runner: runner}
	replacement.Manager = manager
	agent := manager.NewAgent([]string{executable, "daemon", "run"}, paths.LogsDir(), map[string]string{launchd.PathEnvVar: "/usr/bin", config.EnvHome: paths.Home() + "-other"})
	data, err := agent.Render()
	if err != nil {
		t.Fatal(err)
	}
	plist, err := manager.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(plist), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(plist, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = replacement.PreflightDoctorReplacement(t.Context()); err == nil {
		t.Fatal("foreign installation preflight passed")
	}
	if err = replacement.StartDoctorReplacement(t.Context(), intent); err == nil {
		t.Fatal("foreign installation restarted")
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "kickstart") {
			t.Fatal("foreign restart effect")
		}
	}
	pending, err := ReadDoctorLaunchdRecovery(t.Context(), d.db, paths)
	if err != nil || pending != nil {
		t.Fatalf("foreign pointer retained: %+v %v", pending, err)
	}
}
