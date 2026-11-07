package turn

import (
	"fmt"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// applyNodeAccess maps the access level an agent node declares (lf_access) to
// the sandbox that node runs in. A node that declares none gets the policy's
// default; a policy with no default leaves the resolved sandbox untouched.
// The tool set is never narrowed: every node holds the full surface, and what
// a node should do is said in its Markdown role.
func applyNodeAccess(policy WorkflowPolicy, node graphexec.Node, req Request) (Request, error) {
	switch access := nodeAccessLevel(policy, node); access {
	case "":
	case "read":
		req.CodexSandbox = config.SandboxReadOnly
	case "write":
		if req.CodexSandbox == config.SandboxReadOnly {
			return req, fmt.Errorf("node %q write access exceeds the mode's read-only sandbox", node.Name)
		}
		req.CodexSandbox = config.SandboxWorkspaceWrite
		req.nodeWrite = true
	default:
		return req, fmt.Errorf("node %q declares unsupported access %q", node.Name, access)
	}
	return req, nil
}

// nodeAccessLevel is the access level an agent node runs under: what its graph
// declares, or the policy's default when it declares none.
func nodeAccessLevel(policy WorkflowPolicy, node graphexec.Node) string {
	if access := node.Attrs[graphexec.AccessAttribute]; access != "" {
		return access
	}
	return policy.DefaultAccess
}
