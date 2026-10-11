package presentation

import (
	"strconv"
	"strings"
	"unicode"
)

// FromSource adapts an authoritative redacted result without interpreting its
// truth from prose. Producers supply status; uncertain legacy producers retain
// their complete caveat. Parsing here only selects exact rendering spans.
func FromSource(raw string, status Status, language string) Outcome {
	o := Outcome{Version: Version, Status: status, RawText: raw, Language: language}

	start := 0
	if marker := strings.Index(raw, "\n[lf:ask:v1]\n"); strings.HasPrefix(raw, "> ") && marker >= 0 {
		start = marker + len("\n[lf:ask:v1]\n")
		o.Protected = append(o.Protected, Protected{Handle: "ask_prefix", Kind: AskPrefix, Source: Source{End: start, Value: raw[:start]}})
	}
	o.Facts = []Fact{{Handle: "result", Source: Source{Start: start, End: len(raw), Value: raw[start:]}, Mandatory: status != Success}}
	for _, line := range strings.SplitAfter(raw[start:], "\n") {
		value := strings.TrimSuffix(line, "\n")
		fixed := strings.ContainsAny(value, "`[]/:") || protectedProse.MatchString(value) || strings.Contains(value, "Token:") || strings.HasPrefix(value, "page ") || strings.HasPrefix(value, "next:")
		for _, r := range value {
			fixed = fixed || unicode.IsNumber(r)
		}
		if fixed && value != "" {
			o.Protected = append(o.Protected, Protected{Handle: "fixed_" + strconv.Itoa(len(o.Protected)), Kind: ExactValue, Source: Source{Start: start, End: start + len(value), Value: value}})
		}
		start += len(line)
	}
	return o
}

// DetectLanguage is deliberately conservative: indeterminate questions use
// English. It receives only the latest question, never prior answer content.
func DetectLanguage(question string) string {
	lower := " " + strings.Join(strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) }), " ") + " "
	for _, r := range question {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			return "Japanese"
		case unicode.In(r, unicode.Hangul):
			return "Korean"

		}
	}
	for _, option := range []struct {
		language string
		words    []string
	}{
		{"German", []string{" wie ", " warum ", " bitte ", " zeige ", " erklärt ", " ist ", " sind "}},
		{"Spanish", []string{" qué ", " cómo ", " por qué ", " muestra ", " puedes ", " estado "}},
		{"French", []string{" pourquoi ", " comment ", " quel ", " quelle ", " affiche ", " peux "}},
		{"Portuguese", []string{" como ", " por que ", " mostre ", " você "}},
		{"Russian", []string{" почему ", " как ", " покажи "}},
		{"Arabic", []string{" كيف ", " لماذا "}},
		{"Italian", []string{" perché ", " quale ", " mostra ", " puoi "}},
	} {
		for _, word := range option.words {
			if strings.Contains(lower, word) {
				return option.language
			}
		}
	}
	if strings.Contains(question, "什么") || strings.Contains(question, "如何") || strings.Contains(question, "怎么") {
		return "Chinese"
	}
	return "English"
}
