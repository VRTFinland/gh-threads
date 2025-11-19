package render

import (
	"encoding/json"
	"fmt"
	"io"
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

func DumpJSON(payload threads.Payload) (string, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
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
			lines := renderCommentBody(comment.Body, opts.Markdown, width)
			for _, line := range lines {
				printMultiline(w, "          ", "          ", line)
			}

			if commentIdx == 0 {
				snippetShown := printHistoricalSnippet(w, comment.Snippet, colour, opts.Width, comment.Body)
				if opts.ShowDiff && !snippetShown {
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
		return strings.Split(body, "\n")
	}
	out, err := renderer.Render(body)
	if err != nil {
		return strings.Split(body, "\n")
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

type diffEntry struct {
	line      string
	highlight bool
}

func printHistoricalSnippet(w io.Writer, snippet *threads.HistoricalSnippet, colour bool, width int, commentBody string) bool {
	if snippet == nil || len(snippet.Lines) == 0 {
		return false
	}
	commit := snippet.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	header := fmt.Sprintf("          Code at %s @ %s:", snippet.Path, commit)
	fmt.Fprintln(w, grey.apply(header, colour))
	for idx, line := range snippet.Lines {
		lineNo := snippet.StartLine + idx
		isHighlight := lineNo == snippet.HighlightLine
		content := emphasize(line, colour, isHighlight)
		fmt.Fprintf(w, "            %5d  %s\n", lineNo, content)
		if isHighlight {
			printCommentBlock(w, commentBody, width, colour)
		}
	}
	return true
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
	bold    = colouriser{code: "1"}
	cyan    = colouriser{code: "36"}
	magenta = colouriser{code: "35"}
	yellow  = colouriser{code: "33"}
	blue    = colouriser{code: "34"}
	green   = colouriser{code: "32"}
	red     = colouriser{code: "31"}
	grey    = colouriser{code: "90"}
)

const (
	snippetCommentBackground = "\x1b[48;5;238m\x1b[97m"
	ansiReset                = "\x1b[0m"
)

func printCommentBlock(w io.Writer, body string, width int, colour bool) {
	blockWidth := clamp(width-12, 40, 120)
	margin := 2
	innerWidth := blockWidth - margin*2
	if innerWidth < 10 {
		innerWidth = blockWidth
		margin = 0
	}

	lines := wrapCommentSnippetLines(body, innerWidth)
	if len(lines) == 0 {
		return
	}

	formatLine := func(content string) string {
		padding := strings.Repeat(" ", margin)
		return padding + padRight(content, innerWidth) + padding
	}

	printBackgroundLine := func(content string) {
		formatted := formatLine(content)
		if colour {
			fmt.Fprintf(w, "            %s%s%s\n", snippetCommentBackground, formatted, ansiReset)
		} else {
			fmt.Fprintf(w, "            [comment] %s\n", formatted)
		}
	}

	printBackgroundLine("")
	for _, line := range lines {
		printBackgroundLine(line)
	}
	printBackgroundLine("")
}

func wrapCommentSnippetLines(body string, limit int) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
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
