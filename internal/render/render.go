package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

type Options struct {
	Colour   bool
	ShowDiff bool
	Markdown bool
	Width    int
}

var (
	jsonNumberPattern   = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?`)
	jsonBoolNullPattern = regexp.MustCompile(`\b(?:true|false|null)\b`)
)

func DumpJSON(payload threads.Payload, colour bool) (string, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return highlightJSON(string(body), colour), nil
}

func PrintSummary(w io.Writer, payload threads.Payload, opts Options) {
	width := opts.Width
	if width <= 0 {
		width = 80
	}

	colour := opts.Colour
	fmt.Fprintln(w, bold.apply(fmt.Sprintf("%s PR #%d", payload.Repository, payload.PullRequest), colour))
	fmt.Fprintln(w, cyan.apply(fmt.Sprintf("- conversation comments: %d (showing up to 5 below)", len(payload.ConversationComments)), colour))

	for _, comment := range payload.ConversationComments[:min(5, len(payload.ConversationComments))] {
		header := formatCommentHeader(comment.Author, comment.CreatedAt, comment.URL, colour)
		printMultiline(w, "    * ", "      ", header)
		lines := renderCommentBody(comment.Body, opts.Markdown, width)
		for _, line := range lines {
			printMultiline(w, "      ", "      ", line)
		}
	}

	fmt.Fprintln(w, cyan.apply(fmt.Sprintf("- review threads: %d", len(payload.ReviewThreads)), colour))

	for idx, thread := range payload.ReviewThreads {
		path := thread.Path
		if path == "" {
			path = "<no file>"
		}
		line := firstNonNil(thread.Line, thread.OriginalLine)
		lineDisplay := "?"
		if line != nil {
			lineDisplay = fmt.Sprintf("%d", *line)
		}
		status := "unresolved"
		if thread.IsResolved {
			status = "resolved"
		}
		suffix := ""
		if thread.IsOutdated {
			suffix = " (outdated)"
		}
		header := magenta.apply(fmt.Sprintf("    %2d. %s:%s [%s]%s", idx+1, path, lineDisplay, status, suffix), colour)
		fmt.Fprintln(w, header)

		visible := min(2, len(thread.Comments))
		for commentIdx := 0; commentIdx < visible; commentIdx++ {
			comment := thread.Comments[commentIdx]
			header := formatCommentHeader(comment.Author, comment.CreatedAt, comment.URL, colour)
			printMultiline(w, "        - ", "          ", header)
			// Render the snippet first but hold it back: it may carry the body
			// beside its highlighted line, and only it knows whether it did.
			var snippetOut bytes.Buffer
			bodyEmitted := false
			if commentIdx == 0 {
				bodyEmitted = printHistoricalSnippet(&snippetOut, comment.Snippet, colour, opts.Width, comment, opts.Markdown)
			}
			if !bodyEmitted {
				lines := renderCommentBody(comment.Body, opts.Markdown, width)
				for _, line := range lines {
					printMultiline(w, "          ", "          ", line)
				}
			}

			if commentIdx == 0 {
				snippetOut.WriteTo(w)
				if opts.ShowDiff {
					diffLines := formatDiff(comment.DiffHunk, colour, comment.Line, comment.OriginalLine)
					if len(diffLines) > 0 {
						fmt.Fprintln(w, grey.apply("          Diff:", colour))
						for _, line := range diffLines {
							fmt.Fprintf(w, "            %s\n", line)
						}
					}
				}
			}

			if commentIdx < visible-1 {
				fmt.Fprintln(w)
			}
		}

		if len(thread.Comments) > visible {
			fmt.Fprintln(w, "        - …")
		}

		if idx != len(payload.ReviewThreads)-1 {
			fmt.Fprintln(w)
			fmt.Fprintln(w)
		}
	}
}

func summariseComment(author, createdAt string) string {
	if author == "" {
		author = "unknown"
	}
	if createdAt == "" {
		createdAt = "unknown"
	} else if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		createdAt = t.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("%s at %s", author, createdAt)
}

func formatCommentHeader(author, createdAt, url string, colour bool) string {
	base := summariseComment(author, createdAt)
	coloured := yellow.apply(base, colour)
	if url == "" {
		return coloured
	}
	urlText := url
	if colour {
		urlText = blue.apply(url, colour)
	}
	return fmt.Sprintf("%s (%s)", coloured, urlText)
}

func renderCommentBody(body string, markdown bool, width int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	body = strings.ReplaceAll(body, "\r", "")
	if !markdown {
		return strings.Split(body, "\n")
	}
	wrap := clamp(width-12, 40, 120)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		renderer, err = glamour.NewTermRenderer(
			glamour.WithStandardStyle("notty"),
			glamour.WithWordWrap(wrap),
		)
	}
	if err != nil {
		return strings.Split(body, "\n")
	}
	out, err := renderer.Render(body)
	if err != nil {
		if fallbackRenderer, fallbackErr := glamour.NewTermRenderer(
			glamour.WithStandardStyle("notty"),
			glamour.WithWordWrap(wrap),
		); fallbackErr == nil {
			if fallback, fallbackRenderErr := fallbackRenderer.Render(body); fallbackRenderErr == nil {
				out = fallback
				err = nil
			}
		}
	}
	if err != nil {
		return strings.Split(body, "\n")
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func replaceSuggestionBlocks(body string, snippet *threads.HistoricalSnippet, startLine, endLine *int, colour bool, markdown bool) string {
	matches := suggestionBlockRegexp.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return body
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		builder.WriteString(body[last:match[0]])
		content := body[match[2]:match[3]]
		if replacement := renderSuggestionDiff(content, snippet, startLine, endLine, colour, markdown); replacement != "" {
			builder.WriteString(replacement)
		} else {
			builder.WriteString(body[match[0]:match[1]])
		}
		last = match[1]
	}
	builder.WriteString(body[last:])

	return builder.String()
}

func renderSuggestionDiff(content string, snippet *threads.HistoricalSnippet, startLine, endLine *int, colour bool, markdown bool) string {
	start, end, ok := normalizeLineRange(startLine, endLine)
	if !ok {
		return ""
	}
	original := snippetLinesForRange(snippet, start, end)
	if len(original) == 0 {
		return ""
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.TrimSuffix(content, "\n")
	suggestion := strings.Split(content, "\n")
	diffLines := formatSuggestionDiff(original, suggestion, colour, markdown)
	if len(diffLines) == 0 {
		return ""
	}
	if markdown {
		return fmt.Sprintf("```diff\n%s\n```", strings.Join(diffLines, "\n"))
	}
	return strings.Join(diffLines, "\n")
}

func formatSuggestionDiff(original, suggestion []string, colour bool, markdown bool) []string {
	formatted := make([]string, 0, len(original)+len(suggestion))
	for _, line := range original {
		formatted = append(formatted, formatSuggestionLine("-", line, colour, markdown))
	}
	for _, line := range suggestion {
		formatted = append(formatted, formatSuggestionLine("+", line, colour, markdown))
	}
	return formatted
}

func formatSuggestionLine(prefix, line string, colour bool, markdown bool) string {
	text := fmt.Sprintf("%s %s", prefix, line)
	if markdown {
		return text
	}

	switch prefix {
	case "-":
		return red.apply(text, colour)
	case "+":
		return green.apply(text, colour)
	default:
		return text
	}
}

func snippetLinesForRange(snippet *threads.HistoricalSnippet, start, end int) []string {
	if snippet == nil || len(snippet.Lines) == 0 {
		return nil
	}
	snippetStart := snippet.StartLine
	snippetEnd := snippet.StartLine + len(snippet.Lines) - 1
	if start < snippetStart || end > snippetEnd {
		return nil
	}
	startIdx := start - snippetStart
	endIdx := end - snippetStart
	lines := make([]string, endIdx-startIdx+1)
	copy(lines, snippet.Lines[startIdx:endIdx+1])
	return lines
}

func normalizeLineRange(start, end *int) (int, int, bool) {
	if start == nil || end == nil {
		return 0, 0, false
	}
	startVal := *start
	endVal := *end
	if startVal > endVal {
		startVal, endVal = endVal, startVal
	}
	return startVal, endVal, true
}

type diffEntry struct {
	line      string
	highlight bool
}

// printHistoricalSnippet reports whether comment.Body was emitted as part of the
// snippet. Callers must print the body themselves when it returns false: the
// body is only shown beside the highlighted line, and that line is not always
// reached (an out-of-range HighlightLine, or a body that renders to nothing).
func printHistoricalSnippet(w io.Writer, snippet *threads.HistoricalSnippet, colour bool, width int, comment threads.ThreadComment, markdown bool) (bodyEmitted bool) {
	if snippet == nil || len(snippet.Lines) == 0 {
		return false
	}
	startLine, endLine := commentLineRange(comment)
	commit := snippet.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	fmt.Fprintln(w)
	header := fmt.Sprintf("          Code at %s @ %s:", snippet.Path, commit)
	fmt.Fprintln(w, grey.apply(header, colour))
	for idx, line := range snippet.Lines {
		lineNo := snippet.StartLine + idx
		isHighlight := lineNo == snippet.HighlightLine
		content := emphasize(line, colour, isHighlight)
		fmt.Fprintf(w, "            %5d  %s\n", lineNo, content)
		if isHighlight && printCommentBlock(w, comment.Body, width, colour, markdown, snippet, startLine, endLine) {
			bodyEmitted = true
		}
	}
	return bodyEmitted
}

func formatDiff(diff string, colour bool, newLine, oldLine *int) []string {
	if diff == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(diff, "\r", ""), "\n")
	entries := make([]diffEntry, 0, len(lines))

	var currentNew, currentOld *int

	parseMeta := func(token string) *int {
		if len(token) < 2 {
			return nil
		}
		token = token[1:]
		if idx := strings.Index(token, ","); idx >= 0 {
			token = token[:idx]
		}
		val, err := strconv.Atoi(token)
		if err != nil {
			return nil
		}
		v := val - 1
		return &v
	}

	for _, raw := range lines {
		switch {
		case strings.HasPrefix(raw, "@@"):
			parts := strings.Fields(raw)
			if len(parts) >= 3 {
				currentOld = parseMeta(parts[1])
				currentNew = parseMeta(parts[2])
			}
			entries = append(entries, diffEntry{line: magenta.apply(raw, colour)})
			continue
		case strings.HasPrefix(raw, "+"):
			if currentNew == nil {
				currentNew = ptr(0)
			}
			*currentNew++
			highlight := newLine != nil && currentNew != nil && *currentNew == *newLine
			line := green.apply(raw, colour)
			entries = append(entries, diffEntry{line: emphasize(line, colour, highlight), highlight: highlight})
		case strings.HasPrefix(raw, "-"):
			if currentOld == nil {
				currentOld = ptr(0)
			}
			*currentOld++
			highlight := oldLine != nil && currentOld != nil && *currentOld == *oldLine
			line := red.apply(raw, colour)
			entries = append(entries, diffEntry{line: emphasize(line, colour, highlight), highlight: highlight})
		default:
			if currentNew != nil {
				*currentNew++
			}
			if currentOld != nil {
				*currentOld++
			}
			highlight := (newLine != nil && currentNew != nil && *currentNew == *newLine) ||
				(oldLine != nil && currentOld != nil && *currentOld == *oldLine)
			line := grey.apply(raw, colour)
			entries = append(entries, diffEntry{line: emphasize(line, colour, highlight), highlight: highlight})
		}
	}

	trimmed := trimAroundHighlights(entries, newLine != nil || oldLine != nil)
	out := make([]string, len(trimmed))
	for i, entry := range trimmed {
		out[i] = entry.line
	}
	return out
}

func commentLineRange(comment threads.ThreadComment) (*int, *int) {
	end := firstNonNil(comment.OriginalLine, comment.Line)
	start := end
	if comment.StartLine != nil {
		start = comment.StartLine
	}
	if start == nil || end == nil {
		return nil, nil
	}
	startVal := *start
	endVal := *end
	if startVal > endVal {
		startVal, endVal = endVal, startVal
	}
	return &startVal, &endVal
}

func trimAroundHighlights(entries []diffEntry, hasTarget bool) []diffEntry {
	if !hasTarget {
		if len(entries) > 15 {
			return entries[:15]
		}
		return entries
	}
	indexes := make([]int, 0)
	for idx, e := range entries {
		if e.highlight {
			indexes = append(indexes, idx)
		}
	}
	if len(indexes) == 0 {
		if len(entries) > 15 {
			return entries[:15]
		}
		return entries
	}
	start := max(0, indexes[0]-7)
	end := min(len(entries), indexes[len(indexes)-1]+8)
	return entries[start:end]
}

func printMultiline(w io.Writer, first, rest, text string) {
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		prefix := rest
		if idx == 0 {
			prefix = first
		}
		fmt.Fprintf(w, "%s%s\n", prefix, line)
	}
}

func firstNonNil(values ...*int) *int {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func clamp(value, minValue, maxValue int) int {
	return max(minValue, min(maxValue, value))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ptr(value int) *int {
	return &value
}

func emphasize(text string, colour bool, highlight bool) string {
	if !highlight {
		return text
	}
	if colour {
		return fmt.Sprintf("\x1b[1m%s\x1b[0m", text)
	}
	return fmt.Sprintf(">> %s", text)
}

func highlightJSON(body string, colour bool) string {
	if !colour || body == "" {
		return body
	}

	var out strings.Builder
	last := 0
	inString := false
	escaped := false
	stringStart := 0

	writePlain := func(segment string) {
		out.WriteString(colourPlainJSON(segment))
	}

	for idx, r := range body {
		if !inString {
			if r == '"' {
				writePlain(body[last:idx])
				inString = true
				stringStart = idx
				escaped = false
			}
			continue
		}

		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			segment := body[stringStart : idx+1]
			out.WriteString(colourJSONString(segment, body[idx+1:]))
			last = idx + 1
			inString = false
		}
	}

	if last < len(body) {
		writePlain(body[last:])
	}

	return out.String()
}

func colourPlainJSON(segment string) string {
	segment = jsonNumberPattern.ReplaceAllStringFunc(segment, func(match string) string {
		return magenta.apply(match, true)
	})
	segment = jsonBoolNullPattern.ReplaceAllStringFunc(segment, func(match string) string {
		return green.apply(match, true)
	})
	return segment
}

func colourJSONString(segment string, remainder string) string {
	for i := 0; i < len(remainder); i++ {
		switch remainder[i] {
		case ' ', '\t', '\r', '\n':
			continue
		case ':':
			return cyan.apply(segment, true)
		default:
			return yellow.apply(segment, true)
		}
	}
	return yellow.apply(segment, true)
}

type colouriser struct {
	code string
}

func (c colouriser) apply(text string, enabled bool) string {
	if !enabled || text == "" {
		return text
	}
	return fmt.Sprintf("\x1b[%sm%s\x1b[0m", c.code, text)
}

var (
	bold      = colouriser{code: "1"}
	cyan      = colouriser{code: "36"}
	magenta   = colouriser{code: "35"}
	yellow    = colouriser{code: "33"}
	blue      = colouriser{code: "34"}
	green     = colouriser{code: "32"}
	red       = colouriser{code: "31"}
	grey      = colouriser{code: "90"}
	darkGreen = colouriser{code: "32"}
)

// printCommentBlock reports whether it printed anything, so the caller can fall
// back to printing the body itself.
func printCommentBlock(w io.Writer, body string, width int, colour bool, markdown bool, snippet *threads.HistoricalSnippet, startLine, endLine *int) bool {
	blockWidth := clamp(width-12, 40, 120)
	innerWidth := blockWidth - 4
	if innerWidth < 10 {
		innerWidth = blockWidth
	}

	lines := compactSnippetLines(renderCommentSnippet(body, markdown, innerWidth, snippet, startLine, endLine, colour))
	if len(lines) == 0 {
		return false
	}

	border := "┌" + strings.Repeat("─", innerWidth+2) + "┐"
	bottom := "└" + strings.Repeat("─", innerWidth+2) + "┘"
	borderColour := func(text string) string {
		if colour {
			return darkGreen.apply(text, true)
		}
		return text
	}
	left := "│"
	right := "│"
	if colour {
		left = darkGreen.apply(left, true)
		right = darkGreen.apply(right, true)
	}

	fmt.Fprintf(w, "            %s\n", borderColour(border))
	for _, line := range lines {
		content := padRightVisible(line, innerWidth)
		fmt.Fprintf(w, "            %s %s %s\n", left, content, right)
	}
	fmt.Fprintf(w, "            %s\n", borderColour(bottom))
	return true
}

func renderCommentSnippet(body string, markdown bool, width int, snippet *threads.HistoricalSnippet, startLine, endLine *int, colour bool) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	resolvedBody := replaceSuggestionBlocks(body, snippet, startLine, endLine, colour, markdown)
	if !markdown {
		return wrapCommentSnippetLines(resolvedBody, width)
	}

	return renderCommentBody(resolvedBody, markdown, width+12)
}

func wrapCommentSnippetLines(body string, limit int) []string {
	if limit <= 0 {
		limit = 60
	}
	rawLines := strings.Split(body, "\n")
	var lines []string
	for _, raw := range rawLines {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		wrapped := wrapPlainLine(raw, limit)
		lines = append(lines, wrapped...)
	}
	return lines
}

func wrapPlainLine(line string, limit int) []string {
	if limit <= 0 || utf8.RuneCountInString(line) <= limit {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	var result []string
	var builder strings.Builder
	currentLen := 0
	for _, word := range words {
		wordLen := utf8.RuneCountInString(word)
		if currentLen == 0 {
			builder.WriteString(word)
			currentLen = wordLen
			continue
		}
		if currentLen+1+wordLen > limit {
			result = append(result, builder.String())
			builder.Reset()
			builder.WriteString(word)
			currentLen = wordLen
		} else {
			builder.WriteString(" ")
			builder.WriteString(word)
			currentLen += 1 + wordLen
		}
	}
	if builder.Len() > 0 {
		result = append(result, builder.String())
	}
	return result
}

var suggestionBlockRegexp = regexp.MustCompile("(?s)```suggestion[^\\n]*\\n(.*?)\\n?```")

func padRight(text string, width int) string {
	if width <= 0 {
		return text
	}
	length := utf8.RuneCountInString(text)
	if length >= width {
		return text
	}
	return text + strings.Repeat(" ", width-length)
}

var ansiEscapeRegexp = regexp.MustCompile("\x1b\\[[0-9;]*m")

func padRightVisible(text string, width int) string {
	if width <= 0 {
		return text
	}
	visible := utf8.RuneCountInString(ansiEscapeRegexp.ReplaceAllString(text, ""))
	if visible >= width {
		return text
	}
	return text + strings.Repeat(" ", width-visible)
}

// isVisuallyBlank reports whether a line carries no visible text. Judging this
// on the raw string would treat an ANSI-coloured blank line as non-blank, which
// made padding survive in colour mode but not when piped.
func isVisuallyBlank(line string) bool {
	return strings.TrimSpace(ansiEscapeRegexp.ReplaceAllString(line, "")) == ""
}

// compactSnippetLines trims blank padding from the ends of a rendered comment
// while preserving interior blanks: paragraph breaks and blank lines inside
// fenced code blocks are part of the content, not padding.
func compactSnippetLines(lines []string) []string {
	start := 0
	for start < len(lines) && isVisuallyBlank(lines[start]) {
		start++
	}
	end := len(lines)
	for end > start && isVisuallyBlank(lines[end-1]) {
		end--
	}
	if start >= end {
		return nil
	}
	out := make([]string, end-start)
	copy(out, lines[start:end])
	return out
}
