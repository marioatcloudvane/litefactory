package launchd

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type doctorRestartRunner struct {
	args []string
	err  error
}

func (r *doctorRestartRunner) Run(_ context.Context, args ...string) (string, error) {
	r.args = append([]string(nil), args...)
	return "", r.err
}
func TestDoctorRestartUsesInstalledIdentityWithoutInstall(t *testing.T) {
	loss := errors.New("reply lost")
	runner := &doctorRestartRunner{err: loss}
	m := Manager{UID: 501, Label: "test.litefactory", Runner: runner}
	if err := m.Restart(t.Context()); !errors.Is(err, loss) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runner.args, []string{"kickstart", "-k", "gui/501/test.litefactory"}) {
		t.Fatal(runner.args)
	}
	runner.args = nil
	m.Label = "wrong/target"
	if err := m.Restart(t.Context()); err == nil || runner.args != nil {
		t.Fatal("unsafe service target executed")
	}
}
