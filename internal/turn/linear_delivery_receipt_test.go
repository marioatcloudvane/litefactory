package turn

import (
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestCodeProposalReceiptRequiresExactPreparedPublication(t *testing.T) {
	marker := "<!-- litefactory:code-proposal:proposal -->"
	authority := store.DeliveryCodeAuthority{Origin: "https://api.github.com", Repository: "owner/code", PublisherID: 9}
	intent := store.ReportDelivery{BotUserID: 9, Body: "Approve the exact proposal.\n\n" + marker, Marker: marker}
	receipt := github.Comment{ID: 123, Author: github.User{ID: 9}, Body: intent.Body, APIURL: "https://api.github.com/repos/owner/code/issues/comments/123", IssueAPIURL: "https://api.github.com/repos/owner/code/issues/7", CreatedAt: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
	if err := verifyDeliveryPromptReceipt(receipt, authority, 7, intent); err != nil {
		t.Fatalf("exact receipt rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*github.Comment)
	}{
		{"owner whitespace edit", func(c *github.Comment) { c.Body = "\n" + c.Body + "\n" }},
		{"different prepared text", func(c *github.Comment) { c.Body = "Different proposal.\n\n" + marker }},
		{"invalid remote identity", func(c *github.Comment) { c.ID = 0 }},
		{"different publisher", func(c *github.Comment) { c.Author.ID = 99 }},
		{"another API origin", func(c *github.Comment) { c.APIURL = "https://enterprise.example/repos/owner/code/issues/comments/123" }},
		{"another comment", func(c *github.Comment) { c.APIURL = "https://api.github.com/repos/owner/code/issues/comments/124" }},
		{"another pull request", func(c *github.Comment) { c.IssueAPIURL = "https://api.github.com/repos/owner/code/issues/8" }},
		{"another repository", func(c *github.Comment) { c.IssueAPIURL = "https://api.github.com/repos/owner/other/issues/7" }},
		{"missing publication time", func(c *github.Comment) { c.CreatedAt = time.Time{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := receipt
			tc.change(&changed)
			if err := verifyDeliveryPromptReceipt(changed, authority, 7, intent); err == nil {
				t.Fatal("divergent receipt would advance enrollment")
			}
		})
	}
}
