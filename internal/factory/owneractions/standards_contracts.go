package owneractions

import (
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
)

// StandardsRequest identifies one exact retained operation.
type StandardsRequest struct {
	Operation                                              string
	Arguments                                              map[string]string
	Transport, OwnerID, ChannelID, MessageID, OperationKey string
	At                                                     time.Time
}

// StandardsResult contains factual domain output and factory-issued targets.
type StandardsResult struct {
	Status string

	Text        string
	References  map[string]string
	Interrupted bool
}

// StandardsCommand identifies the existing standards command family.
func StandardsCommand(command string) bool {
	return command == "standards" || strings.HasPrefix(command, "standards-")
}

// StandardsOperation maps the command to its fixed supported operation.
func StandardsOperation(inv owneraction.Invocation) (string, error) {
	action := inv.Arguments["action"]
	if inv.Command != "standards" {
		action = strings.TrimPrefix(inv.Command, "standards-") + "." + action
	}
	allowed := "book.current book.history book.categories book.standards book.show author.list author.inspect author.start author.answer author.revise author.edit author.continue author.validate author.complete author.rebase author.publish proposal.list proposal.inspect proposal.review proposal.edit proposal.approve proposal.publish exception.propose exception.show exception.list exception.approve exception.revoke analysis.start analysis.list analysis.show operation.show"
	if inv.Command == "standards" && (action == "discover" || action == "help") {
		return "discover", nil
	}
	for _, op := range strings.Fields(allowed) {
		if op != action {
			continue
		}
		read := strings.HasPrefix(op, "book.") || strings.HasSuffix(op, ".inspect") || strings.HasSuffix(op, ".review") || strings.HasSuffix(op, ".show") || strings.HasSuffix(op, ".list")
		if read == (inv.Command == "standards") {
			return op, nil
		}
	}
	return "", errors.New("standards action is unavailable; ask “show available standards operations” here to inspect supported actions")
}

// StandardsMissingTerms reports missing exact terms before a standards mutation.
func StandardsMissingTerms(op string, args map[string]string) string {
	var required []string
	switch op {
	case "author.start":
		required = []string{"intent"}
	case "author.answer", "author.revise", "author.publish":
		// The session defaults to this conversation's; the text is the
		// owner's own words (for publish: the words that say to publish).
		required = []string{"text"}
	case "proposal.edit":
		required = []string{"id", "version"}
	case "proposal.approve", "proposal.publish":
		required = []string{"id", "version", "digest"}
	case "exception.approve":
		required = []string{"id", "digest"}
	case "exception.revoke":
		required = []string{"id", "reason"}
	case "exception.propose":
		if args["request"] == "" {
			required = []string{"book_revision", "standard_id", "repository_id", "task_ref", "departure", "reason", "expires_at"}
		}
	case "analysis.start":
		if args["repository_id"] == "" {
			required = append(required, "project")
		}
		required = append(required, "after", "before")
	}
	var missing []string
	for _, field := range required {
		if strings.TrimSpace(args[field]) == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return "Please specify the exact standards terms before changing state: " + strings.Join(missing, ", ") + "."
	}
	return ""
}
