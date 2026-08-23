package naming

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// MaxSlugLength is the character budget for the slugified title. The issue
	// number that precedes it in a branch name is never counted against this
	// and is never truncated.
	MaxSlugLength = 40

	// FallbackSlug is used when slugification leaves nothing behind — a title
	// that is entirely punctuation, emoji or non-Latin script.
	FallbackSlug = "task"

	// DefaultFeaturePrefix is the leading branch component when a repository
	// configures none.
	DefaultFeaturePrefix = "lf"
)

// slugPattern is the post-condition every slug must satisfy. It is written
// here as the literal expression from AC-3.7 so the code and the acceptance
// criterion can be compared by eye: lowercase alphanumerics and interior
// hyphens only, never leading, trailing or doubled.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Slug derives the feature slug from an issue title, per D-4 and S-6:
//
//  1. lowercase the title;
//  2. replace every run of non-alphanumeric characters with a single hyphen;
//  3. strip leading and trailing hyphens;
//  4. truncate to [MaxSlugLength], cutting back to the last hyphen so no
//     partial word survives;
//  5. strip a trailing hyphen;
//  6. an empty result becomes [FallbackSlug].
//
// "Alphanumeric" means ASCII a-z and 0-9 and nothing else. Accented Latin,
// Cyrillic, CJK and emoji are all separators, which is what makes rule 6 fire
// for a title written entirely in another script — a deliberate trade: a
// mechanically safe branch name matters more than a descriptive one, and the
// issue number carries the identity.
//
// Derivation is deterministic and depends on nothing but the title, so the
// slug can be recomputed after the database is lost (AC-3.6). Once derived it
// is frozen on the sessions row and reused; later runs must not re-derive it,
// because editing the issue title would otherwise move the branch.
//
// The returned slug is checked against [ValidateSlug] before it is returned.
// An error therefore means the derivation above is broken, not that the title
// was unusable — there is no unusable title.
func Slug(title string) (string, error) {
	slug := truncateSlug(slugify(title))
	if slug == "" {
		slug = FallbackSlug
	}
	if err := ValidateSlug(slug); err != nil {
		return "", fmt.Errorf("derive slug from title %q: %w", title, err)
	}
	return slug, nil
}

// ValidateSlug reports whether s is a legal frozen slug. Callers holding a
// slug that came back out of the database should run it through here before
// building a branch or a path from it: the value was written by an older
// version of this code and is only as trustworthy as that version was.
func ValidateSlug(s string) error {
	if !slugPattern.MatchString(s) {
		return fmt.Errorf("%w: %q does not match %s", ErrInvalidSlug, s, slugPattern)
	}
	if len(s) > MaxSlugLength {
		return fmt.Errorf("%w: %q is %d characters, over the %d limit", ErrInvalidSlug, s, len(s), MaxSlugLength)
	}
	return nil
}

// Branch builds the branch name for an issue: <featurePrefix>/<issue>-<slug>,
// for example "lf/123-fix-the-thing". An empty featurePrefix means
// [DefaultFeaturePrefix].
//
// The slug is validated rather than derived, because the caller holds the slug
// frozen on the sessions row and that value — not a fresh derivation — is what
// the branch must agree with.
func Branch(featurePrefix string, issue int, slug string) (string, error) {
	prefix := featurePrefix
	if prefix == "" {
		prefix = DefaultFeaturePrefix
	}
	if err := ValidateFeaturePrefix(prefix); err != nil {
		return "", err
	}
	if err := validateIssue(issue); err != nil {
		return "", err
	}
	if err := ValidateSlug(slug); err != nil {
		return "", fmt.Errorf("branch for issue %d: %w", issue, err)
	}
	return prefix + "/" + strconv.Itoa(issue) + "-" + slug, nil
}

// ValidateFeaturePrefix checks a configured feature_prefix against the subset
// of git-check-ref-format that applies to a leading branch component. A prefix
// may contain slashes — "bots/lf" is a legal namespace — but each component
// must stand on its own.
//
// This runs at config validation time as well as here, so a bad prefix is a
// startup error rather than a mystery halfway through the first turn.
func ValidateFeaturePrefix(prefix string) error {
	fail := func(reason string) error {
		return fmt.Errorf("%w: %q %s", ErrInvalidFeaturePrefix, prefix, reason)
	}
	if prefix == "" {
		return fail("is empty")
	}
	if strings.Contains(prefix, "@{") {
		return fail(`contains "@{", which git reserves for reflog references`)
	}
	for _, component := range strings.Split(prefix, "/") {
		switch {
		case component == "":
			return fail("has an empty path component")
		case strings.HasPrefix(component, "-"), strings.HasPrefix(component, "."):
			return fail("has a component starting with a hyphen or a dot")
		case strings.HasSuffix(component, "-"), strings.HasSuffix(component, "."):
			return fail("has a component ending with a hyphen or a dot")
		case strings.Contains(component, ".."):
			return fail(`contains ".."`)
		case strings.HasSuffix(component, ".lock"):
			return fail(`has a component ending in ".lock", which git reserves`)
		}
		for _, r := range component {
			if !isPrefixRune(r) {
				return fail(fmt.Sprintf("contains %q", r))
			}
		}
	}
	return nil
}

// slugify applies rules 1 to 3: lowercase, collapse every run of
// non-alphanumerics to a single hyphen, and drop leading and trailing hyphens.
// All three happen in one pass — a hyphen is only emitted once a following
// alphanumeric proves it was interior.
func slugify(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	separatorPending := false
	for _, r := range strings.ToLower(title) {
		if !isSlugRune(r) {
			separatorPending = true
			continue
		}
		if separatorPending && b.Len() > 0 {
			b.WriteByte('-')
		}
		separatorPending = false
		b.WriteRune(r)
	}
	return b.String()
}

// truncateSlug applies rules 4 and 5 to an already-slugified string.
//
// The cut only walks back to the previous hyphen when it would otherwise land
// inside a word. When the character just past the budget is itself a hyphen,
// the budget already ends on a word boundary and cutting back would throw away
// a whole word for nothing.
//
// A single word longer than the budget has no hyphen to retreat to. It is hard
// cut at the budget rather than reduced to the empty string: a truncated word
// is a worse name but a working one, and rule 6 must not fire just because
// someone wrote a long compound title.
func truncateSlug(s string) string {
	if len(s) <= MaxSlugLength {
		return strings.TrimRight(s, "-")
	}
	cut := s[:MaxSlugLength]
	if s[MaxSlugLength] != '-' {
		if i := strings.LastIndexByte(cut, '-'); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.TrimRight(cut, "-")
}

// isSlugRune reports whether r survives slugification. Everything else is a
// separator, including every non-ASCII rune.
func isSlugRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// isPrefixRune reports whether r may appear in a feature prefix component.
func isPrefixRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '-':
		return true
	default:
		return false
	}
}
