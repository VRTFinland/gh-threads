package threads

import (
	"regexp"
	"strings"
)

// suggestionBlockRegexp matches a GitHub suggestion fence and captures the
// replacement text inside it. Both renderers used to carry a copy, which is a
// pattern subtle enough -- the lazy body, the optional final newline -- that
// two of them is one too many.
var suggestionBlockRegexp = regexp.MustCompile("(?s)```suggestion[^\\n]*\\n(.*?)\\n?```")

// HasSuggestion reports whether the body opens a suggestion fence at all, which
// is looser than the fences ReplaceSuggestions rewrites: a fence left unclosed
// still tells the reader the comment is a suggestion.
func HasSuggestion(body string) bool {
	return strings.Contains(strings.ToLower(body), "```suggestion")
}

// ReplaceSuggestions rewrites every complete suggestion fence in body. replace
// is handed the suggested text and returns what to put in the fence's place;
// returning false leaves that fence exactly as it was written.
func ReplaceSuggestions(body string, replace func(suggested string) (string, bool)) string {
	matches := suggestionBlockRegexp.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return body
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		builder.WriteString(body[last:match[0]])
		if replacement, ok := replace(body[match[2]:match[3]]); ok {
			builder.WriteString(replacement)
		} else {
			builder.WriteString(body[match[0]:match[1]])
		}
		last = match[1]
	}
	builder.WriteString(body[last:])
	return builder.String()
}

// StripSuggestions removes every complete suggestion fence from body.
func StripSuggestions(body string) string {
	return ReplaceSuggestions(body, func(string) (string, bool) { return "", true })
}
