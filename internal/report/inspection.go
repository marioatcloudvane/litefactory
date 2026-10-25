package report

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
)

func shellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// RunInspectionCommand targets the session ordinal accepted by the logs CLI.
func RunInspectionCommand(ref naming.Ref, ordinal int) string {
	if ordinal < 1 {
		return TaskInspectionCommand(ref.String())
	}
	return fmt.Sprintf("litefactory logs %s --run %d", shellArgument(ref.String()), ordinal)
}

// TaskInspectionCommand selects existing factory diagnostics without treating a
// task reference as a regular expression supplied by the owner.
func TaskInspectionCommand(task string) string {
	return "litefactory logs --all --grep " + shellArgument(regexp.QuoteMeta(task))
}
