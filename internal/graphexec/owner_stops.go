package graphexec

import (
	"errors"
	"slices"
	"strings"
)

// OwnerStopAttributes declares a shared opt-in contract, not workflow topology.
// Runtime failures are implicit outcomes; application stops have no forward edge.
func OwnerStopAttributes() map[string]AttributeSpec {
	return map[string]AttributeSpec{
		"lf_park": {Values: []string{"owner"}},
		"lf_stop": {List: true, Values: []string{"blocked", "failed", "invalid", "exhausted", "stale"}},
	}
}

// ParksOwner is true only for an explicitly declared owner-origin contract.
func ParksOwner(node Node) bool { return node.Attrs["lf_park"] == "owner" }

// ParksOutcome reports whether DOT declares this outcome as an origin stop.
func ParksOutcome(node Node, outcome string) bool {
	if !ParksOwner(node) {
		return false
	}
	for value := range strings.SplitSeq(node.Attrs["lf_stop"], ",") {
		if strings.TrimSpace(value) == outcome {
			return true
		}
	}
	return false
}

func validateOwnerStop(node Node) error {
	var faults []error
	if node.Attrs["lf_response"] != "" && (node.Action != "lf_agent" || node.Attrs["lf_response"] != "result") {
		faults = append(faults, errors.New("result response requires an agent"))
	}
	if !ParksOwner(node) {
		if node.Attrs["lf_park"] != "" || node.Attrs["lf_stop"] != "" {
			faults = append(faults, errors.New("stop dispositions require lf_park=owner"))
		}
		return errors.Join(faults...)
	}
	values := OwnerStopAttributes()["lf_stop"].Values
	if node.Action == SyncBaseAction {
		values = append(values, "conflict", "retry")
	}
	faults = append(faults, validateListAttribute(node.Name, "lf_stop", node.Attrs["lf_stop"], values)...)
	return errors.Join(faults...)
}

// OwnerStopAgentOutcomes is the shared response vocabulary. Each policy retains
// responsibility for validating and authorizing its accepted application data.
func OwnerStopAgentOutcomes() []string {
	return slices.Clone([]string{"success", "failed", "ask", "done", "approved", "succeeded", "reshape", "blocked"})
}
