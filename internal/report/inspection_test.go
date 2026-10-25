package report

import (
	"github.com/cloudvane/litefactory/internal/naming"
	"strings"
	"testing"
)

func TestInspectionCommandsBindAffectedRunAndEscapeTask(t *testing.T) {
	ref, err := naming.ParseRef("github:owner/repo#9")
	if err != nil {
		t.Fatal(err)
	}
	command := RunInspectionCommand(ref, 17)
	if command != "litefactory logs 'github:owner/repo#9' --run 17" {
		t.Fatal(command)
	}
	task := TaskInspectionCommand("github:o/r.e'p#9")
	if !strings.Contains(task, `r\.e'"'"'p`) {
		t.Fatal(task)
	}
}
