package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/denypolicy"
)

func claudeDenyRules(policy *denypolicy.Policy, worktree string) ([]string, error) {
	if !policy.Selected() {
		return nil, nil
	}
	if len(policy.Rules) == 0 {
		return nil, fmt.Errorf("selected deny groups %v contain no resolved entries", policy.Groups)
	}
	var rules []string
	for _, rule := range policy.Rules {
		compiled, err := claudeDenyRule(rule, worktree)
		if err != nil {
			return nil, fmt.Errorf("%s: deny group %q entry %d %q: Claude cannot enforce this entry: %w", rule.Selection, rule.Group, rule.Index, rule.Original, err)
		}
		if !slices.Contains(rules, compiled) {
			rules = append(rules, compiled)
		}
	}
	return rules, nil
}

func claudeDenyRule(rule denypolicy.Rule, worktree string) (string, error) {
	parsed, err := denypolicy.ParseRule(rule.Original)
	if err != nil {
		return "", err
	}
	if parsed.Tool != rule.Tool || parsed.Pattern != rule.Pattern {
		return "", errors.New("resolved entry does not match its original rule")
	}
	pattern := rule.Pattern
	switch rule.Tool {
	case "Bash":
		// Claude's supported command forms are literal/wildcard patterns and
		// the native trailing :* prefix notation; parameter selectors are not
		// command patterns. Do not invent alternate invocation forms.
		if strings.Contains(strings.TrimSuffix(pattern, ":*"), ":") || strings.ContainsAny(pattern, "()[]{}?\\") {
			return "", errors.New("use a native Bash literal or * wildcard pattern, optionally ending in :*")
		}
	case "Agent":
		if pattern == "" {
			return "Agent", nil
		}
		if strings.ContainsFunc(pattern, func(ch rune) bool {
			return !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '-' && ch != '_' && ch != '.'
		}) {
			return "", errors.New("use Agent or Agent(name) with a literal native subagent name, without parameter selectors or wildcards")
		}
	case "Read", "Edit":
		if strings.HasPrefix(pattern, "!") {
			return "", errors.New("file negation is not an additive deny; use an explicit positive file pattern")
		}
		if strings.ContainsAny(pattern, "()[]{}?\\:") || strings.HasPrefix(pattern, "~") && !strings.HasPrefix(pattern, "~/") {
			return "", errors.New("use a file path with literal components, * or ** globs, and an optional // or ~/ anchor")
		}
		switch {
		case strings.HasPrefix(pattern, "//"), strings.HasPrefix(pattern, "~/"):
			// Native absolute and home-relative anchors retain their meaning.
		case strings.HasPrefix(pattern, "/"):
			return "", errors.New("a single / is settings-source-relative; use // for an absolute path or a worktree-relative path")
		default:
			if !filepath.IsAbs(worktree) {
				return "", errors.New("worktree must be absolute to anchor relative file rules")
			}
			if strings.ContainsAny(worktree, "*?[]{}\\():") {
				return "", errors.New("worktree contains native glob characters or parameter/delimiter syntax; use a path without native pattern syntax")
			}
			// Cleaning glob components can change their matching meaning. Only
			// leading ./ is harmless; reject traversal instead of rewriting it.
			pattern = strings.TrimPrefix(pattern, "./")
			for _, component := range strings.Split(pattern, "/") {
				if component == ".." || component == "." || component == "" {
					return "", errors.New("file paths must not contain empty, . or .. components")
				}
			}
			// Native deny patterns containing only a filename, or one directory
			// followed by /**, match at every depth under cwd. An explicit
			// absolute anchor must retain that implicit depth while fixing cwd
			// to this attempt's launch worktree.
			if !strings.Contains(pattern, "/") && pattern != "**" || strings.Count(pattern, "/") == 1 && strings.HasSuffix(pattern, "/**") && !strings.HasPrefix(pattern, "**/") {
				pattern = "**/" + pattern
			}
			pattern = "/" + filepath.ToSlash(filepath.Clean(worktree)) + "/" + pattern
		}
	default:
		return "", fmt.Errorf("unsupported tool %q", rule.Tool)
	}
	return rule.Tool + "(" + pattern + ")", nil
}

// prepareDenySettings creates a private settings snapshot. It is outside every
// writable sandbox root, and its own file-tool edit deny also protects shell-free
// runs. The ordinary no-selection settings path never enters this function.
func (r *Runner) prepareDenySettings(ctx context.Context, req agent.Request, logPath string, sandboxed bool) (runSandbox, error) {
	if err := ctx.Err(); err != nil {
		return runSandbox{}, err
	}
	rules, err := claudeDenyRules(req.DenyPolicy, req.WorktreePath)
	if err != nil {
		return runSandbox{}, err
	}
	var document map[string]any
	var env []string
	baseCleanup := func() error { return nil }
	if sandboxed {
		document, env, baseCleanup, err = r.sandboxDocument(req)
		if err != nil {
			return runSandbox{}, err
		}
	} else {
		// #nosec G304 -- settingsPath is validated operator-owned configuration.
		raw, readErr := os.ReadFile(r.settingsPath)
		if readErr != nil {
			return runSandbox{}, fmt.Errorf("read selected Claude settings: %w", readErr)
		}
		document, err = decodeDenySettings(raw)
		if err != nil {
			return runSandbox{}, err
		}
	}
	fail := func(err error) (runSandbox, error) { return runSandbox{}, errors.Join(err, baseCleanup()) }
	if err := anchorSettingsFileRules(document, filepath.Dir(r.settingsPath)); err != nil {
		return fail(err)
	}
	parent := filepath.Dir(logPath)
	if err := r.checkPolicyDirectory(req, parent); err != nil {
		return fail(err)
	}
	dir, err := os.MkdirTemp(parent, ".deny-")
	if err != nil {
		return fail(fmt.Errorf("create protected deny settings directory: %w", err))
	}
	cleanup := func() error { return errors.Join(os.RemoveAll(dir), baseCleanup()) }
	permissions, ok := document["permissions"].(map[string]any)
	if !ok {
		return runSandbox{}, errors.Join(errors.New("selected Claude settings permissions must be an object"), cleanup())
	}
	denies, ok := permissions["deny"].([]any)
	if !ok {
		return runSandbox{}, errors.Join(errors.New("selected Claude settings permissions.deny must be a string array"), cleanup())
	}
	shared := denyRules("//" + filepath.ToSlash(filepath.Clean(r.home)))
	shared = append(shared, "Edit(/"+filepath.ToSlash(dir)+"/**)")
	for _, entry := range append(shared, rules...) {
		if !slices.Contains(denies, any(entry)) {
			denies = append(denies, entry)
		}
	}
	permissions["deny"] = denies
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return runSandbox{}, errors.Join(err, cleanup())
	}
	path := filepath.Join(dir, "settings.json")
	if err := ctx.Err(); err != nil {
		return runSandbox{}, errors.Join(err, cleanup())
	}
	if err := writeFileAtomic(path, append(encoded, '\n'), settingsFileMode); err != nil {
		return runSandbox{}, errors.Join(fmt.Errorf("write selected Claude settings: %w", err), cleanup())
	}
	return runSandbox{settingsPath: path, env: env, cleanup: cleanup}, nil
}

// A selected graph judge retains its existing isolated settings and read-only
// tool ceiling. Only shared and selected denies are added, in a private atomic
// file; ordinary judge calls do not load workflow or operator permissions.
func (r *Runner) prepareJudgeDenySettings(ctx context.Context, worktree string, rules []string) (runSandbox, error) {
	if err := ctx.Err(); err != nil {
		return runSandbox{}, err
	}
	parent := r.paths.LogsDir()
	if err := os.MkdirAll(parent, settingsDirMode); err != nil {
		return runSandbox{}, fmt.Errorf("create selected judge settings directory: %w", err)
	}
	if err := r.checkPolicyDirectory(agent.Request{WorktreePath: worktree}, parent); err != nil {
		return runSandbox{}, err
	}
	dir, err := os.MkdirTemp(parent, ".judge-deny-")
	if err != nil {
		return runSandbox{}, fmt.Errorf("create protected judge deny settings directory: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(dir) }
	denies := denyRules("//" + filepath.ToSlash(filepath.Clean(r.home)))
	denies = append(denies, "Edit(/"+filepath.ToSlash(dir)+"/**)")
	denies = append(denies, rules...)
	data, err := json.Marshal(map[string]any{"permissions": map[string]any{"deny": denies}})
	if err != nil {
		return runSandbox{}, errors.Join(err, cleanup())
	}
	path := filepath.Join(dir, "settings.json")
	if err := ctx.Err(); err != nil {
		return runSandbox{}, errors.Join(err, cleanup())
	}
	if err := writeFileAtomic(path, append(data, '\n'), settingsFileMode); err != nil {
		return runSandbox{}, errors.Join(fmt.Errorf("write selected judge settings: %w", err), cleanup())
	}
	return runSandbox{settingsPath: path, cleanup: cleanup}, nil
}

// Native single-/ file rules anchor to the settings file's own directory.
// Rebase these before moving the document, in every permission list, so the
// attempt snapshot retains the original permissions' meaning.
func anchorSettingsFileRules(document map[string]any, sourceDir string) error {
	permissions, ok := document["permissions"].(map[string]any)
	if !ok {
		return errors.New("selected Claude settings permissions must be an object")
	}
	for _, key := range []string{"allow", "ask", "deny"} {
		value, exists := permissions[key]
		if !exists {
			continue
		}
		entries, ok := value.([]any)
		if !ok {
			return fmt.Errorf("selected Claude settings permissions.%s must be a string array", key)
		}
		for i, value := range entries {
			entry, ok := value.(string)
			if !ok {
				return fmt.Errorf("selected Claude settings permissions.%s must contain only strings", key)
			}
			tool, pattern, ok := strings.Cut(entry, "(")
			if !ok || tool != "Read" && tool != "Edit" || !strings.HasSuffix(pattern, ")") {
				continue
			}
			pattern = strings.TrimSuffix(pattern, ")")
			// Native leading-! negations stay cwd-relative even before /, ~/
			// or //, so their meaning is independent of the settings source.
			if !strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "//") {
				continue
			}
			if strings.ContainsAny(sourceDir, "*?[]{}\\():") {
				return fmt.Errorf("cannot preserve permissions.%s entry %q: original settings directory contains native glob characters or parameter/delimiter syntax", key, entry)
			}
			entries[i] = tool + "(/" + filepath.ToSlash(filepath.Clean(sourceDir)) + pattern + ")"
		}
	}
	return nil
}

func decodeDenySettings(raw []byte) (map[string]any, error) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode selected Claude settings: %w", err)
	}
	if document == nil {
		return nil, errors.New("selected Claude settings must be a JSON object")
	}
	if sandbox, exists := document["sandbox"]; exists {
		if value, ok := sandbox.(map[string]any); !ok || value == nil {
			return nil, errors.New("selected Claude settings sandbox must be an object")
		}
	}
	value, exists := document["permissions"]
	if !exists {
		value = map[string]any{}
		document["permissions"] = value
	}
	permissions, ok := value.(map[string]any)
	if !ok || permissions == nil {
		return nil, errors.New("selected Claude settings permissions must be an object")
	}
	for _, key := range []string{"deny", "allow", "ask"} {
		value, exists := permissions[key]
		if !exists {
			if key == "deny" {
				permissions[key] = []any{}
			}
			continue
		}
		entries, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("selected Claude settings permissions.%s must be a string array", key)
		}
		for _, entry := range entries {
			if _, ok := entry.(string); !ok {
				return nil, fmt.Errorf("selected Claude settings permissions.%s must contain only strings", key)
			}
		}
	}
	return document, nil
}

func (r *Runner) checkPolicyDirectory(req agent.Request, dir string) error {
	if strings.ContainsAny(dir, "*?[]{}\\():") {
		return errors.New("selected Claude settings directory contains native glob characters or parameter/delimiter syntax; move factory logs to a path without native pattern syntax")
	}
	protected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolve protected deny settings directory: %w", err)
	}
	roots := append([]string{req.WorktreePath, req.SubmissionRoot, r.paths.ReviewCacheDir(), r.goBuildCaches.Main, r.goBuildCaches.Reviewer}, r.packageCacheDirs()...)
	if gitDir := worktreeGitDir(req.WorktreePath); gitDir != "" {
		roots = append(roots, gitDir)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		rel, err := filepath.Rel(root, protected)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("selected Claude settings directory is inside agent-writable root %q; move factory logs outside writable roots", root)
		}
	}
	return nil
}

// protectSelectedSandbox makes implicit factory isolation defaults explicit
// before merging an operator sandbox. Settings that replace the enforcement
// machinery cannot be merged safely with the factory's boundary.
func protectSelectedSandbox(generated map[string]any, original any) error {
	generated["enableWeakerNestedSandbox"] = false
	if _, exists := generated["enableWeakerNetworkIsolation"]; !exists {
		generated["enableWeakerNetworkIsolation"] = false
	}
	generated["allowAppleEvents"] = false
	generated["allowNestedUserNamespaces"] = false
	filesystem, ok := generated["filesystem"].(map[string]any)
	if !ok {
		return errors.New("factory sandbox filesystem must be an object")
	}
	filesystem["disabled"] = false
	if _, exists := filesystem["allowRead"]; !exists {
		filesystem["allowRead"] = []string{}
	}
	network, ok := generated["network"].(map[string]any)
	if !ok {
		return errors.New("factory sandbox network must be an object")
	}
	network["allowMachLookup"] = []string{}
	base, ok := original.(map[string]any)
	if !ok {
		return nil // preserveSandboxLimits diagnoses a malformed original object.
	}
	for _, key := range []string{"bwrapPath", "socatPath", "ripgrep"} {
		if _, exists := base[key]; exists {
			return fmt.Errorf("selected Claude settings sandbox.%s replaces factory sandbox machinery; remove this override before using selected groups", key)
		}
	}
	if network, ok := base["network"].(map[string]any); ok {
		for _, key := range []string{"httpProxyPort", "socksProxyPort"} {
			if _, exists := network[key]; exists {
				return fmt.Errorf("selected Claude settings sandbox.network.%s bypasses native domain restrictions; remove the proxy override before using selected groups", key)
			}
		}
	}
	return nil
}

// preserveSandboxLimits keeps operator limits while adding factory limits. An
// explicit allow list can only shrink; denies can only grow. Unknown settings
// remain present for Claude to interpret.
func preserveSandboxLimits(generated map[string]any, original any) error {
	if original == nil {
		return nil
	}
	base, ok := original.(map[string]any)
	if !ok {
		return errors.New("selected Claude settings sandbox must be an object")
	}
	for key, value := range base {
		required, exists := generated[key]
		if !exists {
			generated[key] = value
			continue
		}
		if child, ok := required.(map[string]any); ok {
			if err := preserveSandboxLimits(child, value); err != nil {
				return fmt.Errorf("sandbox.%s: %w", key, err)
			}
			continue
		}
		if entries, ok := required.([]string); ok {
			originalEntries, ok := value.([]any)
			if !ok {
				return fmt.Errorf("selected Claude settings sandbox.%s must be a string array", key)
			}
			var stringsOnly []string
			for _, entry := range originalEntries {
				text, ok := entry.(string)
				if !ok {
					return fmt.Errorf("selected Claude settings sandbox.%s must contain only strings", key)
				}
				stringsOnly = append(stringsOnly, text)
			}
			switch {
			case strings.HasPrefix(key, "deny"), strings.HasPrefix(key, "denied"):
				for _, entry := range stringsOnly {
					if !slices.Contains(entries, entry) {
						entries = append(entries, entry)
					}
				}
			case strings.HasPrefix(key, "allow"), strings.HasPrefix(key, "allowed"):
				entries = slices.DeleteFunc(entries, func(entry string) bool { return !slices.Contains(stringsOnly, entry) })
			}
			generated[key] = entries
			continue
		}
		if flag, ok := required.(bool); ok {
			originalFlag, ok := value.(bool)
			if !ok {
				return fmt.Errorf("selected Claude settings sandbox.%s must be boolean", key)
			}
			if key != "enabled" && key != "failIfUnavailable" {
				generated[key] = flag && originalFlag
			}
		}
	}
	return nil
}
