package interactive

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/VRTFinland/gh-threads/internal/threads"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

var (
	threadHighlightStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("152"))
	commentHighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("61")).PaddingLeft(2)
	normalStyle           = lipgloss.NewStyle()
	topBarStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("57")).Bold(true).Padding(0, 1)
	bottomBarStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("253"))
	dividerStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	detailStyle           = lipgloss.NewStyle().Padding(1, 2)
	authorStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	timeStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	linkStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Underline(true)
	commentHeaderStyle    = lipgloss.NewStyle().PaddingLeft(2)
	commentBodyStyle      = lipgloss.NewStyle().PaddingLeft(2)
	replyHeaderStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("211")).Bold(true)
	sweepScreenSeq        = "\x1b[0J"
	lineNumberStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	lineNumberHighlight   = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Bold(true)
	snippetRendererOnce   sync.Once
	snippetRenderer       *glamour.TermRenderer
	snippetRendererErr    error
	markdownRendererOnce  sync.Once
	markdownRenderer      *glamour.TermRenderer
	markdownRendererErr   error
)

func RenderView(state Model, width int, height int, listHeight int, detailHeight int, showStatus bool, statusIndex int, showFilter bool, filterIndex int, currentState State, input textinput.Model, inputPurpose string) string {
	if height <= 0 {
		height = 60
	}
	if width <= 0 {
		width = 80
	}
	listHeight = max(3, listHeight)
	detailHeight = max(3, detailHeight)

	var b strings.Builder
	b.WriteString(renderTopBar(state, width))
	b.WriteString("\n")
	b.WriteString(renderThreadList(state, listHeight, width))
	b.WriteString("\n")
	b.WriteString(renderDivider(width))
	b.WriteString("\n")
	detail := renderDetailBlock(state, detailHeight, width, currentState, input, inputPurpose, showStatus, statusIndex, showFilter, filterIndex)
	b.WriteString(detail)
	b.WriteString("\n")
	b.WriteString(renderBottomBar(state, width))
	b.WriteString(sweepScreenSeq)
	return b.String()
}

func renderThreadList(state Model, height int, width int) string {
	var b strings.Builder
	threads := state.FilteredThreads()
	if len(threads) == 0 {
		return normalizeBlock("No threads match current filters.", width, height)
	}
	window := vmax(1, height)
	start := clamp(state.listOffset, 0, max(0, len(threads)-window))
	end := vmin(len(threads), start+window)
	for idx := start; idx < end; idx++ {
		thread := threads[idx]
		line := fmt.Sprintf("%3d. %-60s:%v [%s] (%d)", idx+1, thread.Path, firstNonNilString(thread.Line, thread.OriginalLine), threadStatus(thread), len(thread.Comments))
		if idx == state.selectedThread {
			b.WriteString(threadHighlightStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		if idx < end-1 {
			b.WriteString("\n")
		}
	}
	return normalizeBlock(b.String(), width, height)
}

func renderDetailBlock(state Model, height int, width int, currentState State, input textinput.Model, inputPurpose string, showStatus bool, statusIndex int, showFilter bool, filterIndex int) string {
	sections := make([]string, 0, 4)
	sections = append(sections, renderDetailContent(state, height))

	if currentState == StateReply {
		if replyTarget := strings.TrimSpace(renderReplyTarget(state)); replyTarget != "" {
			sections = append(sections, replyTarget)
		}
	}
	if currentState == StateReply || currentState == StateFilter {
		sections = append(sections, input.View())
	}
	if currentState == StateFilter && inputPurpose == "author" {
		if suggestions := state.AuthorSuggestions(input.Value(), 6); len(suggestions) > 0 {
			sections = append(sections, renderAuthorSuggestions(suggestions))
		}
	}
	if showStatus {
		sections = append(sections, renderStatusPicker(statusIndex))
	}
	if showFilter {
		sections = append(sections, renderFilterPicker(filterIndex))
	}

	content := strings.Join(filterEmptySections(sections), "\n\n")
	return normalizeBlock(content, width, height)
}

func filterEmptySections(parts []string) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, part)
		}
	}
	return result
}

func renderDetailContent(state Model, maxHeight int) string {
	thread, ok := state.SelectedThread()
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(detailStyle.Render(fmt.Sprintf("%s:%v [%s]", thread.Path, firstNonNilString(thread.Line, thread.OriginalLine), threadStatus(*thread))))
	b.WriteString("\n")
	if len(thread.Comments) == 0 {
		return b.String()
	}

	maxComments := vmax(1, maxHeight/6)
	if !state.detailExpanded {
		maxComments = 1
	}
	if maxComments > len(thread.Comments) {
		maxComments = len(thread.Comments)
	}
	start := 0
	if state.detailExpanded && len(thread.Comments) > maxComments {
		start = state.selectedComment - maxComments/2
		if start < 0 {
			start = 0
		}
		if start+maxComments > len(thread.Comments) {
			start = len(thread.Comments) - maxComments
		}
	}
	end := start + maxComments

	for i := start; i < end; i++ {
		comment := thread.Comments[i]
		header := fmt.Sprintf("%s %s", authorStyle.Render(comment.Author), timeStyle.Render("at "+comment.CreatedAt))
		if comment.URL != "" {
			header = fmt.Sprintf("%s (%s)", header, linkStyle.Render(comment.URL))
		}
		if i == state.selectedComment {
			b.WriteString(commentHighlightStyle.Render(header))
		} else {
			b.WriteString(commentHeaderStyle.Render(header))
		}
		b.WriteString("\n")
		body := strings.TrimSpace(comment.Body)
		for _, line := range renderCommentMarkdown(body) {
			b.WriteString(commentBodyStyle.Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		insertBlankAfterSnippet := false
		if state.detailMode == detailSnippet && comment.Snippet != nil && i == 0 {
			snippetLines := snippetDisplayLines(comment.Snippet)
			for offset, line := range snippetLines {
				lineNo := comment.Snippet.StartLine + offset
				marker := "  "
				lineLabel := lineNumberStyle.Render(fmt.Sprintf("%5d", lineNo))
				if lineNo == comment.Snippet.HighlightLine {
					marker = ">>"
					lineLabel = lineNumberHighlight.Render(fmt.Sprintf("%5d", lineNo))
				}
				b.WriteString(fmt.Sprintf("%s %s  %s\n", marker, lineLabel, line))
			}
			insertBlankAfterSnippet = true
		} else if state.detailMode == detailDiff && comment.DiffHunk != "" {
			diffLines := formatDiffHunk(comment.DiffHunk, comment.Line, comment.OriginalLine)
			for _, line := range diffLines {
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
		if insertBlankAfterSnippet && i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderTopBar(state Model, width int) string {
	url := state.PRURL()
	if url == "" {
		url = "unknown"
	}
	text := fmt.Sprintf("PR: %s | author: %s | comments: %d | mergeable: %s", url, state.PRAuthor(), state.TotalCommentCount(), state.MergeableState())
	return topBarStyle.Width(width).Render(padOrTrim(text, width))
}

func renderBottomBar(state Model, width int) string {
	if width <= 0 {
		return ""
	}
	left := fmt.Sprintf("filters: author=%s status=%s text=%s", state.filters.Author, state.filters.Status, state.filters.Text)
	if state.errMessage != "" {
		left = left + " | err: " + state.errMessage
	} else if state.infoMessage != "" {
		left = left + " | " + state.infoMessage
	}
	right := "Press ? for help"
	contentWidth := width
	if width > 1 {
		contentWidth = width - 1
	}
	line := alignLeftRight(left, right, contentWidth)
	line = padOrTrim(line, contentWidth)
	if width > 1 {
		line = line + " "
	}
	return bottomBarStyle.Width(width).Render(padOrTrim(line, width))
}

func renderDivider(width int) string {
	if width <= 0 {
		width = 80
	}
	return dividerStyle.Render(strings.Repeat("-", width))
}

func renderStatusPicker(index int) string {
	options := []string{"resolved", "unresolved"}
	var b strings.Builder
	b.WriteString("Set status:\n")
	for i, option := range options {
		prefix := "  "
		if i == index {
			prefix = "> "
		}
		b.WriteString(prefix + option + "\n")
	}
	return b.String()
}

func renderFilterPicker(index int) string {
	options := []string{"all", "resolved", "unresolved"}
	var b strings.Builder
	b.WriteString("Filter threads by status:\n")
	for i, option := range options {
		prefix := "  "
		if i == index {
			prefix = "> "
		}
		b.WriteString(prefix + option + "\n")
	}
	return b.String()
}

func renderAuthorSuggestions(matches []string) string {
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Matching authors:\n")
	for _, name := range matches {
		b.WriteString("  " + name + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func firstNonNilString(values ...*int) string {
	for _, v := range values {
		if v != nil {
			return fmt.Sprintf("%d", *v)
		}
	}
	return "?"
}

func threadStatus(thread threads.ReviewThread) string {
	if thread.IsResolved {
		return "resolved"
	}
	return "unresolved"
}

func renderReplyTarget(state Model) string {
	thread, ok := state.SelectedThread()
	if !ok || len(thread.Comments) == 0 {
		return ""
	}
	idx := clamp(state.selectedComment, 0, len(thread.Comments)-1)
	comment := thread.Comments[idx]

	header := fmt.Sprintf("Replying to comment #%d by %s", idx+1, comment.Author)
	if comment.URL != "" {
		header = fmt.Sprintf("%s (%s)", header, linkStyle.Render(comment.URL))
	}

	body := replyPreview(comment.Body)

	var b strings.Builder
	b.WriteString(replyHeaderStyle.Render(header))
	if body != "" {
		b.WriteString("\n")
		b.WriteString(commentBodyStyle.Render(body))
	}
	return b.String()
}

func replyPreview(body string) string {
	text := strings.TrimSpace(body)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	const maxLines = 3
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	const maxLineLen = 120
	for i, line := range lines {
		runes := []rune(strings.TrimSpace(line))
		if len(runes) > maxLineLen {
			runes = append(runes[:maxLineLen-1], '…')
		}
		lines[i] = string(runes)
	}
	return strings.Join(lines, "\n")
}

func normalizeBlock(text string, width int, height int) string {
	if height <= 0 {
		return ""
	}
	clean := strings.TrimRight(text, "\n")
	var lines []string
	if clean == "" {
		lines = []string{}
	} else {
		lines = strings.Split(clean, "\n")
	}
	if len(lines) < height {
		lines = append(lines, make([]string, height-len(lines))...)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := 0; i < len(lines); i++ {
		lines[i] = padOrTrim(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func padOrTrim(text string, width int) string {
	if width <= 0 {
		return sanitizeLine(text)
	}
	line := sanitizeLine(text)
	current := lipgloss.Width(line)
	if current == width {
		return line
	}
	if current < width {
		return line + strings.Repeat(" ", width-current)
	}
	runes := []rune(line)
	if width <= 3 {
		if width > len(runes) {
			width = len(runes)
		}
		return string(runes[:width])
	}
	if width-3 > len(runes) {
		return line
	}
	return string(runes[:width-3]) + "..."
}

func sanitizeLine(text string) string {
	return strings.ReplaceAll(text, "\n", " ")
}

func buildSingleLine(left, right string, width int) string {
	if width <= 0 {
		return sanitizeLine(left)
	}
	rightClean := sanitizeLine(right)
	rightWidth := lipgloss.Width(rightClean)
	space := 1
	leftWidth := width - rightWidth - space
	if leftWidth < 0 {
		leftWidth = 0
		space = 0
	}
	leftClean := ""
	if leftWidth > 0 {
		leftClean = padOrTrim(left, leftWidth)
	}
	var b strings.Builder
	b.WriteString(leftClean)
	if space > 0 {
		b.WriteString(strings.Repeat(" ", space))
	}
	b.WriteString(rightClean)
	return padOrTrim(b.String(), width)
}

func alignLeftRight(left, right string, width int) string {
	if width <= 0 {
		if left == "" {
			return sanitizeLine(right)
		}
		if right == "" {
			return sanitizeLine(left)
		}
		return sanitizeLine(left) + " " + sanitizeLine(right)
	}
	rightClean := sanitizeLine(right)
	rightWidth := lipgloss.Width(rightClean)
	if rightWidth >= width {
		return padOrTrim(rightClean, width)
	}
	space := 1
	leftWidth := width - rightWidth - space
	if leftWidth < 0 {
		leftWidth = 0
		space = 0
	}
	leftClean := ""
	if leftWidth > 0 {
		leftClean = padOrTrim(left, leftWidth)
	}
	var b strings.Builder
	b.WriteString(leftClean)
	if space > 0 {
		b.WriteString(strings.Repeat(" ", space))
	}
	b.WriteString(rightClean)
	return b.String()
}

func snippetDisplayLines(snippet *threads.HistoricalSnippet) []string {
	if snippet == nil || len(snippet.Lines) == 0 {
		return nil
	}
	if lines, ok := highlightSnippet(snippet); ok {
		return lines
	}
	copyLines := make([]string, len(snippet.Lines))
	copy(copyLines, snippet.Lines)
	return copyLines
}

func highlightSnippet(snippet *threads.HistoricalSnippet) ([]string, bool) {
	renderer, err := loadSnippetRenderer()
	if err != nil || renderer == nil {
		return nil, false
	}
	code := strings.Join(snippet.Lines, "\n")
	language := snippetLanguage(snippet.Path)
	block := fmt.Sprintf("```%s\n%s\n```", language, code)
	rendered, err := renderer.Render(block)
	if err != nil {
		return nil, false
	}
	lines := cleanupHighlightedLines(rendered, snippet.Lines)
	if lines == nil {
		return nil, false
	}
	return lines, true
}

func cleanupHighlightedLines(rendered string, original []string) []string {
	rendered = strings.ReplaceAll(rendered, "\r", "")
	rendered = strings.TrimRight(rendered, "\n")
	lines := strings.Split(rendered, "\n")
	result := make([]string, 0, len(original))
	idx := 0
	for _, orig := range original {
		if idx >= len(lines) {
			return nil
		}
		current := lines[idx]
		origBlank := strings.TrimSpace(orig) == ""
		for lineIsEffectivelyBlank(current) && !origBlank {
			idx++
			if idx >= len(lines) {
				return nil
			}
			current = lines[idx]
		}
		result = append(result, current)
		idx++
	}
	if len(result) != len(original) {
		return nil
	}
	return result
}

func lineIsEffectivelyBlank(line string) bool {
	if stripped := strings.TrimSpace(line); stripped == "" {
		return true
	}
	plain := strings.TrimSpace(xansi.Strip(line))
	return plain == ""
}

func loadSnippetRenderer() (*glamour.TermRenderer, error) {
	snippetRendererOnce.Do(func() {
		style := snippetStyleConfig()
		snippetRenderer, snippetRendererErr = glamour.NewTermRenderer(
			glamour.WithStyles(style),
			glamour.WithColorProfile(termenv.TrueColor),
			glamour.WithWordWrap(0),
			glamour.WithPreservedNewLines(),
		)
	})
	return snippetRenderer, snippetRendererErr
}

func snippetStyleConfig() ansi.StyleConfig {
	style := styles.DarkStyleConfig
	if !termenv.HasDarkBackground() {
		style = styles.LightStyleConfig
	}
	zero := uint(0)
	style.Document.Margin = nil
	style.Document.StylePrimitive.BlockPrefix = ""
	style.Document.StylePrimitive.BlockSuffix = ""
	style.CodeBlock.Margin = &zero
	style.CodeBlock.Indent = &zero
	style.CodeBlock.StylePrimitive.BlockPrefix = ""
	style.CodeBlock.StylePrimitive.BlockSuffix = ""
	return style
}

func snippetLanguage(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return ""
	}
	switch ext {
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "jsx":
		return "jsx"
	case "tsx":
		return "tsx"
	case "sh", "bash", "zsh":
		return "bash"
	case "yml":
		return "yaml"
	case "rb":
		return "ruby"
	case "c", "h":
		return "c"
	case "hpp", "hxx", "cc", "cpp", "cxx":
		return "cpp"
	case "tf":
		return "hcl"
	default:
		return ext
	}
}

func renderCommentMarkdown(body string) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return []string{""}
	}
	renderer, err := loadMarkdownRenderer()
	if err != nil || renderer == nil {
		return strings.Split(body, "\n")
	}
	out, err := renderer.Render(body)
	if err != nil {
		return strings.Split(body, "\n")
	}
	out = strings.TrimRight(out, "\n")
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func loadMarkdownRenderer() (*glamour.TermRenderer, error) {
	markdownRendererOnce.Do(func() {
		style := snippetStyleConfig()
		markdownRenderer, markdownRendererErr = glamour.NewTermRenderer(
			glamour.WithStyles(style),
			glamour.WithColorProfile(termenv.TrueColor),
			glamour.WithWordWrap(0),
			glamour.WithPreservedNewLines(),
		)
	})
	return markdownRenderer, markdownRendererErr
}

type diffEntry struct {
	line      string
	highlight bool
}

func formatDiffHunk(diff string, targetNew *int, targetOld *int) []string {
	clean := strings.ReplaceAll(diff, "\r", "")
	clean = strings.Trim(clean, "\n")
	if clean == "" {
		return nil
	}
	rawLines := strings.Split(clean, "\n")
	entries := make([]diffEntry, 0, len(rawLines))
	var currentNew, currentOld int
	var hasNew, hasOld bool
	for _, raw := range rawLines {
		if strings.HasPrefix(raw, "@@") {
			if parts := strings.Fields(raw); len(parts) >= 3 {
				if start, ok := parseHunkStart(parts[1]); ok {
					currentOld = start - 1
					hasOld = true
				} else {
					hasOld = false
				}
				if start, ok := parseHunkStart(parts[2]); ok {
					currentNew = start - 1
					hasNew = true
				} else {
					hasNew = false
				}
			}
			entries = append(entries, diffEntry{line: raw})
			continue
		}
		highlight := false
		switch {
		case strings.HasPrefix(raw, "+"):
			if !hasNew {
				hasNew = true
				currentNew = 0
			}
			currentNew++
			if targetNew != nil && currentNew == *targetNew {
				highlight = true
			}
		case strings.HasPrefix(raw, "-"):
			if !hasOld {
				hasOld = true
				currentOld = 0
			}
			currentOld++
			if targetOld != nil && currentOld == *targetOld {
				highlight = true
			}
		default:
			if hasNew {
				currentNew++
			}
			if hasOld {
				currentOld++
			}
			if (targetNew != nil && hasNew && currentNew == *targetNew) || (targetOld != nil && hasOld && currentOld == *targetOld) {
				highlight = true
			}
		}
		line := raw
		if highlight {
			line = ">> " + raw
		}
		entries = append(entries, diffEntry{line: line, highlight: highlight})
	}
	if len(entries) <= 15 {
		return collectDiffLines(entries)
	}
	highlightIdx := make([]int, 0, len(entries))
	for idx, entry := range entries {
		if entry.highlight {
			highlightIdx = append(highlightIdx, idx)
		}
	}
	start := 0
	end := vmin(len(entries), 15)
	if len(highlightIdx) > 0 {
		start = max(0, highlightIdx[0]-7)
		end = vmin(len(entries), highlightIdx[len(highlightIdx)-1]+8)
	}
	if end-start > 15 {
		end = start + 15
		if end > len(entries) {
			end = len(entries)
		}
	}
	return collectDiffLines(entries[start:end])
}

func collectDiffLines(entries []diffEntry) []string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.line)
	}
	return lines
}

func parseHunkStart(meta string) (int, bool) {
	meta = strings.TrimSpace(meta)
	if meta == "" {
		return 0, false
	}
	if meta[0] == '+' || meta[0] == '-' {
		meta = meta[1:]
	}
	if idx := strings.Index(meta, ","); idx != -1 {
		meta = meta[:idx]
	}
	value, err := strconv.Atoi(meta)
	if err != nil {
		return 0, false
	}
	return value, true
}

func sectionHeights(total int) (int, int) {
	if total <= 2 {
		return 1, vmax(1, total-2)
	}
	content := total - 2
	divider := 1
	if content <= divider {
		return 1, vmax(1, content-divider)
	}
	content -= divider
	if content < 2 {
		return 1, 1
	}
	list := vmax(3, content*3/10)
	if list > content-3 {
		list = vmax(1, content-3)
	}
	detail := content - list
	if detail < 1 {
		detail = 1
	}
	return list, detail
}

func vmax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func vmin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
