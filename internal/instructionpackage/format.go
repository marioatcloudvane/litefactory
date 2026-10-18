package instructionpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	// SchemaVersion is the supported package and capture format.
	SchemaVersion = 1
	// MaxFileBytes bounds each metadata, catalogue, and role file.
	MaxFileBytes = 1 << 20
	// MaxCaptureBytes bounds total validated content.
	MaxCaptureBytes = 16 << 20
	// MaxFiles bounds the shipped inventory, including metadata and catalogue.
	MaxFiles = 4096
	// MetadataPath carries schema and shipped version identity.
	MetadataPath = "metadata.yaml"
	// CataloguePath lists callable role names as a YAML sequence.
	CataloguePath = "catalogue.yaml"
)

var roleNamePattern = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)

type metadata struct {
	Schema  int    `yaml:"schema"`
	Version string `yaml:"version"`
}

func yamlDocument(raw []byte) (*yaml.Node, error) {
	if len(raw) > MaxFileBytes {
		return nil, fmt.Errorf("YAML exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("YAML is not UTF-8")
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode trailing YAML: %w", err)
		}
		return nil, fmt.Errorf("multiple YAML documents")
	}
	if len(doc.Content) != 1 {
		return nil, fmt.Errorf("empty YAML document")
	}
	var check func(*yaml.Node) error
	check = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return fmt.Errorf("YAML aliases and anchors are unsupported")
		}
		for _, c := range n.Content {
			if err := check(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(&doc); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func parseMetadata(raw []byte) (metadata, error) {
	var out metadata
	n, err := yamlDocument(raw)
	if err != nil {
		return out, err
	}
	if n.Kind != yaml.MappingNode {
		return out, fmt.Errorf("metadata must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		v := n.Content[i+1]
		if k.Tag != "!!str" || (k.Value != "schema" && k.Value != "version") || seen[k.Value] {
			return out, fmt.Errorf("unknown or duplicate metadata field")
		}
		seen[k.Value] = true
		if k.Value == "schema" && v.Tag != "!!int" {
			return out, fmt.Errorf("schema must be an integer")
		}
		if k.Value == "version" && v.Tag != "!!str" {
			return out, fmt.Errorf("version must be a string")
		}
	}
	if err := n.Decode(&out); err != nil {
		return out, fmt.Errorf("decode metadata: %w", err)
	}
	if out.Schema != SchemaVersion {
		return out, fmt.Errorf("unsupported package schema %d", out.Schema)
	}
	if strings.TrimSpace(out.Version) == "" || strings.TrimSpace(out.Version) != out.Version {
		return out, fmt.Errorf("package version must be nonblank without surrounding whitespace")
	}
	return out, nil
}

func parseCatalogue(raw []byte) ([]string, error) {
	n, err := yamlDocument(raw)
	if err != nil {
		return nil, err
	}
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("catalogue must be a YAML sequence")
	}
	if len(n.Content) > MaxFiles-2 {
		return nil, fmt.Errorf("catalogue exceeds inventory limit")
	}
	roles := make([]string, 0, len(n.Content))
	seen := map[string]bool{}
	for _, entry := range n.Content {
		if entry.Kind != yaml.ScalarNode || entry.Tag != "!!str" || !roleNamePattern.MatchString(entry.Value) {
			return nil, fmt.Errorf("catalogue contains an invalid agent name")
		}
		if seen[entry.Value] {
			return nil, fmt.Errorf("catalogue contains duplicate agent %q", entry.Value)
		}
		seen[entry.Value] = true
		roles = append(roles, entry.Value)
	}
	return roles, nil
}

func rolePath(name string) string { return "agents/" + name + ".md" }
func validateText(raw []byte) error {
	if len(raw) > MaxFileBytes {
		return fmt.Errorf("role exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(raw) {
		return fmt.Errorf("role is not UTF-8")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("role is blank")
	}
	return nil
}

func contentDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	var framing [8]byte
	for _, name := range names {
		binary.BigEndian.PutUint64(framing[:], uint64(len(name)))
		_, _ = h.Write(framing[:])
		_, _ = h.Write([]byte(name))
		binary.BigEndian.PutUint64(framing[:], uint64(len(files[name])))
		_, _ = h.Write(framing[:])
		_, _ = h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}
