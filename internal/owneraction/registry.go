package owneraction

import (
	"errors"
	"strings"
)

// Fixed policy tiers cannot be overridden by transport input.
const (
	Conversational = "conversational"
	Binding        = "binding"
	PolicyVersion  = 1
)

// Classify is the fixed allowlist shared by discovery and admission. New changing
// actions remain unavailable until deliberately classified here.
func Classify(inv Invocation) (tier, scope string, err error) {
	switch inv.Command {
	case "workflow-analysis":
		return Conversational, "repository:actions", nil
	case "workflow-author":
		return Conversational, "standards:management", nil
	case "draft", "diagnose", "answer":
		return Conversational, "repository:actions", nil
	case "confirm":
		return Binding, "repository:actions", nil
	}
	if !strings.HasPrefix(inv.Command, "standards-") {
		return "", "", errors.New("action is not classified")
	}
	op := strings.TrimPrefix(inv.Command, "standards-") + "." + inv.Arguments["action"]
	scope = "standards:management"
	if strings.HasPrefix(op, "exception.") || op == "analysis.start" {
		scope = "repository:actions"
	}
	switch op {
	case "analysis.start", "author.start", "author.answer", "author.revise", "author.edit", "author.continue", "author.validate", "author.complete", "author.rebase", "proposal.edit", "exception.propose":
		return Conversational, scope, nil
	case "author.publish", "proposal.approve", "proposal.publish", "exception.approve", "exception.revoke":
		return Binding, scope, nil
	default:
		return "", "", errors.New("action is not classified")
	}
}
