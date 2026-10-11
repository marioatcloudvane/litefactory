package report

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/naming"
	"net/url"
	"strings"
)

// ticketBrowserURL accepts provider-returned browser links as display values.
// It never supplies authority or a mutation destination.
func ticketBrowserURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && !strings.ContainsAny(value, "\r\n\t <>`")
}

func storySource(ref naming.Ref, child int, comment int64, parentURL, replyURL string) string {
	if child <= 0 || comment <= 0 || ref.Validate() != nil {
		return ""
	}
	if parentURL != "" || replyURL != "" {
		if !ticketBrowserURL(parentURL) || !ticketBrowserURL(replyURL) {
			return ""
		}
		return fmt.Sprintf("\n\nParent plan: %s · source: %s", parentURL, replyURL)
	}
	// Retained reports without provider links preserve their legacy bytes.
	return fmt.Sprintf("\n\nParent plan: https://github.com/%s/%s/issues/%d · source: https://github.com/%s/%s/issues/%d#issuecomment-%d", ref.Owner, ref.Repo, ref.Issue, ref.Owner, ref.Repo, child, comment)
}

// StoryAcknowledgementMarker identifies the delivery owed by a durable admission.
func StoryAcknowledgementMarker(ref naming.Ref, generation, child int, comment int64) string {
	if ref.Validate() != nil || generation < 1 || child < 1 || comment < 1 {
		return ""
	}
	return fmt.Sprintf("<!-- lf:story-ack %s generation=%d child=%d comment=%d -->", ref.String(), generation, child, comment)
}
