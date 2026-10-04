package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// --- decode helpers -------------------------------------------------------
//
// Decode the emitted basic strings independently using JSON's shared TOML
// escape subset, so Go-only escapes such as \a, \v and \x cannot pass.

// mcpKV extracts the mcp_servers.litefactory.* "-c" pairs from an argv slice
// (either mcpOverrides' own return value or a full args() slice) into a
// key->raw-encoded-value map.
func mcpKV(t *testing.T, argv []string) map[string]string {
	t.Helper()
	const prefix = "mcp_servers." + mcpServerName + "."
	kv := make(map[string]string)
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] != "-c" || !strings.HasPrefix(argv[i+1], prefix) {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(argv[i+1], prefix), "=")
		if !ok {
			t.Fatalf("malformed mcp override %q", argv[i+1])
		}
		kv[key] = value
	}
	return kv
}

// decodeQuoted accepts the JSON/TOML basic-string subset used by the host.
func decodeQuoted(t *testing.T, s string) string {
	t.Helper()
	var v string
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode host string %q: %v", s, err)
	}
	return v
}

// decodeQuotedList decodes a basic-string array,
// splitting only on commas outside a quoted literal so a decoded value that
// itself contains a comma cannot be mistaken for a list boundary.
func decodeQuotedList(t *testing.T, s string) []string {
	t.Helper()
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		t.Fatalf("not a list literal: %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	var tokens []string
	var cur strings.Builder
	inQuote, escaped := false, false
	for _, r := range inner {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case inQuote && r == '\\':
			cur.WriteRune(r)
			escaped = true
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			tokens = append(tokens, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	tokens = append(tokens, cur.String())
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		out[i] = decodeQuoted(t, tok)
	}
	return out
}

// mustSpec builds a valid *agent.MCPLaunchSpec or fails the test immediately;
// every case below needs one that survives Validate() before mcpOverrides is
// ever reached.
func mustSpec(t *testing.T, executable, descriptor string, tools ...string) *agent.MCPLaunchSpec {
	t.Helper()
	spec, err := agent.NewMCPLaunchSpec(executable, descriptor, "attempt-ref", tools...)
	if err != nil {
		t.Fatalf("NewMCPLaunchSpec: %v", err)
	}
	return spec
}

// --- 1: quoted/backslash/newline values round-trip ------------------------

func TestMcpOverrides_RoundTripsQuotedBackslashAndNewlineValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		executable string
		descriptor string
	}{
		{
			name:       "quotes and backslash/round-trips exactly",
			executable: `/factory/quoted " ' backslash\bin/litefactory`,
			descriptor: `/private/quoted " ' backslash\dir/descriptor.json`,
		},
		{
			// filepath.Clean does not touch a newline embedded inside a path
			// component (it only resolves "/", "." and ".."), so
			// MCPLaunchSpec.Validate does not reject it either: it reaches
			// mcpOverrides and must be encoded, not silently mishandled.
			name:       "embedded newline/round-trips exactly",
			executable: "/factory/before\nafter/bin/litefactory",
			descriptor: "/private/before\nafter/dir/descriptor.json",
		},
		{
			name:       "TOML control escapes/round-trips exactly",
			executable: "/factory/before\a\b\t\v\f\r\x1f\x7fafter/bin/litefactory",
			descriptor: "/private/before\a\b\t\v\f\r\x1f\x7fafter/descriptor.json",
		},
		{
			name:       "unicode and shell metacharacters/round-trips exactly",
			executable: `/factory/quoted " München $(command)\bin/litefactory`,
			descriptor: `/private/quoted " München $(command)\dir/descriptor.json`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := mustSpec(t, tt.executable, tt.descriptor, "receipts_get")
			r := &Runner{}

			args, err := r.mcpOverrides(spec, true)
			if err != nil {
				t.Fatalf("mcpOverrides: %v", err)
			}

			kv := mcpKV(t, args)
			if got := decodeQuoted(t, kv["command"]); got != tt.executable {
				t.Errorf("command decoded = %q, want %q", got, tt.executable)
			}
			wantArgs := spec.Arguments()
			gotArgs := decodeQuotedList(t, kv["args"])
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Errorf("args decoded = %v, want %v", gotArgs, wantArgs)
			}
			// The descriptor path is only ever surfaced through args, never
			// its own key, so its round-trip is exercised via gotArgs above.
		})
	}
}

// A null byte is the one control character MCPLaunchSpec.Validate itself
// refuses in the executable/descriptor, so mcpOverrides never has a chance to
// mis-encode it: confirming the rejection at construction stands in for the
// "or confirm that rejection instead of silently mis-encoding" alternative
// this requirement calls for.
func TestMCPLaunchSpec_RejectsNulByteInExecutable(t *testing.T) {
	t.Parallel()
	if _, err := agent.NewMCPLaunchSpec("/factory/bad\x00exe", "/private/descriptor.json", "attempt"); err == nil {
		t.Fatal("nul byte in executable was accepted; want a validation error")
	}
}

// --- 2: exact catalogs ------------------------------------------------------

func TestMcpOverrides_EnabledToolsCatalogIsExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tools []string
	}{
		{"multiple tools/preserves exact order", []string{"receipts_get", "communication_preflight", "evidence_submit"}},
		{"single tool/no leakage", []string{"receipts_get"}},
		{"empty catalog/emits empty list", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", tt.tools...)
			r := &Runner{}

			args, err := r.mcpOverrides(spec, true)
			if err != nil {
				t.Fatalf("mcpOverrides: %v", err)
			}

			kv := mcpKV(t, args)
			if kv["env"] != "{}" {
				t.Fatalf("env override = %q, want empty minimal environment", kv["env"])
			}
			got := decodeQuotedList(t, kv["enabled_tools"])
			want := spec.PermittedTools()
			if len(got) != len(want) {
				t.Fatalf("enabled_tools = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("enabled_tools[%d] = %q, want %q (order matters)", i, got[i], want[i])
				}
			}
		})
	}
}

// --- 3: collisions -----------------------------------------------------------

func TestMcpOverrides_CollisionDetection(t *testing.T) {
	tests := []struct {
		name      string
		config    string // empty means no config.toml file at all
		wantError bool
	}{
		{
			name: "existing table header/refused",
			config: "[some_other_stuff]\n" +
				"foo = \"bar\"\n" +
				"\n" +
				"[mcp_servers.litefactory]\n" +
				"command = \"/already/here\"\n",
			wantError: true,
		},
		{
			name:      "existing dotted key/refused",
			config:    "mcp_servers.litefactory.command = \"/already/here\"\n",
			wantError: true,
		},
		{
			name: "unrelated content/passes through cleanly",
			config: "[mcp_servers.other]\n" +
				"command = \"/other/tool\"\n" +
				"\n" +
				"[some_section]\n" +
				"value = 1\n",
			wantError: false,
		},
		{
			name:      "no config file at all/passes through cleanly",
			config:    "",
			wantError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.config != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			r := &Runner{environ: []string{"CODEX_HOME=" + dir}}
			spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "receipts_get")

			args, err := r.mcpOverrides(spec, false)

			if tt.wantError {
				if err == nil {
					t.Fatal("collision was not refused")
				}
				if !errors.Is(err, agent.ErrInvalidRequest) {
					t.Errorf("error = %v, want it to wrap agent.ErrInvalidRequest", err)
				}
				if args != nil {
					t.Errorf("args = %v, want nil on refusal", args)
				}
				return
			}
			if err != nil {
				t.Fatalf("mcpOverrides: %v", err)
			}
			if len(args) != 12 {
				t.Fatalf("args = %v, want 6 -c pairs (12 elements)", args)
			}
		})
	}
}

// --- 4: enabled/disabled cold/resume/fallback paths -------------------------

func TestArgs_NilMCPLaunchIsAdditiveOnlyNoop(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	req := request(t, agent.PathCold)
	req.MCPLaunch = nil

	got, err := r.args(req, "workspace-write")
	if err != nil {
		t.Fatalf("args: %v", err)
	}

	want := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "workspace-write", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want byte-for-byte %v (no MCP registration must change nothing)", got, want)
	}
}

func TestArgs_EnabledMCPLaunchLandsBeforeModelOnColdPath(t *testing.T) {
	t.Parallel()
	spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "receipts_get")
	r := &Runner{model: "gpt-test"}
	req := request(t, agent.PathCold)
	req.MCPLaunch = spec

	got, err := r.args(req, "workspace-write")
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	if got := mcpKV(t, got)["required"]; got != "true" {
		t.Fatalf("MCP required = %q, want boolean true so startup fails if the server is unavailable", got)
	}
	if got := mcpKV(t, got)["default_tools_approval_mode"]; got != `"approve"` {
		t.Fatalf("MCP default_tools_approval_mode = %s, want \"approve\" so calls are not refused under approval policy never", got)
	}

	mcpArgs, err := r.mcpOverrides(spec, true)
	if err != nil {
		t.Fatalf("mcpOverrides: %v", err)
	}
	want := append([]string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "workspace-write"}, mcpArgs...)
	want = append(want, "--model", "gpt-test", "-")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestArgs_EnabledMCPLaunchLandsBeforeResumeSubcommand(t *testing.T) {
	t.Parallel()
	spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "receipts_get")
	r := &Runner{model: "gpt-test"}
	req := request(t, agent.PathResume)
	req.Session.Resume = "thread-99"
	req.MCPLaunch = spec

	got, err := r.args(req, "workspace-write")
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	if got := mcpKV(t, got)["required"]; got != "true" {
		t.Fatalf("MCP required = %q, want boolean true so resume fails if the server is unavailable", got)
	}

	mcpArgs, err := r.mcpOverrides(spec, true)
	if err != nil {
		t.Fatalf("mcpOverrides: %v", err)
	}
	want := append([]string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "workspace-write"}, mcpArgs...)
	want = append(want, "--model", "gpt-test", "resume", "thread-99", "-")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	joined := strings.Join(got, " ")
	if idx := strings.Index(joined, "mcp_servers"); idx == -1 || idx > strings.Index(joined, "resume thread-99") {
		t.Fatalf("mcp overrides do not precede the resume subcommand: %s", joined)
	}
}

// ignoreUserConfig true is the "fallback" isolation path the story names:
// the collision check is skipped because --ignore-user-config already keeps
// Codex from reading this file for the invocation, so a colliding config
// must not block the run.
func TestArgs_IgnoreUserConfigTrueSkipsCollisionCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	collidingConfig := "[mcp_servers.litefactory]\ncommand = \"/already/here\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(collidingConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// userConfig=false (default) forces ignoreUserConfig=true regardless of
	// req.Delivery, per args()'s own "!r.userConfig || req.Delivery != nil".
	r := &Runner{environ: []string{"CODEX_HOME=" + dir}}
	spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "receipts_get")
	req := request(t, agent.PathCold)
	req.MCPLaunch = spec

	got, err := r.args(req, "workspace-write")
	if err != nil {
		t.Fatalf("args: %v, want the colliding config ignored under --ignore-user-config", err)
	}
	if kv := mcpKV(t, got); kv["command"] == "" {
		t.Fatalf("args = %v, want mcp overrides present", got)
	}
}

// ignoreUserConfig false is the active-isolation path: the operator opted
// into their own config.toml (userConfig=true) on a request with no
// Delivery, so Codex will actually read that file and the collision check
// must run and refuse.
func TestArgs_IgnoreUserConfigFalseActivatesCollisionCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	collidingConfig := "[mcp_servers.litefactory]\ncommand = \"/already/here\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(collidingConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{userConfig: true, environ: []string{"CODEX_HOME=" + dir}}
	spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "receipts_get")
	req := request(t, agent.PathCold)
	req.MCPLaunch = spec
	req.Delivery = nil

	_, err := r.args(req, "workspace-write")

	if err == nil {
		t.Fatal("colliding config was not refused with the collision check active")
	}
	if !errors.Is(err, agent.ErrInvalidRequest) {
		t.Errorf("error = %v, want it to wrap agent.ErrInvalidRequest", err)
	}
}

// --- 5: Validate mode restrictions are unaffected ---------------------------

func TestArgs_ValidateModeRestrictionsUnaffectedByMCP(t *testing.T) {
	t.Parallel()
	spec := mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "receipts_get")
	r := &Runner{}
	req := request(t, agent.PathCold)
	req.Mode = "validate"
	req.MCPLaunch = spec

	got, err := r.args(req, "read-only")
	if err != nil {
		t.Fatalf("args: %v", err)
	}

	joined := strings.Join(got, " ")
	for _, want := range []string{"--sandbox read-only", "features.multi_agent=false"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s missing %s (MCP enablement must not widen Validate)", joined, want)
		}
	}
	if strings.Contains(joined, "features.shell_tool=false") {
		t.Fatalf("%s disables the shell, Codex Validate's only file reader", joined)
	}
	if strings.Contains(joined, "workspace-write") {
		t.Fatalf("%s widened Validate's read-only sandbox", joined)
	}
	kv := mcpKV(t, got)
	if kv["command"] == "" {
		t.Fatalf("args = %v, want mcp overrides still present alongside Validate's restrictions", got)
	}
	// features.* must still precede the mcp overrides: their relative
	// position in args() is unconditional on Mode=="validate", independent of
	// whether MCP is present at all.
	if idx := strings.Index(joined, "mcp_servers"); idx == -1 || idx < strings.Index(joined, "features.multi_agent") {
		t.Fatalf("features.* restrictions do not precede mcp overrides: %s", joined)
	}
}

func TestValidateMCPConfig_ReservedServerCollisions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		config    string
		wantError bool
	}{
		{"bare table inline server/refused", "[mcp_servers]\nlitefactory = { command = \"/other\" }\n", true},
		{"spaced table/refused", "[ mcp_servers . litefactory ]\ncommand = \"/other\"\n", true},
		{"quoted table/refused", "[\"mcp_servers\".'litefactory']\ncommand = \"/other\"\n", true},
		{"unicode escaped table/refused", "[mcp_servers.\"litefact\\u006fry\"]\ncommand = \"/other\"\n", true},
		{"quoted dotted key/refused", "'mcp_servers' . \"litefactory\" . command = \"/other\"\n", true},
		{"root inline server/refused", "mcp_servers = { litefactory = { command = \"/other\" } }\n", true},
		{"root inline dotted server/refused", "mcp_servers = { litefactory.command = \"/other\" }\n", true},
		{"parent table dotted server/refused", "[mcp_servers]\nlitefactory.command = \"/other\"\n", true},
		{"server array table/refused", "[[mcp_servers.litefactory]]\ncommand = \"/other\"\n", true},
		{"unterminated string/refused", "model = \"unfinished", true},
		{"unterminated table/refused", "[mcp_servers", true},
		{"excessive nesting/refused", "value = " + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65), true},
		{"oversized config/refused", strings.Repeat("#", (1<<20)+1), true},
		{"invalid UTF8/refused", "model = \"bad\xff\"", true},
		{"multiline string syntax text/accepted", "description = \"\"\"\n[mcp_servers.litefactory]\ncommand = '/other'\n\"\"\"\n", false},
		{"empty containers/accepted", "options = []\nmetadata = {}\n", false},
		{"table reset/accepted", "[mcp_servers]\nother = {}\n[unrelated]\nlitefactory = {}\n", false},
		{"empty config/accepted", "", false},
		{"unrelated server/accepted", "[mcp_servers.other]\ncommand = \"/other\"\n", false},
		{"unrelated inline server/accepted", "mcp_servers = { other = { command = \"/other\" } }\n", false},
		{"similar server name/accepted", "[mcp_servers.litefactory_extra]\ncommand = \"/other\"\n", false},
		{"commented reserved table/accepted", "# [mcp_servers.litefactory]\nmodel = \"example\"\n", false},
		{"string containing reserved table/accepted", "model = \"[mcp_servers.litefactory]\"\n", false},
		{"quoted dot is key text/accepted", "[\"mcp_servers.litefactory\"]\ncommand = \"/other\"\n", false},
		{"nested unrelated server/accepted", "[profiles.example.mcp_servers.litefactory]\ncommand = \"/other\"\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateMCPConfig([]byte(tt.config))
			if tt.wantError {
				if !errors.Is(err, agent.ErrInvalidRequest) {
					t.Fatalf("error = %v, want agent.ErrInvalidRequest", err)
				}
			} else if err != nil {
				t.Fatalf("unrelated config refused: %v", err)
			}
		})
	}
}

func TestMcpOverrides_InvalidUTF8Rejected(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, executable, descriptor string }{
		{"executable", "/factory/bad\xff/litefactory", "/private/descriptor.json"},
		{"descriptor", "/factory/litefactory", "/private/bad\xff/descriptor.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := mustSpec(t, tt.executable, tt.descriptor, "receipts_get")
			args, err := (&Runner{}).mcpOverrides(spec, true)
			if !errors.Is(err, agent.ErrInvalidRequest) || args != nil {
				t.Fatalf("invalid UTF8: args = %v, error = %v; want nil args and invalid request", args, err)
			}
		})
	}
}
