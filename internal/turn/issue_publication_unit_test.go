package turn

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// These construction tests use an unopened store solely as identity material;
// no store accessor or publication handler is invoked.
type commentUnitForge struct{}

func (commentUnitForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	panic("unexpected forge access")
}
func (commentUnitForge) PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error) {
	panic("unexpected publication")
}
func (commentUnitForge) ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error) {
	panic("unexpected observation")
}

func TestIssueCommentRequest_StrictDecode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input string
		valid       bool
	}{
		{"valid/request preserved", `{"issue_number":284,"message":"reviewed text"}`, true},
		{"foreign repository/refused", `{"issue_number":284,"message":"reviewed text","repository":"other/repo"}`, false},
		{"caller grant/refused", `{"issue_number":284,"message":"reviewed text","grant":"approved"}`, false},
		{"duplicate target/refused", `{"issue_number":284,"issue_number":285,"message":"reviewed text"}`, false},
		{"wrong number type/refused", `{"issue_number":"284","message":"reviewed text"}`, false},
		{"wrong message type/refused", `{"issue_number":284,"message":true}`, false},
		{"trailing value/refused", `{"issue_number":284,"message":"reviewed text"} {}`, false},
		{"malformed JSON/refused", `{"issue_number":284,`, false},
		{"invalid UTF8/refused", "{\"issue_number\":284,\"message\":\"\xff\"}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got IssueCommentRequest
			err := decodeMCPArguments([]byte(tt.input), &got)
			if !tt.valid {
				if err == nil {
					t.Fatal("malformed or authority-bearing arguments accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.IssueNumber != 284 || got.Message != "reviewed text" {
				t.Fatalf("request changed: %+v", got)
			}
		})
	}
}

func TestIssuePublicationService_TargetBinding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		targets []int
		bot     int64
		valid   bool
	}{
		{"empty grant/constructs without targets", nil, 9, true},
		{"positive existing targets/constructs", []int{284, 273}, 9, true},
		{"zero target/refused", []int{0}, 9, false},
		{"negative target/refused", []int{-1}, 9, false},
		{"duplicate target/refused", []int{284, 284}, 9, false},
		{"missing authenticated bot/refused", []int{284}, 0, false},
		{"negative authenticated bot/refused", []int{284}, -1, false},
		{"over limit/refused", make([]int, 101), 9, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := &store.DB{}
			receipts := &ReceiptService{db: db, binding: store.ResultBinding{Task: "github:owner/repo#273", Mode: "implement"}}
			svc, err := NewIssuePublicationService(db, receipts, commentUnitForge{}, redact.New(redact.Options{}), tt.targets, tt.bot)
			if !tt.valid {
				if err == nil || svc != nil {
					t.Fatalf("invalid immutable target grant accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := svc.PermissionMaterial()
			if err != nil {
				t.Fatal(err)
			}
			if len(tt.targets) > 0 {
				tt.targets[0] = 999
			}
			after, err := svc.PermissionMaterial()
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("caller mutation expanded immutable permission material")
			}
			var material struct {
				Targets []int
				BotID   int64
			}
			if err := json.Unmarshal(after, &material); err != nil {
				t.Fatal(err)
			}
			if material.BotID != 9 {
				t.Fatalf("bot identity = %d", material.BotID)
			}
			if len(tt.targets) > 0 && !reflect.DeepEqual(material.Targets, []int{273, 284}) {
				t.Fatalf("bound targets = %v", material.Targets)
			}
			if svc.Binding() != receipts.binding {
				t.Fatal("factory binding changed")
			}
			if svc.Registration().Name != "issues_comment" {
				t.Fatal("wrong tool registration")
			}
		})
	}
}

func TestIssuePublicationService_RequiresFactoryDependencies(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing store", "missing receipts", "different store", "missing forge", "missing redactor", "invalid repository binding"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := &store.DB{}
			receipts := &ReceiptService{db: db, binding: store.ResultBinding{Task: "github:owner/repo#273"}}
			var forge CommentForge = commentUnitForge{}
			redactor := redact.New(redact.Options{})
			switch name {
			case "missing store":
				db = nil
			case "missing receipts":
				receipts = nil
			case "different store":
				receipts.db = &store.DB{}
			case "missing forge":
				forge = nil
			case "missing redactor":
				redactor = nil
			case "invalid repository binding":
				receipts.binding.Task = "other/repo"
			}
			got, err := NewIssuePublicationService(db, receipts, forge, redactor, []int{284}, 9)
			if err == nil || got != nil {
				t.Fatalf("incomplete factory dependencies constructed service: %v", err)
			}
		})
	}
}
