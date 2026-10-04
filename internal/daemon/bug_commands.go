package daemon

import "slices"

// bugControlCommands are the owner commands an existing Bug task answers
// itself. The daemon only routes them; the Bug runtime authenticates and
// parses each against its published wait. A command missing here is answered
// as unknown and never reaches the task.
var bugControlCommands = []string{"/approve-bug"}

func isBugControlCommand(name string) bool {
	return slices.Contains(bugControlCommands, name)
}
