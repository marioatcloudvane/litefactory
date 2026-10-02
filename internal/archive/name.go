package archive

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// NameTimeLayout is the UTC timestamp embedded in every archive file name.
const NameTimeLayout = "20060102T150405Z"

// Suffixes of the final and in-progress files.
const (
	FileSuffix    = ".jsonl.gz"
	PartialSuffix = ".partial"
)

var namePattern = regexp.MustCompile(`^([0-9]+)-([0-9]{8}T[0-9]{6}Z)\.jsonl\.gz$`)

// BuildName returns "<issue>-<timestamp>.jsonl.gz" with t truncated to seconds
// in UTC, which is exactly the value the header's archived_at must carry.
func BuildName(issue int, t time.Time) string {
	return fmt.Sprintf("%d-%s%s", issue, t.UTC().Format(NameTimeLayout), FileSuffix)
}

// ParseName reverses [BuildName]. It rejects anything that is not exactly a
// final archive file name, including ".partial" files.
func ParseName(name string) (issue int, t time.Time, err error) {
	m := namePattern.FindStringSubmatch(name)
	if m == nil {
		return 0, time.Time{}, fmt.Errorf("archive: %q is not an archive file name", name)
	}
	issue, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("archive: issue number in %q: %w", name, err)
	}
	t, err = time.Parse(NameTimeLayout, m[2])
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("archive: timestamp in %q: %w", name, err)
	}
	return issue, t.UTC(), nil
}

// TaskDir is the directory that holds every archive of one issue.
func TaskDir(root, forge, owner, repo string, issue int) string {
	return filepath.Join(root, forge, owner, repo, strconv.Itoa(issue))
}
