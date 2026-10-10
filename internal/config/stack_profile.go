package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"
)

// StackProfilePath is the package-root profile in a profile-bearing release.
const StackProfilePath = "stack-profile.json"

// StackOverridePath is read from the admitted repository revision.
const StackOverridePath = ".litefactory/stack.overrides.json"

// StackProfileLimit bounds each raw profile and override before decoding.
const StackProfileLimit = 1 << 20

// StackGuidance names a required skill in the selected package, and its Plan ID.
type StackGuidance struct {
	ID    string `json:"id"`
	Skill string `json:"skill"`
}

// StackRequirements declare host constraints, never installation instructions.
type StackRequirements struct {
	OS          []string `json:"os"`
	Arch        []string `json:"arch"`
	Executables []string `json:"executables"`
}

// StackCommand is one named command using the existing bounded recipe model.
type StackCommand struct {
	Name string `json:"name"`
	BugCheckCommand
}

// StackEnvironment is one factory-approved non-secret environment setting.
type StackEnvironment struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// StackProfile is bounded configuration; it carries no workflow or permission authority.
type StackProfile struct {
	Version        int                `json:"version"`
	Guidance       []StackGuidance    `json:"guidance"`
	Requirements   StackRequirements  `json:"requirements"`
	Checks         []StackCommand     `json:"checks"`
	Setup          []StackCommand     `json:"setup"`
	Environment    []StackEnvironment `json:"environment"`
	OverrideFields []string           `json:"override_fields"`
	RequiredFields []string           `json:"required_fields"`
}

// StackOverride preserves omission separately from an explicitly empty array.
// Host requirements and policy cannot be overridden by a repository.
type StackOverride struct {
	Version     int                 `json:"version"`
	Guidance    *[]StackGuidance    `json:"guidance,omitempty"`
	Checks      *[]StackCommand     `json:"checks,omitempty"`
	Setup       *[]StackCommand     `json:"setup,omitempty"`
	Environment *[]StackEnvironment `json:"environment,omitempty"`
}

// ResolvedStackProfile retains exact inputs and a canonical effective digest.
// Capture owners supply and retain the release identity and repository revision.
type ResolvedStackProfile struct {
	BaseBytes       []byte       `json:"base_bytes"`
	BaseDigest      string       `json:"base_digest"`
	OverrideBytes   []byte       `json:"override_bytes,omitempty"`
	OverrideDigest  string       `json:"override_digest,omitempty"`
	Effective       StackProfile `json:"effective"`
	EffectiveDigest string       `json:"effective_digest"`
}

// DecodeStackProfile strictly decodes and validates a version 1 profile.
func DecodeStackProfile(raw []byte) (StackProfile, error) {
	var profile StackProfile
	if err := decodeStackJSON(raw, &profile); err != nil {
		return StackProfile{}, err
	}
	if err := profile.Validate(); err != nil {
		return StackProfile{}, err
	}
	return profile, nil
}

// DecodeStackOverride strictly decodes a version 1 partial override.
// Team permission and required fields are checked during resolution.
func DecodeStackOverride(raw []byte) (StackOverride, error) {
	var override StackOverride
	if err := decodeStackJSON(raw, &override); err != nil {
		return StackOverride{}, err
	}
	if override.Version != 1 {
		return StackOverride{}, errors.New("stack.version: unsupported schema version")
	}
	return override, nil
}

// ResolveStackProfile applies allowed replacements to freshly decoded inputs.
// Only a nil override means absent; an empty file is invalid.
func ResolveStackProfile(base, override []byte) (ResolvedStackProfile, error) {
	profile, err := DecodeStackProfile(base)
	if err != nil {
		return ResolvedStackProfile{}, err
	}
	if override != nil {
		partial, err := DecodeStackOverride(override)
		if err != nil {
			return ResolvedStackProfile{}, err
		}
		fields := []struct {
			name    string
			present bool
			empty   bool
		}{
			{"guidance", partial.Guidance != nil, partial.Guidance != nil && len(*partial.Guidance) == 0},
			{"checks", partial.Checks != nil, partial.Checks != nil && len(*partial.Checks) == 0},
			{"setup", partial.Setup != nil, partial.Setup != nil && len(*partial.Setup) == 0},
			{"environment", partial.Environment != nil, partial.Environment != nil && len(*partial.Environment) == 0},
		}
		for _, field := range fields {
			if !field.present {
				continue
			}
			if !stackOverrideField(field.name) || !slices.Contains(profile.OverrideFields, field.name) {
				return ResolvedStackProfile{}, fmt.Errorf("stack.%s: repository override forbidden", field.name)
			}
			if field.empty && (field.name == "guidance" || slices.Contains(profile.RequiredFields, field.name)) {
				return ResolvedStackProfile{}, fmt.Errorf("stack.%s: required setting cannot be cleared", field.name)
			}
		}
		if partial.Guidance != nil {
			profile.Guidance = *partial.Guidance
		}
		if partial.Checks != nil {
			profile.Checks = *partial.Checks
		}
		if partial.Setup != nil {
			profile.Setup = *partial.Setup
		}
		if partial.Environment != nil {
			profile.Environment = *partial.Environment
		}
		if err := profile.Validate(); err != nil {
			return ResolvedStackProfile{}, err
		}
	}
	// Normalize optional empty lists so equivalent settings have equal digests.
	profile = normalizeStackProfile(profile)
	effective, err := json.Marshal(profile)
	if err != nil {
		return ResolvedStackProfile{}, errors.New("stack: effective settings cannot be encoded")
	}
	resolved := ResolvedStackProfile{BaseBytes: bytes.Clone(base), BaseDigest: BugRecipeDigest(base), Effective: profile, EffectiveDigest: BugRecipeDigest(effective)}
	if override != nil {
		resolved.OverrideBytes = bytes.Clone(override)
		resolved.OverrideDigest = BugRecipeDigest(override)
	}
	return resolved, nil
}

// ValidateRetained recomputes all digests and settings from the retained bytes.
func (r ResolvedStackProfile) ValidateRetained() error {
	fresh, err := ResolveStackProfile(r.BaseBytes, r.OverrideBytes)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(normalizeStackProfile(r.Effective))
	if err != nil {
		return errors.New("stack.capture: invalid effective settings")
	}
	if r.BaseDigest != fresh.BaseDigest || r.OverrideDigest != fresh.OverrideDigest || r.EffectiveDigest != fresh.EffectiveDigest || BugRecipeDigest(encoded) != fresh.EffectiveDigest {
		return errors.New("stack.capture: bytes, settings or digest changed")
	}
	return nil
}

func normalizeStackProfile(p StackProfile) StackProfile {
	p.Guidance = slices.Clone(p.Guidance)
	p.Checks = normalizeStackCommands(p.Checks)
	p.Setup = normalizeStackCommands(p.Setup)
	p.Environment = slices.Clone(p.Environment)
	p.OverrideFields = slices.Clone(p.OverrideFields)
	p.RequiredFields = slices.Clone(p.RequiredFields)
	p.Requirements.OS = slices.Clone(p.Requirements.OS)
	p.Requirements.Arch = slices.Clone(p.Requirements.Arch)
	p.Requirements.Executables = slices.Clone(p.Requirements.Executables)
	if p.Checks == nil {
		p.Checks = []StackCommand{}
	}
	if p.Setup == nil {
		p.Setup = []StackCommand{}
	}
	if p.Environment == nil {
		p.Environment = []StackEnvironment{}
	}
	if p.OverrideFields == nil {
		p.OverrideFields = []string{}
	}
	if p.RequiredFields == nil {
		p.RequiredFields = []string{}
	}
	if p.Requirements.OS == nil {
		p.Requirements.OS = []string{}
	}
	if p.Requirements.Arch == nil {
		p.Requirements.Arch = []string{}
	}
	if p.Requirements.Executables == nil {
		p.Requirements.Executables = []string{}
	}
	return p
}

func normalizeStackCommands(commands []StackCommand) []StackCommand {
	commands = slices.Clone(commands)
	for i := range commands {
		commands[i].Argv = slices.Clone(commands[i].Argv)
		commands[i].Env = slices.Clone(commands[i].Env)
		if commands[i].Env == nil {
			commands[i].Env = []string{}
		}
		if commands[i].Rerun != nil {
			rerun := *commands[i].Rerun
			rerun.Argv = slices.Clone(rerun.Argv)
			commands[i].Rerun = &rerun
		}
	}
	return commands
}

func stackOverrideField(name string) bool {
	switch name {
	case "guidance", "checks", "setup", "environment":
		return true
	}
	return false
}

func stackIdentifier(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for i, c := range name {
		if (c < 'a' || c > 'z') && (i == 0 || (c < '0' || c > '9') && c != '-') {
			return false
		}
	}
	return true
}

func stackPackagePath(name string) bool {
	return len(name) <= 1024 && name != "." && fs.ValidPath(name) && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00\r\n%?#")
}

// Validate checks independent faults in stable field order without echoing values.
func (p StackProfile) Validate() error {
	var faults []error
	if p.Version != 1 {
		faults = append(faults, errors.New("stack.version: unsupported schema version"))
	}
	if len(p.Guidance) < 1 || len(p.Guidance) > 64 {
		faults = append(faults, errors.New("stack.guidance: requires 1..64 skills"))
	}
	seen := map[string]bool{}
	for i, guidance := range p.Guidance {
		if !stackIdentifier(guidance.ID) || !stackPackagePath(guidance.Skill) || !strings.HasSuffix(guidance.Skill, "/SKILL.md") || seen[guidance.ID] {
			faults = append(faults, fmt.Errorf("stack.guidance[%d]: invalid or duplicate ID or unsafe skill path", i))
		}
		seen[guidance.ID] = true
	}
	faults = append(faults, stackRequirementsFaults(p.Requirements)...)
	faults = append(faults, stackCommandFaults("checks", p.Checks, p.Environment)...)
	faults = append(faults, stackCommandFaults("setup", p.Setup, p.Environment)...)
	if len(p.Environment) > 64 {
		faults = append(faults, errors.New("stack.environment: exceeds 64 entries"))
	}
	seen = map[string]bool{}
	for i, entry := range p.Environment {
		if err := ValidateStackEnvironment(entry); err != nil {
			faults = append(faults, fmt.Errorf("stack.environment[%d]: %w", i, err))
		}
		if seen[entry.Name] {
			faults = append(faults, fmt.Errorf("stack.environment[%d]: duplicate name", i))
		}
		seen[entry.Name] = true
	}
	for _, fields := range []struct {
		name   string
		values []string
	}{{"override_fields", p.OverrideFields}, {"required_fields", p.RequiredFields}} {
		if len(fields.values) > 4 {
			faults = append(faults, fmt.Errorf("stack.%s: exceeds field limit", fields.name))
		}
		seen = map[string]bool{}
		for i, field := range fields.values {
			if !stackOverrideField(field) || seen[field] {
				faults = append(faults, fmt.Errorf("stack.%s[%d]: forbidden or duplicate field", fields.name, i))
			}
			seen[field] = true
		}
	}
	for _, field := range p.RequiredFields {
		empty := field == "checks" && len(p.Checks) == 0 || field == "setup" && len(p.Setup) == 0 || field == "environment" && len(p.Environment) == 0
		if empty {
			faults = append(faults, fmt.Errorf("stack.%s: required setting cannot be empty", field))
		}
	}
	return errors.Join(faults...)
}

func stackRequirementsFaults(r StackRequirements) []error {
	var faults []error
	for _, list := range []struct {
		name   string
		values []string
	}{{"os", r.OS}, {"arch", r.Arch}, {"executables", r.Executables}} {
		if len(list.values) > 64 {
			faults = append(faults, fmt.Errorf("stack.requirements.%s: exceeds 64 entries", list.name))
		}
		seen := map[string]bool{}
		for i, value := range list.values {
			if !stackIdentifier(value) || seen[value] {
				faults = append(faults, fmt.Errorf("stack.requirements.%s[%d]: invalid or duplicate name", list.name, i))
			}
			seen[value] = true
		}
	}
	return faults
}

func stackCommandFaults(field string, commands []StackCommand, environment []StackEnvironment) []error {
	var faults []error
	if len(commands) > 64 {
		faults = append(faults, fmt.Errorf("stack.%s: exceeds 64 commands", field))
	}
	seen := map[string]bool{}
	for i, command := range commands {
		what := fmt.Sprintf("stack.%s[%d]", field, i)
		if !stackIdentifier(command.Name) || seen[command.Name] {
			faults = append(faults, fmt.Errorf("%s.name: invalid or duplicate name", what))
		}
		seen[command.Name] = true
		faults = append(faults, bugArgvFaults(what, command.Argv)...)
		faults = append(faults, bugRerunFaults(what, command.Rerun)...)
		if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 86400 {
			faults = append(faults, fmt.Errorf("%s.timeout_seconds: outside 1..86400", what))
		}
		if command.CWD != "." && !stackPackagePath(command.CWD) {
			faults = append(faults, fmt.Errorf("%s.cwd: must stay inside worktree", what))
		}
		if len(command.Env) > 64 {
			faults = append(faults, fmt.Errorf("%s.env: exceeds 64 entries", what))
		}
		envSeen := map[string]bool{}
		for j, name := range command.Env {
			found := slices.ContainsFunc(environment, func(e StackEnvironment) bool { return e.Name == name })
			if !found || envSeen[name] {
				faults = append(faults, fmt.Errorf("%s.env[%d]: undeclared or duplicate environment reference", what, j))
			}
			envSeen[name] = true
		}
	}
	return faults
}

// ValidateStackEnvironment enforces the factory's fixed safe variable policy.
// Credentials, provider configuration, PATH, HOME and cache/root paths are absent.
func ValidateStackEnvironment(entry StackEnvironment) error {
	if len(entry.Value) > 4096 || strings.ContainsAny(entry.Value, "\x00\r\n") {
		return errors.New("value exceeds bounds or contains control characters")
	}
	switch entry.Name {
	case "LANG", "LC_ALL", "TZ":
		if len(entry.Value) == 0 {
			return errors.New("locale setting cannot be empty")
		}
	case "CI", "NO_COLOR", "DOTNET_NOLOGO", "DOTNET_CLI_TELEMETRY_OPTOUT", "PYTHONDONTWRITEBYTECODE", "PYTHONUNBUFFERED", "CGO_ENABLED":
		if entry.Value != "0" && entry.Value != "1" && entry.Value != "true" && entry.Value != "false" {
			return errors.New("requires a boolean value")
		}
	case "GOTOOLCHAIN":
		if entry.Value != "local" {
			return errors.New("automatic host installation is forbidden")
		}
	case "GOMAXPROCS":
		if len(entry.Value) == 0 || len(entry.Value) > 5 || strings.Trim(entry.Value, "0123456789") != "" || strings.Trim(entry.Value, "0") == "" {
			return errors.New("requires a positive bounded integer")
		}
	default:
		return errors.New("reserved or unapproved environment name")
	}
	return nil
}

// decodeStackJSON preflights duplicate keys, nulls and nesting before typed decoding.
// Decoder errors are deliberately not echoed: they may contain sensitive values.
func decodeStackJSON(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > StackProfileLimit {
		return errors.New("stack: input size outside 1..1048576 bytes")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := stackJSONValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("stack: trailing or malformed JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := stackJSONFields(raw, reflect.TypeOf(target).Elem(), "stack"); err != nil {
		return err
	}
	if err := d.Decode(target); err != nil {
		return errors.New("stack: unknown field or invalid JSON field type")
	}
	return nil
}

// encoding/json accepts case-insensitive struct fields. Check exact schema names
// first so alternate spellings cannot bypass duplicate-key or closed-field rules.
func stackJSONFields(raw []byte, typ reflect.Type, field string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return fmt.Errorf("%s: requires an object", field)
		}
		known := stackJSONStructFields(typ)
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			nested, ok := known[key]
			if !ok {
				return fmt.Errorf("%s: unknown schema field", field)
			}
			if err := stackJSONFields(object[key], nested, field+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			return fmt.Errorf("%s: requires an array", field)
		}
		for i, element := range array {
			if err := stackJSONFields(element, typ.Elem(), fmt.Sprintf("%s[%d]", field, i)); err != nil {
				return err
			}
		}
	default:
		if err := json.Unmarshal(raw, reflect.New(typ).Interface()); err != nil {
			return fmt.Errorf("%s: invalid field type", field)
		}
	}
	return nil
}

func stackJSONStructFields(typ reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			for name, nested := range stackJSONStructFields(field.Type) {
				fields[name] = nested
			}
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields[name] = field.Type
		}
	}
	return fields
}

func stackJSONValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("stack: JSON nesting exceeds limit")
	}
	token, err := d.Token()
	if err != nil {
		return errors.New("stack: malformed JSON")
	}
	if token == nil {
		return errors.New("stack: null settings are forbidden; omit or use an empty list")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return errors.New("stack: malformed JSON object")
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return errors.New("stack: duplicate or invalid JSON field")
			}
			seen[key] = true
			if err := stackJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		count := 0
		for d.More() {
			count++
			if count > 128 {
				return errors.New("stack: JSON collection exceeds 128 entries")
			}
			if err := stackJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("stack: malformed JSON delimiter")
	}
	if _, err := d.Token(); err != nil {
		return errors.New("stack: malformed JSON closure")
	}
	return nil
}
