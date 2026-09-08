package teampublish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// ByteBenchmark records deterministic source measurements, never inferred tokens.
type ByteBenchmark struct {
	Measurement           string      `json:"measurement"`
	BaselineFixtureSHA256 string      `json:"baseline_fixture_sha256"`
	BaselineFiles         int         `json:"baseline_files"`
	BaselineBytes         int         `json:"baseline_bytes"`
	Modes                 []ModeBytes `json:"modes"`
}

// ModeBytes identifies one mode's selected source closure and its measured size.
type ModeBytes struct {
	Mode           string `json:"mode"`
	Agents         int    `json:"agents"`
	Files          int    `json:"files"`
	Bytes          int    `json:"bytes"`
	SnapshotSHA256 string `json:"snapshot_sha256"`
}

// Measure uses the real transitive capture, so a newly declared skill dependency
// changes benchmark results without maintaining a parallel dependency walker.
func Measure(ctx context.Context, source, baseline, name string) (_ *ByteBenchmark, err error) {
	root, err := os.OpenRoot(filepath.Dir(baseline))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	f, err := root.Open(filepath.Base(baseline))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	b, err := io.ReadAll(io.LimitReader(f, 32<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 32<<20 {
		return nil, fmt.Errorf("baseline fixture exceeds size bound")
	}
	var fixture struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		return nil, err
	}
	if len(fixture.Files) == 0 {
		return nil, fmt.Errorf("empty baseline fixture")
	}
	result := &ByteBenchmark{Measurement: "Captured source bytes including index.json; not delivered prompt bytes, token usage or latency.", BaselineFixtureSHA256: team.HashBytes(b), BaselineFiles: len(fixture.Files)}
	for _, b := range fixture.Files {
		result.BaselineBytes += len(b)
	}
	err = team.WithCatalogLock(ctx, source, func() error {
		for _, mode := range team.Modes() {
			s, err := teamsource.CaptureUnlocked(ctx, source, name, mode)
			if err != nil {
				return err
			}
			m := ModeBytes{Mode: mode, Agents: len(s.Agents), Files: len(s.Files), SnapshotSHA256: s.Digest}
			for _, b := range s.Files {
				m.Bytes += len(b)
			}
			result.Modes = append(result.Modes, m)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
