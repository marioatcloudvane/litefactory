package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/codex"
)

// selectedLaunch owns an immutable configuration generation for one leased attempt.
// The runtime may update its canonical authentication/state, but agent tools may
// not write either the policy or its backing state.
type selectedLaunch struct {
	home, canonical string
	artifacts       []denyArtifact
	links           map[string]string
	roots           []string
	protected       []string
	resolvedPaths   map[string]string
}

type denyArtifact struct {
	path      string
	info      os.FileInfo
	digest    [32]byte
	directory bool
}

func (r *Runner) prepareSelectedLaunch(ctx context.Context, req agent.Request, policy *compiledDenyPolicy, sandbox string, args []string, env []string) (*selectedLaunch, []string, []string, error) {
	fail := func(err error) (*selectedLaunch, []string, []string, error) {
		return nil, nil, nil, denyPolicyError(req.DenyPolicy, err.Error())
	}
	canonical, err := r.denyCanonicalHome()
	if err != nil {
		return fail(err)
	}
	root, err := filepath.EvalSymlinks(r.paths.CodexPolicyDir())
	if err != nil {
		return fail(err)
	}
	identity := sha256.Sum256([]byte(req.Ref.String() + "\x00" + req.Session.Ref))
	base := filepath.Join(root, hex.EncodeToString(identity[:]))
	home := filepath.Join(base, "home")
	if err := denyMkdir(home); err != nil {
		return fail(err)
	}
	launch := &selectedLaunch{home: home, canonical: canonical, links: map[string]string{}, resolvedPaths: map[string]string{}, protected: []string{root, canonical}}
	launch.roots = append([]string{req.WorktreePath}, r.writable...)
	launch.roots = append(launch.roots, r.goBuildCaches.Main, r.goBuildCaches.Reviewer, req.SubmissionRoot)
	if sandbox == "workspace-write" {
		for _, temp := range []string{"/tmp", envValue(env, "TMPDIR")} {
			if temp != "" {
				if !filepath.IsAbs(temp) {
					return fail(errors.New("selected groups require an absolute temporary directory"))
				}
				if info, err := os.Stat(temp); err != nil || !info.IsDir() {
					return fail(errors.New("selected groups require accessible existing temporary directories"))
				}
				launch.roots = append(launch.roots, temp)
			}
		}
	}
	if err := launch.containment(); err != nil {
		return fail(err)
	}
	if err := launch.bridge(); err != nil {
		return fail(err)
	}
	configText, preparedArgs, err := selectedConfiguration(req, policy, sandbox, args, launch)
	if err != nil {
		return fail(err)
	}
	generation, err := os.MkdirTemp(base, "generation-")
	if err != nil {
		return fail(err)
	}
	activated := false
	defer func() {
		if !activated {
			_ = os.RemoveAll(generation)
		}
	}()
	if err := os.Mkdir(filepath.Join(generation, "rules"), dirMode); err != nil {
		return fail(err)
	}
	for name, content := range map[string]string{"config.toml": configText, "rules/selected.rules": policy.Rules} {
		if err := os.WriteFile(filepath.Join(generation, name), []byte(content), logMode); err != nil {
			return fail(err)
		}
	}
	binary, err := filepath.EvalSymlinks(r.binary.Path)
	if err != nil {
		return fail(errors.New("cannot resolve selected runtime binary"))
	}
	launch.resolvedPaths[r.binary.Path] = binary
	if err := launch.capture(binary); err != nil {
		return fail(err)
	}
	policyBytes, err := json.Marshal(req.DenyPolicy)
	if err != nil {
		return fail(err)
	}
	policyDigest := sha256.Sum256(policyBytes)
	manifest, err := json.Marshal(struct {
		PolicyDigest                                             string
		Binary, BinaryDigest, Version, Canonical, FactorySession string
		RunOrdinal                                               int
	}{hex.EncodeToString(policyDigest[:]), r.binary.Path, hex.EncodeToString(launch.artifacts[len(launch.artifacts)-1].digest[:]), denySupportedVersion, canonical, req.Session.Ref, req.RunOrdinal})
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(generation, "manifest.json"), manifest, logMode); err != nil {
		return fail(err)
	}
	for _, path := range []string{root, base, home, generation, filepath.Join(generation, "rules"), filepath.Join(generation, "config.toml"), filepath.Join(generation, "rules", "selected.rules"), filepath.Join(generation, "manifest.json"), canonical} {
		if err := launch.capture(path); err != nil {
			return fail(err)
		}
	}
	// Validate a complete, unpublished generation before atomically activating it.
	for _, name := range []string{"auth.json", "sessions", "archived_sessions", "thread-writer-locks"} {
		if err := launch.link(filepath.Join(generation, name), filepath.Join(canonical, name)); err != nil {
			return fail(err)
		}
	}
	stageEnv := append(slices.Clone(env), "CODEX_HOME="+generation)
	if err := r.checkSelectedCapabilities(ctx, req, stageEnv, preparedArgs, generation); err != nil {
		return fail(err)
	}
	if err := launch.verifyArtifacts(); err != nil {
		return fail(err)
	}
	// Stable links keep native resume and child configuration folder identity fixed.
	for name, target := range map[string]string{"config.toml": filepath.Join(base, "active", "config.toml"), "rules": filepath.Join(base, "active", "rules")} {
		if err := launch.link(filepath.Join(home, name), target); err != nil {
			return fail(err)
		}
	}
	active := filepath.Join(base, "active")
	if st, err := os.Lstat(active); err == nil && st.Mode()&os.ModeSymlink == 0 {
		return fail(errors.New("policy activation is not a host-owned symlink"))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	pending := filepath.Join(generation, "activate")
	if err := os.Symlink(generation, pending); err != nil {
		return fail(err)
	}
	if err := os.Rename(pending, active); err != nil {
		return fail(err)
	}
	activated = true
	launch.links[active] = generation
	env = append(slices.Clone(env), "CODEX_HOME="+home)
	if err := launch.verify(); err != nil {
		return fail(err)
	}
	return launch, preparedArgs, env, nil
}

func denyMkdir(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("policy directory must be absolute")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, dirMode); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			st, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe policy directory %s", current)
		}
	}
	return nil
}

func (r *Runner) denyCanonicalHome() (string, error) {
	home := envValue(r.environ, "HOME")
	if !filepath.IsAbs(home) {
		return "", errors.New("selected groups require an absolute HOME and existing file login")
	}
	userHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", errors.New("cannot resolve canonical HOME")
	}
	source := filepath.Join(userHome, ".codex")
	sourceInfo, err := os.Lstat(source)
	if err != nil || !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("canonical Codex home must be a real directory to preserve historical usage")
	}
	canonical, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", errors.New("selected groups require an existing canonical Codex home and file login")
	}
	if custom := envValue(r.environ, "CODEX_HOME"); custom != "" {
		resolved, err := filepath.EvalSymlinks(custom)
		if err != nil || resolved != canonical {
			return "", errors.New("custom CODEX_HOME cannot preserve the ordinary session store; use the canonical HOME/.codex login")
		}
	}
	auth := filepath.Join(canonical, "auth.json")
	st, err := os.Lstat(auth)
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("selected groups require a regular canonical auth.json file; keyring-only login is unsupported")
	}
	// User configuration is not imported. Inspect credential-mode declarations so
	// a stale file cannot masquerade as an active keyring login.
	for _, path := range []string{filepath.Join(canonical, "config.toml"), "/etc/codex/config.toml", "/etc/codex/managed_config.toml"} {
		//nolint:gosec // Only fixed host credential configuration or admitted policy artifacts are read.
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", errors.New("cannot inspect credential storage configuration")
		}
		if err := denyCredentialMode(data); err != nil {
			return "", err
		}
	}
	return canonical, nil
}

func denyCredentialMode(data []byte) error {
	if len(data) > 1<<20 {
		return errors.New("credential configuration exceeds inspection limit")
	}
	p := mcpConfigScanner{source: string(data)}
	for {
		token, err := p.next()
		if err != nil {
			return errors.New("credential configuration cannot be inspected safely")
		}
		if token.kind == 0 {
			return nil
		}
		if (token.kind == 'k' || token.kind == 'q') && token.text == "cli_auth_credentials_store" {
			if err := p.expect('='); err != nil {
				return errors.New("credential storage declaration cannot be inspected safely")
			}
			value, err := p.next()
			if err != nil || value.kind != 'q' || value.text != "file" {
				return errors.New("selected groups support only file credential storage; keyring and auto modes are unsupported")
			}
		}
	}
}

func (s *selectedLaunch) bridge() error {
	for _, name := range []string{"sessions", "archived_sessions", "thread-writer-locks"} {
		target := filepath.Join(s.canonical, name)
		if err := denyMkdir(target); err != nil {
			return err
		}
		if err := s.capture(target); err != nil {
			return err
		}
		if err := s.link(filepath.Join(s.home, name), target); err != nil {
			return err
		}
	}
	return s.link(filepath.Join(s.home, "auth.json"), filepath.Join(s.canonical, "auth.json"))
}
func (s *selectedLaunch) link(path, target string) error {
	actual, err := os.Readlink(path)
	if errors.Is(err, os.ErrNotExist) {
		err = os.Symlink(target, path)
	} else if err == nil && actual != target {
		err = errors.New("configuration bridge target changed")
	}
	if err != nil {
		return err
	}
	s.links[path] = target
	return nil
}
func (s *selectedLaunch) containment() error {
	for _, root := range s.roots {
		if root == "" {
			continue
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			return fmt.Errorf("resolve writable root: %w", err)
		}
		for _, protected := range s.protected {
			if pathWithin(canonical, protected) || pathWithin(protected, canonical) {
				return fmt.Errorf("protected policy or canonical state %q overlaps writable root %q; use homes outside agent-writable roots", protected, canonical)
			}
		}
	}
	return nil
}
func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func (s *selectedLaunch) capture(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	artifact := denyArtifact{path: path, info: st, directory: st.IsDir()}
	if !st.IsDir() {
		if !st.Mode().IsRegular() {
			return errors.New("policy artifact is not regular")
		}
		info, digest, err := denyFileFingerprint(path)
		if err != nil {
			return err
		}
		if !os.SameFile(st, info) {
			return errors.New("policy artifact changed during capture")
		}
		artifact.digest = digest
	}
	s.artifacts = append(s.artifacts, artifact)
	return nil
}
func (s *selectedLaunch) verify() error {
	if err := s.containment(); err != nil {
		return err
	}
	for path, target := range s.resolvedPaths {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != target {
			return errors.New("runtime path target changed")
		}
	}
	for path, target := range s.links {
		actual, err := os.Readlink(path)
		if err != nil || actual != target {
			return errors.New("protected configuration link changed")
		}
	}
	if err := s.verifyArtifacts(); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(s.home, "rules"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "selected.rules" {
		return errors.New("protected rule inventory changed")
	}
	auth, err := os.Lstat(filepath.Join(s.canonical, "auth.json"))
	if err != nil || !auth.Mode().IsRegular() {
		return errors.New("canonical credential target changed")
	}
	return nil
}

func (s *selectedLaunch) verifyArtifacts() error {
	for _, a := range s.artifacts {
		resolved, err := filepath.EvalSymlinks(a.path)
		if err != nil || resolved != a.path {
			return errors.New("protected artifact ancestor changed")
		}
		st, err := os.Lstat(a.path)
		if err != nil || !os.SameFile(a.info, st) || st.Mode() != a.info.Mode() {
			return errors.New("protected configuration artifact replaced")
		}
		if !a.directory {
			info, digest, err := denyFileFingerprint(a.path)
			if err != nil || !os.SameFile(a.info, info) || digest != a.digest {
				return errors.New("protected configuration artifact changed")
			}
		}
	}
	return nil
}

func denyFileFingerprint(path string) (os.FileInfo, [32]byte, error) {
	var digest [32]byte
	//nolint:gosec // Admitted host artifact; O_NOFOLLOW rejects a replaced leaf link.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, digest, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, digest, err
	}
	if !info.Mode().IsRegular() {
		return nil, digest, errors.New("policy artifact is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, digest, err
	}
	copy(digest[:], hash.Sum(nil))
	return info, digest, nil
}

func selectedConfiguration(req agent.Request, policy *compiledDenyPolicy, sandbox string, args []string, launch *selectedLaunch) (string, []string, error) {
	var config strings.Builder
	config.WriteString("approval_policy = \"never\"\ncli_auth_credentials_store = \"file\"\nsqlite_home = " + mcpQuote(launch.canonical) + "\n")
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "-c" && strings.HasPrefix(args[i+1], "permissions.factory_reviewer.") {
			config.WriteString(args[i+1] + "\n")
			i++
		}
	}
	for dir := req.WorktreePath; ; dir = filepath.Dir(dir) {
		fmt.Fprintf(&config, "[projects.%s]\ntrust_level = \"untrusted\"\n", mcpQuote(dir))
		if filepath.Dir(dir) == dir {
			break
		}
	}
	parent := ":workspace"
	if sandbox == "read-only" {
		parent = ":read-only"
	}
	out := []string{args[0], "--strict-config"}
	var extraRoots []string
	network := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--ignore-rules", "--ignore-user-config", "--strict-config":
			continue
		case "--sandbox":
			if i+1 >= len(args) {
				return "", nil, errors.New("invalid sandbox argument")
			}
			i++
			continue
		case "--add-dir":
			if i+1 >= len(args) {
				return "", nil, errors.New("invalid writable-root argument")
			}
			i++
			//nolint:gosec // The preceding bounds check reserves this consumed argument.
			extraRoots = append(extraRoots, args[i])
			continue
		case "-c":
			if i+1 >= len(args) {
				return "", nil, errors.New("invalid runtime override")
			}
			//nolint:gosec // The preceding bounds check reserves the override value.
			value := args[i+1]
			if strings.HasPrefix(value, "permissions.factory_reviewer.") {
				i++
				continue
			}
			if strings.HasPrefix(value, "sandbox_workspace_write.network_access=") {
				network = strings.HasSuffix(value, "=true")
				i++
				continue
			}
			if strings.HasPrefix(value, "default_permissions=") {
				if value == `default_permissions="factory_reviewer"` {
					parent = "factory_reviewer"
				}
				i++
				continue
			}
		}
		out = append(out, args[i])
	}
	fmt.Fprintf(&config, "[permissions.factory_selected]\nextends = %s\n", mcpQuote(parent))
	fmt.Fprintf(&config, "[permissions.factory_selected.filesystem]\n%s = \"read\"\n%s = \"read\"\n", mcpQuote(launch.protected[0]), mcpQuote(launch.canonical))
	if len(extraRoots) > 0 {
		config.WriteString("[permissions.factory_selected.workspace_roots]\n")
		for _, root := range extraRoots {
			fmt.Fprintf(&config, "%s = true\n", mcpQuote(root))
		}
	}
	fmt.Fprintf(&config, "[permissions.factory_selected.network]\nenabled = %t\n", network)
	overrides := []string{"-c", `default_permissions="factory_selected"`, "-c", `approval_policy="never"`}
	if policy.DisableDelegation {
		overrides = append(overrides, "-c", "features.multi_agent=false")
	}
	out = append(append([]string{out[0]}, overrides...), out[1:]...)
	return config.String(), out, nil
}

func (r *Runner) checkSelectedCapabilities(ctx context.Context, req agent.Request, env, args []string, generation string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := func(argv []string) ([]byte, error) {
		cmd := native.Command(ctx, r.binary.Path, argv...)
		cmd.Env = env
		cmd.Dir = req.WorktreePath
		cmd.Stderr = io.Discard
		output, err := cmd.Output()
		if err != nil {
			return nil, errors.New("native capability/configuration check failed")
		}
		return output, nil
	}
	version, err := run([]string{"--version"})
	if err != nil {
		return err
	}
	if err = validateDenyVersion(string(version)); err != nil {
		return err
	}
	help, err := run([]string{"exec", "--help"})
	if err != nil {
		return err
	}
	for _, flag := range []string{"--strict-config", "--ignore-user-config", "--ignore-rules"} {
		if !strings.Contains(string(help), flag) {
			return fmt.Errorf("native capability missing: %s", flag)
		}
	}
	for _, rule := range req.DenyPolicy.Rules {
		if rule.Tool != "Bash" {
			continue
		}
		tokens, err := literalDenyPrefix(rule.Pattern)
		if err != nil {
			return err
		}
		argv := append([]string{"execpolicy", "check", "--rules", filepath.Join(generation, "rules", "selected.rules"), "--"}, tokens...)
		data, err := run(argv)
		if err != nil {
			return err
		}
		var result struct {
			Decision string `json:"decision"`
		}
		if json.Unmarshal(data, &result) != nil || result.Decision != "forbidden" {
			return errors.New("native checker did not retain the forbidden prefix")
		}
	}
	return r.checkSelectedConfiguration(ctx, req, env, args)
}
