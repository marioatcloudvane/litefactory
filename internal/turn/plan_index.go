package turn

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// planIndex pins a validated directory and original file identity across forge
// publication. All IO is capability-rooted; replacement never follows a target
// symlink, and changed paths/content fail instead of overwriting newer edits.
type planIndex struct {
	dir        *os.Root
	path       string
	info       os.FileInfo
	original   []byte
	start, end int
	// lead separates an appended Story Index from the existing text. It is
	// empty whenever the file already had a Story Index section, so that
	// replacement stays byte-identical to before.
	lead string
}

func openPlanIndex(root *os.Root, path string) (*planIndex, error) {
	target := filepath.Join(path, planbrief.FileName)
	if err := checkStoryComponents(root, target); err != nil {
		return nil, err
	}
	dir, err := root.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	p, err := readPlanIndex(dir, path)
	if err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	if err := p.unchanged(root); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	return p, nil
}

func readPlanIndex(dir *os.Root, path string) (*planIndex, error) {
	before, err := dir.Lstat(planbrief.FileName)
	if err != nil {
		return nil, err
	}
	original, err := readStoryFile(dir, planbrief.FileName, planbrief.MaxProposalBytes)
	if err != nil {
		return nil, err
	}
	p := &planIndex{dir: dir, path: path, info: before, original: original}
	p.start = strings.Index(string(original), "## Story Index")
	if p.start < 0 {
		// The factory owns this section. A plan_brief.md rewritten without it
		// (for example by an agent) gets a fresh section appended at the end,
		// instead of failing every save the same way. The rest of the file and
		// every identity and concurrent-change check stay exactly as they are.
		p.start, p.end = len(original), len(original)
		switch {
		case len(original) == 0 || bytes.HasSuffix(original, []byte("\n\n")):
		case bytes.HasSuffix(original, []byte("\n")):
			p.lead = "\n"
		default:
			p.lead = "\n\n"
		}
		return p, nil
	}
	p.end = len(original)
	if next := strings.Index(string(original[p.start+len("## Story Index"):]), "\n## "); next >= 0 {
		p.end = p.start + len("## Story Index") + next + 1
	}
	return p, nil
}

func (p *planIndex) unchanged(root *os.Root) error {
	if err := checkStoryComponents(root, filepath.Join(p.path, planbrief.FileName)); err != nil {
		return err
	}
	currentDir, err := root.Lstat(p.path)
	if err != nil {
		return err
	}
	pinnedDir, err := p.dir.Stat(".")
	if err != nil {
		return err
	}
	current, err := p.dir.Lstat(planbrief.FileName)
	if err != nil {
		return err
	}
	if !os.SameFile(currentDir, pinnedDir) || !os.SameFile(current, p.info) {
		return fmt.Errorf("plan index destination changed during publication")
	}
	data, err := readStoryFile(p.dir, planbrief.FileName, planbrief.MaxProposalBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, p.original) {
		return fmt.Errorf("plan index contents changed during publication")
	}
	return nil
}

func (p *planIndex) replace(root *os.Root, index []byte) (err error) {
	if err := p.unchanged(root); err != nil {
		return err
	}
	temp := ".litefactory-plan-index-" + rand.Text()
	f, err := p.dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, planbriefFileMode)
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := p.dir.Remove(temp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	updated := string(p.original[:p.start]) + p.lead + string(index) + "\n" + string(p.original[p.end:])
	_, writeErr := f.WriteString(updated)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	if err := p.unchanged(root); err != nil {
		return err
	}
	return p.dir.Rename(temp, planbrief.FileName)
}
