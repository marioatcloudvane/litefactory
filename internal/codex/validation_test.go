package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestValidateKeepsReadOnlyShellOnAllConversationPaths(t *testing.T) {
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		req := request(t, path)
		req.Mode = "validate"
		r := &Runner{}
		argv, err := r.args(req, "read-only")
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(argv, " ")
		for _, want := range []string{"--sandbox read-only", "features.multi_agent=false"} {
			if !strings.Contains(args, want) {
				t.Fatalf("%s missing %s", args, want)
			}
		}
		if strings.Contains(args, "features.shell_tool=false") {
			t.Fatalf("%s disables the shell, Codex Validate's only file reader", args)
		}
	}
}

// The shell Validate keeps is only safe inside the read-only sandbox.
func TestValidateRefusesWritableSandbox(t *testing.T) {
	runner, _ := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	req := request(t, agent.PathCold)
	req.Mode = "validate"
	_, err := runner.Run(context.Background(), req)
	if !errors.Is(err, agent.ErrInvalidRequest) || !strings.Contains(fmt.Sprint(err), "read-only sandbox") {
		t.Fatalf("writable Validate sandbox accepted: %v", err)
	}
}
