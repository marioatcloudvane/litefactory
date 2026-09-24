package render

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// NativeVersion participates in conversation identity: renderer changes must
// never silently alter the instructions behind a resumable conversation.
const NativeVersion = "native-1"

var nativeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// NativeRole is an already-selected specialist with a locally resolved model.
// Tools is bounded by the parent mode before reaching this pure renderer.
type NativeRole struct {
	Source ResolvedSource
	Model  string
	Tools  []string
	Skills []string
	// InheritTools renders a Claude role without a tools line, so it gets the
	// session's tools, attempt MCP tools included, within the --tools ceiling.
	// A written list is an allowlist: materializing the native ceiling there
	// hid the MCP tools from every graph node. Tools is ignored when set.
	InheritTools bool
}

// Native renders file-backed native roles, not skill-shaped approximations of
// agents. No specialist prompt belongs on a process command line.
func Native(kind string, role NativeRole, sandbox string) (RenderedFile, error) {
	fm := role.Source.Frontmatter
	if !nativeName.MatchString(fm.Name) || strings.TrimSpace(role.Model) == "" {
		return RenderedFile{}, fmt.Errorf("native role needs a safe name and resolved model")
	}
	if len(fm.Extra) != 0 {
		return RenderedFile{}, fmt.Errorf("native role %s has unsupported frontmatter", fm.Name)
	}
	switch kind {
	case "claude":
		var tools *string
		if !role.InheritTools {
			joined := strings.Join(role.Tools, ",")
			tools = &joined
		}
		header, err := yaml.Marshal(struct {
			Name        string   `yaml:"name"`
			Description string   `yaml:"description"`
			Model       string   `yaml:"model"`
			Tools       *string  `yaml:"tools,omitempty"`
			Skills      []string `yaml:"skills,omitempty"`
		}{fm.Name, fm.Description, role.Model, tools, role.Skills})
		if err != nil {
			return RenderedFile{}, fmt.Errorf("render native role: %w", err)
		}
		return RenderedFile{Path: "plugin/agents/" + fm.Name + ".md", Bytes: []byte("---\n" + string(header) + "---\n\n" + strings.TrimSpace(role.Source.Body) + "\n")}, nil
	case "codex":
		if sandbox != "read-only" && sandbox != "workspace-write" {
			return RenderedFile{}, fmt.Errorf("native role has invalid sandbox %q", sandbox)
		}
		// JSON string escaping is also valid TOML basic-string escaping for
		// these UTF-8 fields, including embedded newlines and quotes.
		quote := func(s string) string { raw, _ := json.Marshal(s); return string(raw) }
		body := "model = " + quote(role.Model) + "\nsandbox_mode = " + quote(sandbox) + "\ndeveloper_instructions = " + quote(role.Source.Body) + "\n"
		return RenderedFile{Path: "roles/" + fm.Name + ".toml", Bytes: []byte(body)}, nil
	default:
		return RenderedFile{}, fmt.Errorf("no native role renderer for %q", kind)
	}
}
