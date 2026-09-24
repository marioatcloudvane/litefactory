package report

// CreationRejection carries only closed categories, never rejected content.
type CreationRejection struct{ CommandKind, Reason string }

func (r CreationRejection) guidance() (string, bool) {
	command := "`/mcp-create`"
	if r.CommandKind == "preparation" {
		command = "`/mcp-create prepare`"
	} else if r.CommandKind != "approval" {
		return "", false
	}
	var advice string
	switch r.Reason {
	case "syntax":
		advice = "The syntax is invalid. Approval requires exactly the command, one lowercase 64-hex SHA256 from a current proposal, and linked or attach, separated by single spaces. Preparation requires the prepare subcommand followed by one JSON envelope."
	case "payload":
		advice = "The preparation payload is malformed. Supply one JSON envelope using the existing creation fields."
	case "version":
		advice = "The preparation payload version is unsupported. Use version 1."
	case "fields":
		advice = "The preparation fields or limits are invalid. Check the relationship, source binding, artifact fields and existing size and count limits."
	case "mode":
		advice = "Creation requires exactly one Research mode selection. Select Research before submitting a fresh command."
	case "disabled":
		advice = "Creation is disabled in the loaded operator configuration. Ask the operator to enable creation before submitting a fresh command."
	case "proposal":
		advice = "The proposal is absent, expired, superseded, mismatched or otherwise ineligible. Obtain a current eligible proposal and approve it in a fresh comment."
	case "controls":
		advice = "The owner control metadata is ineligible, stale or edited. Submit a fresh owner comment with the required controls."
	default:
		return "", false
	}
	return command + " rejected. " + advice + " Corrections require a new comment; this rejected input grants no authority and starts no work.", true
}
