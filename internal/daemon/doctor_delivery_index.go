package daemon

import (
	"net/url"
	"strconv"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
)

func doctorCommentAtDestination(comment github.Comment, destination naming.Ref) bool {
	if comment.HTMLURL == "" {
		// Raw API readback may omit browser links. Both provider-owned API URLs
		// must then bind this exact comment to the expected issue.
		base := "https://api.github.com/repos/" + destination.Owner + "/" + destination.Repo
		return comment.ID > 0 && comment.APIURL == base+"/issues/comments/"+strconv.FormatInt(comment.ID, 10) && comment.IssueAPIURL == base+"/issues/"+strconv.Itoa(destination.Issue)
	}
	parsed, err := url.Parse(comment.HTMLURL)
	return err == nil && comment.ID > 0 && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.Path == "/"+destination.Owner+"/"+destination.Repo+"/issues/"+strconv.Itoa(destination.Issue) && parsed.Fragment == "issuecomment-"+strconv.FormatInt(comment.ID, 10)
}
