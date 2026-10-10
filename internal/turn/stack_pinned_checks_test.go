package turn

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestPinnedCheckReceivesProfileEnvironmentWithoutReplacingRecipe(t *testing.T) {
	p, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"1"},{"name":"GOTOOLCHAIN","value":"local"}],"checks":[{"name":"profile-default","argv":["other-tool","test"],"cwd":".","timeout_seconds":10}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	isolation := bugchecks.Isolation{Binary: "operator-sandbox", Home: "/operator-home", PATH: "/bin", Network: false, Revision: "pinned-operator", WritableRoots: []string{"/approved/cache"}, Environment: map[string]string{"NO_COLOR": "1"}}
	command := config.BugCheckCommand{Argv: []string{"go", "test", "./..."}, CWD: "service", TimeoutSeconds: 321, Env: []string{"CI"}}
	gotIsolation, err := isolation.WithStackProfile(&p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pinnedStackCommand(&p, command)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Argv, command.Argv) || got.CWD != command.CWD || got.TimeoutSeconds != command.TimeoutSeconds || got.Rerun != command.Rerun {
		t.Fatal("profile replaced pinned authoritative recipe", got)
	}
	if slices.Contains(got.Argv, "other-tool") || !slices.Contains(got.Env, "CI") || !slices.Contains(got.Env, "GOTOOLCHAIN") || len(got.Env) != 2 {
		t.Fatal("profile environment not uniformly applied", got.Env)
	}
	if gotIsolation.Environment["CI"] != "1" || gotIsolation.Environment["GOTOOLCHAIN"] != "local" || gotIsolation.Environment["NO_COLOR"] != "1" {
		t.Fatal("effective values differ from provider profile")
	}
	if isolation.Environment["CI"] != "" || !reflect.DeepEqual(command.Env, []string{"CI"}) {
		t.Fatal("pinned inputs were mutated")
	}
	if gotIsolation.Binary != isolation.Binary || gotIsolation.PATH != isolation.PATH || gotIsolation.Home != isolation.Home || gotIsolation.Network != isolation.Network || gotIsolation.Revision != isolation.Revision || !reflect.DeepEqual(gotIsolation.WritableRoots, isolation.WritableRoots) {
		t.Fatal("profile enlarged pinned check permissions")
	}
	p.EffectiveDigest = "tampered"
	if _, err = pinnedStackCommand(&p, command); err == nil {
		t.Fatal("tampered profile reached pinned check")
	}
}

func TestValidatePinnedSuiteReceivesAdmittedEnvironmentInActualActivity(t *testing.T) {
	f := newSuiteFixture(t)
	p, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"1"},{"name":"GOTOOLCHAIN","value":"local"}],"checks":[{"name":"alternate-profile-default","argv":["unselected-profile-command"],"cwd":".","timeout_seconds":10}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	f.b.StackProfile = &p
	original := f.b.Run
	calls := 0
	f.b.Run = func(ctx context.Context, cwd string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		calls++
		if !slices.Contains(c.Env, "CI") || !slices.Contains(c.Env, "GOTOOLCHAIN") {
			t.Fatal("actual pinned suite omitted admitted environment refs", c.Env)
		}
		if c.Argv[0] == "unselected-profile-command" {
			t.Fatal("profile replaced pinned full phase")
		}
		return original(ctx, cwd, c, started)
	}
	_, out, err := f.exec(41)
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 || out.Outcome != "passed" {
		t.Fatal("actual pinned suite was not executed", calls, out)
	}
}
