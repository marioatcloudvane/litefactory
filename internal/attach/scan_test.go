package attach

import (
	"strings"
	"testing"
)

func TestScanFindsAttachmentsInEverySyntaxGitHubRenders(t *testing.T) {
	t.Parallel()

	const uuid = "https://github.com/user-attachments/assets/11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name     string
		body     string
		wantURL  string
		wantName string
	}{
		{
			name:     "markdown image with alt text",
			body:     "Here is the mockup:\n\n![login screen](" + uuid + ")\n",
			wantURL:  uuid,
			wantName: "login screen",
		},
		{
			name:     "markdown image without alt text falls back to the path segment",
			body:     "![](" + uuid + ")",
			wantURL:  uuid,
			wantName: "11111111-2222-3333-4444-555555555555",
		},
		{
			name:     "html img tag",
			body:     `<img width="400" src="` + uuid + `" alt="x">`,
			wantURL:  uuid,
			wantName: "11111111-2222-3333-4444-555555555555",
		},
		{
			name:     "bare url on its own line",
			body:     "see this\n" + uuid + "\nthanks",
			wantURL:  uuid,
			wantName: "11111111-2222-3333-4444-555555555555",
		},
		{
			name:     "legacy user-images host",
			body:     "![shot](https://user-images.githubusercontent.com/1/a.png)",
			wantURL:  "https://user-images.githubusercontent.com/1/a.png",
			wantName: "shot",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refs := Scan(tc.body)
			if len(refs) != 1 {
				t.Fatalf("Scan returned %d refs, want 1: %+v", len(refs), refs)
			}
			if refs[0].URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", refs[0].URL, tc.wantURL)
			}
			if refs[0].Name != tc.wantName {
				t.Errorf("Name = %q, want %q", refs[0].Name, tc.wantName)
			}
		})
	}
}

// TestScanIgnoresEverythingNotAGitHubAsset is AC-3.2 at the only layer that
// can make it true by construction: a URL that never becomes a Ref is a URL
// the fetcher is never asked about, so there is no later check to forget.
func TestScanIgnoresEverythingNotAGitHubAsset(t *testing.T) {
	t.Parallel()

	bodies := []string{
		"![x](https://evil.example.com/payload.png)",
		"![x](https://github.com.evil.example.com/user-attachments/assets/aaa)",
		"![x](http://github.com/user-attachments/assets/aaa)",           // not https
		"![x](https://github.com/cloudvane/litefactory/issues/33)",      // github, wrong path
		"![x](https://raw.githubusercontent.com/o/r/main/thing.png)",    // not an asset host
		"![x](https://user-images.githubusercontent.evil.com/1/a.png)",  // suffix trick
		"see http://169.254.169.254/latest/meta-data/",                  // link-local, not https
		"![x](https://github.com/user-attachments/../../secrets/a.png)", // escapes the prefix
	}
	for _, body := range bodies {
		if refs := Scan(body); len(refs) != 0 {
			t.Errorf("Scan(%q) returned %+v, want none", body, refs)
		}
	}
}

func TestScanCollapsesDuplicatesAcrossBodies(t *testing.T) {
	t.Parallel()

	const url = "https://github.com/user-attachments/assets/dupe-1234"
	comment := "![from the comment](" + url + ")"
	issue := "![from the issue](" + url + ")"

	refs := Scan(comment, issue)
	if len(refs) != 1 {
		t.Fatalf("Scan returned %d refs, want 1: %+v", len(refs), refs)
	}
	// First reference wins, and the comment is scanned first, so the run
	// describes the attachment the way the person who just attached it did.
	if refs[0].Name != "from the comment" {
		t.Errorf("Name = %q, want the first body's name", refs[0].Name)
	}
}

// TestScanIsStableWhenTheTriggerIsTheIssueBody covers the case internal/turn
// creates on every issue-triggered run: the same text passed twice.
func TestScanIsStableWhenTheTriggerIsTheIssueBody(t *testing.T) {
	t.Parallel()

	body := "![a](https://github.com/user-attachments/assets/a1)\n![b](https://github.com/user-attachments/assets/b2)"
	twice := Scan(body, body)
	once := Scan(body)
	if len(twice) != len(once) {
		t.Fatalf("scanning the same body twice produced %d refs, once produced %d", len(twice), len(once))
	}
	for i := range once {
		if twice[i] != once[i] {
			t.Errorf("ref %d differs: %+v vs %+v", i, twice[i], once[i])
		}
	}
}

func TestScanKeepsBodyOrder(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"![one](https://github.com/user-attachments/assets/one)",
		"![two](https://github.com/user-attachments/assets/two)",
		"![three](https://github.com/user-attachments/assets/three)",
	}, "\n\n")

	refs := Scan(body)
	want := []string{"one", "two", "three"}
	if len(refs) != len(want) {
		t.Fatalf("got %d refs, want %d", len(refs), len(want))
	}
	for i, name := range want {
		if refs[i].Name != name {
			t.Errorf("ref %d = %q, want %q", i, refs[i].Name, name)
		}
	}
}

func TestScanHandlesEmptyInput(t *testing.T) {
	t.Parallel()

	if refs := Scan(); len(refs) != 0 {
		t.Errorf("Scan() = %+v, want none", refs)
	}
	if refs := Scan("", "   \n\t "); len(refs) != 0 {
		t.Errorf("Scan(blank) = %+v, want none", refs)
	}
}
