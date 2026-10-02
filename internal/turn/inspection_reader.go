package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/cloudvane/litefactory/internal/agent"
)

const inspectionPageBytes = 32 << 10
const inspectionFileBytes = 2 << 20
const inspectionMaxFiles = 10000

// inspectionReader contains only factory-admitted bytes or a rooted repository
// handle. It never evaluates expressions, starts processes or opens a write fd.
// The dispatcher checks the attempt lease before every operation.
type inspectionReader struct {
	root  *os.Root
	files map[string]map[string][]byte
}

type inspectionQuery struct {
	Reference string `json:"reference"`
	Path      string `json:"path,omitempty"`
	Query     string `json:"query,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type inspectionReply struct {
	References []string `json:"references,omitempty"`
	Paths      []string `json:"paths,omitempty"`
	Content    []byte   `json:"content,omitempty"`
	SHA256     string   `json:"sha256,omitempty"`
	Next       int      `json:"next,omitempty"`
	EOF        bool     `json:"eof"`
}

func newRetainedInspection(v *agent.ValidationContext) (*inspectionReader, error) {
	r := &inspectionReader{files: map[string]map[string][]byte{}}
	if v == nil {
		return r, nil
	}
	for _, s := range v.Stories {
		if s.Evidence == nil {
			continue
		}
		if s.Snapshot == "" || agent.SnapshotDigest(s.Evidence.Files) != s.Snapshot {
			return nil, errors.New("inspection snapshot digest mismatch")
		}
		if len(s.Evidence.Files) > inspectionMaxFiles {
			return nil, errors.New("inspection snapshot inventory exceeds limit")
		}
		files := make(map[string][]byte)
		for _, f := range s.Evidence.Files {
			if !inspectionPath(f.Path) || len(f.Content) > inspectionFileBytes {
				return nil, errors.New("inspection snapshot path or size refused")
			}
			if f.Deleted {
				continue
			}
			if agent.StoryDigest(f.Content) != f.SHA256 {
				return nil, errors.New("inspection snapshot content mismatch")
			}
			if _, duplicate := files[f.Path]; duplicate {
				return nil, errors.New("inspection snapshot has duplicate path")
			}
			files[f.Path] = bytes.Clone(f.Content)
		}
		r.files[s.Snapshot] = files
	}
	return r, nil
}

func newRepositoryInspection(root string) (*inspectionReader, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &inspectionReader{root: r, files: map[string]map[string][]byte{}}, nil
}

func (r *inspectionReader) close() error {
	if r != nil && r.root != nil {
		return r.root.Close()
	}
	return nil
}

func inspectionPath(p string) bool {
	if !fs.ValidPath(p) || p == "." || strings.ContainsAny(p, "\\\x00") {
		return false
	}
	for _, component := range strings.Split(p, "/") {
		if component == ".git" || component == ".litefactory" {
			return false
		}
	}
	return true
}

func (r *inspectionReader) material() string {
	if r == nil {
		return ""
	}
	if r.root != nil {
		return "repository:" + r.root.Name()
	}
	refs := make([]string, 0, len(r.files))
	for ref := range r.files {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return "retained:" + strings.Join(refs, ",")
}

func (r *inspectionReader) paths(ctx context.Context, ref string) ([]string, error) {
	var paths []string
	if r.root == nil {
		files, ok := r.files[ref]
		if !ok {
			return nil, errors.New("inspection reference is not admitted for this attempt")
		}
		for p := range files {
			paths = append(paths, p)
		}
	} else {
		if ref != "repository" {
			return nil, errors.New("inspection reference is not admitted for this attempt")
		}
		visited := 0
		err := fs.WalkDir(r.root.FS(), ".", func(p string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			visited++
			if visited > inspectionMaxFiles {
				return errors.New("inspection repository inventory exceeds limit")
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if p != "." && (!inspectionPath(p) || entry.Type()&os.ModeSymlink != 0) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() {
				paths = append(paths, p)
			}
			if len(paths) > inspectionMaxFiles {
				return errors.New("inspection repository inventory exceeds limit")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func (r *inspectionReader) read(ref, name string) ([]byte, error) {
	if !inspectionPath(name) {
		return nil, errors.New("inspection requires an admitted relative path")
	}
	if r.root == nil {
		body, ok := r.files[ref][name]
		if !ok {
			return nil, errors.New("inspection file is not admitted for this attempt")
		}
		return body, nil
	}
	if ref != "repository" {
		return nil, errors.New("inspection reference is not admitted for this attempt")
	}
	// Lstat refuses symlink components; os.Root also prevents a concurrent
	// replacement from escaping containment between this check and Open.
	for p := name; p != "."; p = path.Dir(p) {
		info, err := r.root.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (p == name && !info.Mode().IsRegular()) {
			return nil, errors.New("inspection refuses symlinks")
		}
	}
	f, err := r.root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > inspectionFileBytes {
		return nil, errors.Join(errors.New("inspection requires a bounded regular file"), err, f.Close())
	}
	body, err := io.ReadAll(io.LimitReader(f, inspectionFileBytes+1))
	err = errors.Join(err, f.Close())
	if len(body) > inspectionFileBytes {
		return nil, errors.New("inspection file exceeds limit")
	}
	return body, err
}

func (r *inspectionReader) handle(ctx context.Context, operation string, data []byte) (json.RawMessage, error) {
	if r == nil {
		return nil, errors.New("inspection service unavailable")
	}
	if len(data) > 8192 {
		return nil, errors.New("inspection request exceeds limit")
	}
	var q inspectionQuery
	if err := decodeMCPArguments(data, &q); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.Offset < 0 || q.Limit < 0 || q.Limit > inspectionPageBytes || len(q.Query) > 256 {
		return nil, errors.New("inspection request bounds refused")
	}
	limit := q.Limit
	if limit == 0 {
		limit = inspectionPageBytes
	}
	var reply inspectionReply
	switch operation {
	case "inspection_read":
		if q.Query != "" {
			return nil, errors.New("read does not accept query")
		}
		body, err := r.read(q.Reference, q.Path)
		if err != nil {
			return nil, err
		}
		if q.Offset > len(body) {
			return nil, errors.New("inspection offset exceeds content")
		}
		end := q.Offset + min(limit, len(body)-q.Offset)
		reply.Content, reply.SHA256, reply.Next, reply.EOF = body[q.Offset:end], agent.StoryDigest(body), end, end == len(body)
	case "inspection_list", "inspection_search":
		if q.Path != "" {
			return nil, errors.New("list and search do not accept path")
		}
		if q.Reference == "" && operation == "inspection_list" {
			for ref := range r.files {
				reply.References = append(reply.References, ref)
			}
			if r.root != nil {
				reply.References = append(reply.References, "repository")
			}
			slices.Sort(reply.References)
			reply.EOF = true
			break
		}
		paths, err := r.paths(ctx, q.Reference)
		if err != nil {
			return nil, err
		}
		if q.Offset > len(paths) {
			return nil, errors.New("inspection offset exceeds inventory")
		}
		if operation == "inspection_search" && q.Query == "" {
			return nil, errors.New("inspection search requires a literal query")
		}
		budget := 8 << 20
		for reply.Next = q.Offset; reply.Next < len(paths) && len(reply.Paths) < min(limit, 100); reply.Next++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			p := paths[reply.Next]
			if operation == "inspection_search" {
				body, err := r.read(q.Reference, p)
				if err != nil {
					return nil, fmt.Errorf("search %s: %w", p, err)
				}
				budget -= len(body)
				if budget < 0 {
					break
				}
				if !bytes.Contains(body, []byte(q.Query)) {
					continue
				}
			} else if q.Query != "" && !strings.Contains(p, q.Query) {
				continue
			}
			reply.Paths = append(reply.Paths, p)
		}
		reply.EOF = reply.Next == len(paths)
	default:
		return nil, errors.New("unknown inspection operation")
	}
	return json.Marshal(reply)
}
