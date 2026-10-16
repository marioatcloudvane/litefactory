package presentation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Generation bounds keep model output small before decoding or checking.
const (
	MaxCandidateBytes = 8 << 10
	MaxClaims         = 6
	MaxClaimBytes     = 512
	MaxReferences     = 16
)

// Bare domains and alphabetic revision codes also belong in factory slots.
var protectedProse = regexp.MustCompile(`(?i)(?:[\pL\pN-]+\.)+[\pL]{2,}|\b[a-f0-9]{8,}\b`)

// thirdPerson matches English narration that reports about the answer
// instead of speaking to the owner directly. Other languages rely on the
// generation and checking prompts.
var thirdPerson = regexp.MustCompile(`(?i)\bthe\s+(?:system|answer|result|reply|response|bot|factory)\s+(?:found|says|asks|reports|wants|was|could|did|has|is)\b|\bit\s+(?:asks\s+you|wants\s+to\s+know)\b`)

// numberToken is a maximal digit run including '.' or ',' between digits.
var numberToken = regexp.MustCompile(`\pN+(?:[.,]\pN+)*`)

// DecodeCandidate rejects duplicate, unknown and trailing fields. Source
// validation remains necessary even for an otherwise well-formed candidate.
func DecodeCandidate(raw []byte, outcome Outcome) (Candidate, error) {
	var candidate Candidate
	if err := strictJSON(raw, &candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, candidate.Validate(outcome)
}

// Validate enforces claim references and mandatory caveat coverage. It does
// not establish semantic support; the independent checker must do that next.
// A number in prose must be one the source prints with the same characters;
// rounded, reformatted or invented numbers are rejected.
func (c Candidate) Validate(o Outcome) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if len(c.Claims) == 0 || len(c.Claims) > MaxClaims {
		return errors.New("presentation: invalid claim count")
	}
	facts := make(map[string]bool, len(o.Facts))
	protected := make(map[string]bool, len(o.Protected))
	for _, fact := range o.Facts {
		facts[fact.Handle] = true
	}
	for _, field := range o.Protected {
		protected[field.Handle] = true
	}
	sourceNumbers := make(map[string]bool)
	for _, token := range numberToken.FindAllString(o.RawText, -1) {
		sourceNumbers[token] = true
	}
	cited := make(map[string]bool)
	var failures []error
	for _, claim := range c.Claims {
		if strings.TrimSpace(claim.Text) == "" || len(claim.Text) > MaxClaimBytes || !utf8.ValidString(claim.Text) || strings.ContainsAny(claim.Text, "\r\n/\\`<>[]@\x00") || strings.Contains(strings.ToLower(claim.Text), "www.") || strings.Contains(claim.Text, "://") || protectedClaimProse(claim.Text) {
			failures = append(failures, errors.New("presentation: invalid claim text"))
		}
		if thirdPerson.MatchString(claim.Text) {
			failures = append(failures, errors.New("presentation: third-person self-description"))
		}
		for _, r := range claim.Text {
			if unicode.IsControl(r) {
				failures = append(failures, errors.New("presentation: control character in claim text"))
				break
			}
		}
		for _, token := range numberToken.FindAllString(claim.Text, -1) {
			if !sourceNumbers[token] {
				failures = append(failures, errors.New("presentation: number not printed in source"))
				break
			}
		}
		if len(claim.Facts) == 0 || len(claim.Facts) > MaxReferences || len(claim.Protected) > MaxReferences {
			failures = append(failures, errors.New("presentation: invalid claim references"))
		}
		seen := make(map[string]bool)
		for _, handle := range claim.Facts {
			if !facts[handle] || seen[handle] {
				failures = append(failures, errors.New("presentation: unknown or duplicate fact reference"))
			}
			seen[handle], cited[handle] = true, true
		}
		for _, handle := range claim.Protected {
			if !protected[handle] || seen[handle] {
				failures = append(failures, errors.New("presentation: unknown or duplicate protected reference"))
			}
			seen[handle] = true
		}
	}
	for _, fact := range o.Facts {
		if fact.Mandatory && !cited[fact.Handle] {
			failures = append(failures, errors.New("presentation: missing mandatory caveat reference"))
		}
	}
	return errors.Join(failures...)
}

// protectedClaimProse exempts hash-like digit runs only within decimal tokens.
// Source span selection still uses protectedProse without this exemption.
func protectedClaimProse(text string) bool {
	matches := protectedProse.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return false
	}
	numbers := numberToken.FindAllStringIndex(text, -1)
	for _, match := range matches {
		decimal := false
		for _, number := range numbers {
			if number[0] <= match[0] && match[1] <= number[1] && strings.Contains(text[number[0]:number[1]], ".") {
				decimal = true
				break
			}
		}
		if !decimal {
			return true
		}
	}
	return false
}

// DecodeCheck requires exactly one supported finding for each claim. A partial,
// uncertain or unsupported verdict cannot be rendered as checked narration.
func DecodeCheck(raw []byte, candidate Candidate) (Check, error) {
	var wire struct {
		Findings []struct {
			Claim   *int    `json:"claim"`
			Support Support `json:"support"`
		} `json:"findings"`
	}
	if err := strictJSON(raw, &wire); err != nil {
		return Check{}, err
	}
	check := Check{Findings: make([]Finding, 0, len(wire.Findings))}
	for _, finding := range wire.Findings {
		if finding.Claim == nil {
			return Check{}, errors.New("presentation: missing claim index")
		}
		check.Findings = append(check.Findings, Finding{Claim: *finding.Claim, Support: finding.Support})
	}
	return check, check.Validate(candidate)
}

// Validate checks complete, unique support coverage. It trusts neither missing
// findings nor an overall approval that omits individual claims.
func (c Check) Validate(candidate Candidate) error {
	if len(candidate.Claims) == 0 || len(candidate.Claims) > MaxClaims || len(c.Findings) != len(candidate.Claims) {
		return errors.New("presentation: incomplete semantic check")
	}
	seen := make(map[int]bool, len(c.Findings))
	var failures []error
	for _, finding := range c.Findings {
		if finding.Claim < 0 || finding.Claim >= len(candidate.Claims) || seen[finding.Claim] || finding.Support != Supported {
			failures = append(failures, errors.New("presentation: claim not independently supported"))
		}
		seen[finding.Claim] = true
	}
	return errors.Join(failures...)
}

func strictJSON(raw []byte, dst any) error {
	if len(raw) == 0 || len(raw) > MaxCandidateBytes || !utf8.Valid(raw) {
		return errors.New("presentation: invalid output size or encoding")
	}
	raw = unfence(bytes.TrimSpace(raw))
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("presentation: output must be an object")
	}
	// The typed decoder rejects unknown fields but accepts duplicates. Inspect
	// tokens first with a bounded depth before decoding the whole object.
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if err := jsonValue(tokens, 0); err != nil {
		return err
	}
	if _, err := tokens.Token(); !errors.Is(err, io.EOF) {
		return errors.New("presentation: trailing output")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

// unfence strips one enclosing markdown code fence (``` or ```json) that
// providers commonly wrap around JSON. Anything else is returned unchanged
// for the strict object rules to judge.
func unfence(raw []byte) []byte {
	if !bytes.HasPrefix(raw, []byte("```")) || !bytes.HasSuffix(raw, []byte("```")) {
		return raw
	}
	open := bytes.IndexByte(raw, '\n')
	if open < 0 {
		return raw
	}
	if lang := strings.TrimSpace(string(raw[3:open])); lang != "" && !strings.EqualFold(lang, "json") {
		return raw
	}
	body := raw[open+1 : len(raw)-3]
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return raw
	}
	return bytes.TrimSpace(body)
}

func jsonValue(d *json.Decoder, depth int) error {
	if depth > 8 {
		return errors.New("presentation: excessive JSON depth")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return errors.New("presentation: null output field")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] || name != strings.ToLower(name) {
				return errors.New("presentation: duplicate JSON field")
			}
			seen[name] = true
			if err := jsonValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := jsonValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("presentation: unexpected JSON delimiter")
	}
	_, err = d.Token()
	return err
}
