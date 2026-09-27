package turn_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestCreationRejectionReason_ConfirmedCategories(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"syntax", "payload", "version", "fields", "mode", "disabled", "proposal", "controls"} {
		t.Run(reason+"/retains safe category through wrapping", func(t *testing.T) {
			t.Parallel()
			cause := errors.New("private payload secret-proposal-content")
			err := fmt.Errorf("intake: %w", &turn.CreationRejectionError{Reason: reason, Cause: cause})
			got, rejected := turn.CreationRejectionReason(err)
			if !rejected || got != reason || !errors.Is(err, cause) {
				t.Fatalf("got (%q,%v), cause retained=%v", got, rejected, errors.Is(err, cause))
			}
		})
	}
}

func TestCreationRejectionReason_OperationalFailuresStayPending(t *testing.T) {
	t.Parallel()
	storage := errors.New("storage observation failed")
	policy := &store.CreationPolicyRefusal{}
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil}, {"broad authority sentinel", store.ErrCreationAuthority},
		{"authority wraps failed observation", fmt.Errorf("%w: %w", store.ErrCreationAuthority, storage)},
		{"storage", storage}, {"shutdown", context.Canceled}, {"deadline", context.DeadlineExceeded},
		{"rate limit", errors.New("forge rate limit")}, {"forge outage", errors.New("forge 503")},
		{"uncertain persistence", errors.New("commit outcome unknown")}, {"unknown internal", errors.New("unknown failure")},
		{"policy joined rollback failure", errors.Join(policy, storage)},
		{"wrapped joined rollback failure", fmt.Errorf("transaction: %w", errors.Join(policy, storage))},
		{"input refusal joined rollback failure", errors.Join(&turn.CreationRejectionError{Reason: "syntax"}, storage)},
	}
	for _, tt := range cases {
		t.Run(tt.name+"/not rejected", func(t *testing.T) {
			t.Parallel()
			reason, rejected := turn.CreationRejectionReason(tt.err)
			if rejected || reason != "" {
				t.Fatalf("operational error classified (%q,%v)", reason, rejected)
			}
		})
	}
	t.Run("confirmed proposal policy/rejected", func(t *testing.T) {
		reason, rejected := turn.CreationRejectionReason(fmt.Errorf("intake: %w", policy))
		if !rejected || reason != "proposal" {
			t.Fatalf("got (%q,%v)", reason, rejected)
		}
	})
}

func TestCreationAuthorizer_MissingWiringStaysPending(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		authorizer *turn.CreationAuthorizer
	}{{"nil authorizer", nil}, {"unwired authorizer", &turn.CreationAuthorizer{}}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, err := tt.authorizer.AdmitOwnerGrantIntent(t.Context(), turn.OwnerGrantControl{Body: "/mcp-create malformed"})
			_, rejected := turn.CreationRejectionReason(err)
			if id != 0 || err == nil || rejected {
				t.Fatalf("id=%d error=%v rejected=%v", id, err, rejected)
			}
		})
	}
}

func TestParseOwnerGrantCommand_ExactApprovalGrammar(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("abcdef01", 8)
	cases := []struct{ name, body, relationship string }{
		{"linked", "/mcp-create " + hash + " linked", "linked"}, {"attach", "/mcp-create " + hash + " attach", "attach"},
		{"uppercase hash", "/mcp-create " + strings.ToUpper(hash) + " linked", ""},
		{"short hash", "/mcp-create " + hash[:63] + " linked", ""},
		{"long hash", "/mcp-create " + hash + "0 linked", ""},
		{"non hexadecimal", "/mcp-create " + hash[:63] + "g linked", ""},
		{"uppercase relationship", "/mcp-create " + hash + " Linked", ""},
		{"unknown relationship", "/mcp-create " + hash + " publish", ""},
		{"double spaces", "/mcp-create  " + hash + " linked", ""},
		{"tab separator", "/mcp-create\t" + hash + " linked", ""},
		{"newline suffix", "/mcp-create " + hash + " linked\n", ""},
		{"trailing prose", "/mcp-create " + hash + " linked please", ""},
		{"wrong command", "/MCP-create " + hash + " linked", ""},
		{"empty", "", ""}, {"oversize", strings.Repeat("x", 129), ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			command, err := turn.ParseOwnerGrantCommand(tt.body)
			if tt.relationship != "" {
				if err != nil || command.ProposalHash != hash || command.Relationship != tt.relationship {
					t.Fatalf("command=%+v err=%v", command, err)
				}
				return
			}
			reason, rejected := turn.CreationRejectionReason(err)
			if err == nil || !rejected || reason != "syntax" || command.ProposalHash != "" || command.Relationship != "" {
				t.Fatalf("invalid input yielded command=%+v error=%v reason=%q rejected=%v", command, err, reason, rejected)
			}
		})
	}
}

func TestCreationAuthorizer_ConfirmedIntakeRefusalCategories(t *testing.T) {
	t.Parallel()
	valid := turn.OwnerGrantControl{Repository: "owner/repo", SourceTask: "github:owner/repo#314", Mode: "research", Body: "/mcp-create " + strings.Repeat("a", 64) + " linked", CommentID: 42, ActorID: 10, CreatedAt: time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC)}
	cases := []struct {
		name, reason string
		disabled     bool
		change       func(*turn.OwnerGrantControl)
	}{
		{"loaded capability disabled", "disabled", true, func(*turn.OwnerGrantControl) {}},
		{"numeric nonowner", "controls", false, func(c *turn.OwnerGrantControl) { c.ActorID = 11 }},
		{"bot identity", "controls", false, func(c *turn.OwnerGrantControl) { c.ActorID = 20 }},
		{"owner marked bot", "controls", false, func(c *turn.OwnerGrantControl) { c.Bot = true }},
		{"absent source", "controls", false, func(c *turn.OwnerGrantControl) { c.CommentID = 0 }},
		{"absent timestamp", "controls", false, func(c *turn.OwnerGrantControl) { c.CreatedAt = time.Time{} }},
		{"invalid approval", "syntax", false, func(c *turn.OwnerGrantControl) { c.Body = "/mcp-create please approve" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Observation reports no prior admission; the zero store forbids new writes.
			a, err := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: &store.CreationStore{}, SourceOutcomes: &sourceOutcomeFake{err: store.ErrNotFound}, Enabled: !tt.disabled, OwnerID: 10, BotID: 20})
			if err != nil {
				t.Fatal(err)
			}
			control := valid
			tt.change(&control)
			id, err := a.AdmitOwnerGrantIntent(t.Context(), control)
			reason, rejected := turn.CreationRejectionReason(err)
			if id != 0 || !rejected || reason != tt.reason {
				t.Fatalf("id=%d error=%v classification=(%q,%v), want %q", id, err, reason, rejected, tt.reason)
			}
		})
	}
}
