package git

import (
	"context"
	"crypto/sha1" // #nosec G505 -- Required to verify historical Git object identities.
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// JudgeRepositoryState is a read-only identity and inventory for evidence capture.
type JudgeRepositoryState struct {
	Root, Commit, StateDigest string
	Paths                     []string
	ChangedPaths              map[string]bool
	CommittedBlobs            map[string]string
}

// CaptureJudgeRepository observes HEAD, status and the nonignored inventory without writing an index.
func CaptureJudgeRepository(ctx context.Context, b *Binary, root string) (JudgeRepositoryState, error) {
	clone, err := b.Open(ctx, root)
	if err != nil {
		return JudgeRepositoryState{}, err
	}
	head, err := clone.runner.run(ctx, readTimeout, "rev-parse", "HEAD")
	if err != nil {
		return JudgeRepositoryState{}, err
	}
	status, err := clone.runner.runRaw(ctx, readTimeout, statusArgs...)
	if err != nil {
		return JudgeRepositoryState{}, err
	}
	files, err := clone.runner.runRaw(ctx, readTimeout, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return JudgeRepositoryState{}, err
	}
	changes, err := parseStatus(status)
	if err != nil {
		return JudgeRepositoryState{}, err
	}
	changed := map[string]bool{}
	for _, change := range changes {
		changed[change.Path] = true
		if change.From != "" {
			changed[change.From] = true
		}
	}
	tree, err := clone.runner.runRaw(ctx, readTimeout, "ls-tree", "-rz", "--full-tree", head)
	if err != nil {
		return JudgeRepositoryState{}, err
	}
	blobs := map[string]string{}
	for _, entry := range strings.Split(tree, "\x00") {
		metadata, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(metadata)
		if ok && len(fields) == 3 && fields[1] == "blob" {
			blobs[path] = fields[2]
		}
	}
	sum := sha256.Sum256([]byte(head + "\x00" + status + "\x00" + files))
	return JudgeRepositoryState{Root: clone.path, Commit: head, StateDigest: hex.EncodeToString(sum[:]), ChangedPaths: changed, CommittedBlobs: blobs, Paths: strings.Split(strings.TrimSuffix(files, "\x00"), "\x00")}, nil
}

// JudgeBlobMatches checks captured bytes against a committed Git blob identity.
// SHA-1 reproduces Git's historical object address; evidence integrity uses SHA-256 separately.
func JudgeBlobMatches(objectID string, raw []byte) bool {
	framed := append([]byte(fmt.Sprintf("blob %d\x00", len(raw))), raw...)
	if len(objectID) == 40 {
		sum := sha1.Sum(framed) // #nosec G401 -- Git SHA-1 object identity compatibility, not a security hash.
		return hex.EncodeToString(sum[:]) == objectID
	}
	if len(objectID) == 64 {
		sum := sha256.Sum256(framed)
		return hex.EncodeToString(sum[:]) == objectID
	}
	return false
}
