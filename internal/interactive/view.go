package interactive

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/VRTFinland/gh-threads/internal/threads"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

var (
	threadHighlightStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("236")).Background(lipgloss.Color("29"))
	commentHighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Bold(true).PaddingLeft(1)
	selectedMarkerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	normalStyle           = lipgloss.NewStyle()
	topBarStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("22")).Bold(true).Padding(0, 1)
	bottomBarStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("253"))
	dividerStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	detailStyle           = lipgloss.NewStyle().Padding(1, 1)
	authorStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Background(lipgloss.NoColor{}).Bold(true)
	aiAuthorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Background(lipgloss.NoColor{}).Bold(true)
	timeStyle             = lipgloss.NewStyle()
	linkStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Underline(true)
	commentHeaderStyle    = lipgloss.NewStyle().PaddingLeft(1)
	commentBodyStyle      = lipgloss.NewStyle().PaddingLeft(1)
	replyHeaderStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("211")).Bold(true)
	replyPaneStyle        = lipgloss.NewStyle().
				Foreground(lipgloss.Color("231")).
				Background(lipgloss.Color("245")).
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("245")).
				PaddingLeft(1).PaddingRight(1)
	sweepScreenSeq       = "\x1b[0J"
	lineNumberStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	lineNumberHighlight  = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Bold(true)
	helpTitleStyle       = lipgloss.NewStyle().Bold(true)
	helpKeyStyle         = lipgloss.NewStyle().Bold(true)
	helpBoxStyle         = lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder())
	pathHeaderStyle      = lipgloss.NewStyle().Bold(true)
	listLineNumberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	snippetRendererOnce  sync.Once
	snippetRenderer      *glamour.TermRenderer
	snippetRendererErr   error
	markdownRendererOnce sync.Once
	markdownRenderer     *glamour.TermRenderer
	markdownRendererErr  error
)

var suggestionBlockRegexp = regexp.MustCompile("(?s)```suggestion[^\\n]*\\n(.*?)\\n?```")
var aiAuthorAliases = map[string][]string{
	"codex":    {"codex"},
	"co-pilot": {"copilot", "co-pilot", "co pilot", "github copilot"},
	"claude":   {"claude"},
	"gemini":   {"gemini", "google gemini"},
}

func RenderView(state Model, width int, height int, listHeight int, detailHeight int, showStatus bool, statusIndex int, showFilter bool, filterIndex int, showHelp bool, currentState State, replyInput textarea.Model, filterInput textinput.Model, inputPurpose string, authorSuggestionIndex int, statusSuggestionIndex int) string {
	if height <= 0 {
		height = 60
	}
	if width <= 0 {
		width = 80
	}
	listHeight = vmax(1, listHeight)
	detailHeight = vmax(1, detailHeight)

	var b strings.Builder
	b.WriteString(renderTopBar(state, width))
	b.WriteString("\n")
	b.WriteString(renderThreadList(state, listHeight, width))
	b.WriteString("\n")
	b.WriteString(renderDivider(width))
	b.WriteString("\n")
	detail := renderDetailBlock(state, detailHeight, width, currentState, replyInput, filterInput, inputPurpose, showStatus, statusIndex, showFilter, filterIndex, showHelp, authorSuggestionIndex, statusSuggestionIndex)
	b.WriteString(detail)
	b.WriteString("\n")
	b.WriteString(renderBottomBar(state, width))
	b.WriteString(sweepScreenSeq)
	return b.String()
}

func renderThreadList(state Model, height int, width int) string {
	threads := state.FilteredThreads()
	if len(threads) == 0 {
		return normalizeBlock("No threads match current filters.", width, height)
	}
	window := vmax(1, height)
	start := clamp(state.listOffset, 0, max(0, len(threads)-window))
	pathCounts := summarizePaths(threads)
	lines, selectionLine := buildThreadListLines(threads, start, window, height, width, state.selectedThread, pathCounts)
	for (selectionLine == -1 || selectionLine >= height) && start < len(threads)-1 {
		start++
		lines, selectionLine = buildThreadListLines(threads, start, window, height, width, state.selectedThread, pathCounts)
	}
	content := strings.Join(lines, "\n")
	return normalizeBlock(content, width, height)
}

func buildThreadListLines(threads []threads.ReviewThread, start int, window int, height int, width int, selectedThread int, pathCounts map[string]pathSummary) ([]string, int) {
	lines := make([]string, 0, window+4)
	selectionLine := -1
	end := vmin(len(threads), start+window)
	lastPath := ""
	for idx := start; idx < end; idx++ {
		thread := threads[idx]
		if idx == start || thread.Path != lastPath {
			counts := pathCounts[thread.Path]
			header := fmt.Sprintf("%s [%d resolved, %d unresolved]", thread.Path, counts.resolved, counts.unresolved)
			lines = append(lines, pathHeaderStyle.Render(padListLine(header, width)))
			lastPath = thread.Path
		}
		isLastInPath := idx == len(threads)-1 || threads[idx+1].Path != thread.Path
		line := renderThreadListEntry(thread, isLastInPath, selectedThread == idx, width)
		lines = append(lines, line)
		if selectedThread == idx {
			selectionLine = len(lines) - 1
		}
		if len(lines) >= height {
			break
		}
	}
	return lines, selectionLine
}

func renderThreadListEntry(thread threads.ReviewThread, isLastInPath bool, selected bool, width int) string {
	branch := "├─"
	if isLastInPath {
		branch = "╰─"
	}
	status := "⬜"
	if thread.IsResolved {
		status = "✅"
	}
	lineNumbers := listLineNumberStyle.Render(fmt.Sprintf("[L%s]", formatThreadLines(thread)))
	author := displayAuthor(firstCommentAuthor(thread))
	prefix := fmt.Sprintf("%s %s %s - %s: ", branch, status, lineNumbers, author)
	available := width - 1 - lipgloss.Width(prefix)
	if available < 1 {
		available = 1
	}
	preview := threadPreview(thread, available)
	parts := prefix + preview
	rendered := padListLine(parts, width)
	if selected {
		return threadHighlightStyle.Render(rendered)
	}
	return rendered
}

func formatThreadLines(thread threads.ReviewThread) string {
	if thread.StartLine != nil && thread.Line != nil && *thread.StartLine != *thread.Line {
		return fmt.Sprintf("%d-%d", *thread.StartLine, *thread.Line)
	}
	if thread.Line != nil {
		return fmt.Sprintf("%d", *thread.Line)
	}
	if thread.StartLine != nil && thread.OriginalLine != nil && *thread.StartLine != *thread.OriginalLine {
		return fmt.Sprintf("%d-%d", *thread.StartLine, *thread.OriginalLine)
	}
	if thread.OriginalLine != nil {
		return fmt.Sprintf("%d", *thread.OriginalLine)
	}
	return "?"
}

func firstCommentAuthor(thread threads.ReviewThread) string {
	if len(thread.Comments) == 0 {
		return "unknown"
	}
	author := strings.TrimSpace(thread.Comments[0].Author)
	if author == "" {
		return "unknown"
	}
	return author
}

func threadPreview(thread threads.ReviewThread, maxWidth int) string {
	if len(thread.Comments) == 0 {
		return "(no comment)"
	}
	body := strings.TrimSpace(thread.Comments[0].Body)
	sanitized := stripSuggestionBlocks(body)
	suggestionOnly := isSuggestionBody(body) && strings.TrimSpace(sanitized) == ""
	rendered := renderCommentMarkdown(sanitized)
	joined := strings.TrimSpace(strings.Join(rendered, " "))
	if suggestionOnly {
		return "suggestion..."
	}
	if joined == "" {
		return "(no comment)"
	}
	if maxWidth < 1 {
		maxWidth = 1
	}
	if lipgloss.Width(joined) > maxWidth {
		return xansi.Truncate(joined, maxWidth, "...")
	}
	return joined
}

func isSuggestionBody(body string) bool {
	return strings.Contains(strings.ToLower(body), "```suggestion")
}

func stripSuggestionBlocks(body string) string {
	return suggestionBlockRegexp.ReplaceAllString(body, "")
}

func displayAuthor(name string) string {
	if ai, ok := aiDisplayName(name); ok {
		return aiAuthorStyle.Render(ai)
	}
	clean := strings.TrimSpace(name)
	if clean == "" {
		clean = "unknown"
	}
	return authorStyle.Render(clean)
}

func aiDisplayName(name string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	for display, aliases := range aiAuthorAliases {
		for _, alias := range aliases {
			if strings.Contains(lower, alias) {
				return fmt.Sprintf("🤖 %s", display), true
			}
		}
	}
	return "", false
}

type pathSummary struct {
	resolved   int
	unresolved int
}

func summarizePaths(threads []threads.ReviewThread) map[string]pathSummary {
	counts := make(map[string]pathSummary)
	for _, thread := range threads {
		current := counts[thread.Path]
		if thread.IsResolved {
			current.resolved++
		} else {
			current.unresolved++
		}
		counts[thread.Path] = current
	}
	return counts
}

func padListLine(text string, width int) string {
	return padOrTrim(" "+text, width)
}

func renderDetailBlock(state Model, height int, width int, currentState State, replyInput textarea.Model, filterInput textinput.Model, inputPurpose string, showStatus bool, statusIndex int, showFilter bool, filterIndex int, showHelp bool, authorSuggestionIndex int, statusSuggestionIndex int) string {
	if showHelp {
		return centerBlock(renderHelp(width), width, height)
	}
	sections := make([]string, 0, 4)
	sections = append(sections, renderDetailContent(state, height, currentState, replyInput, width))

	if currentState == StateFilter {
		sections = append(sections, filterInput.View())
	}
	if currentState == StateFilter && inputPurpose == "author" {
		if suggestions := state.AuthorSuggestions(filterInput.Value(), authorSuggestionLimit); len(suggestions) > 0 {
			sections = append(sections, renderAuthorSuggestions(suggestions, authorSuggestionIndex))
		}
	}
	if currentState == StateFilter && inputPurpose == "status" {
		if suggestions := statusSuggestions(filterInput.Value()); len(suggestions) > 0 {
			sections = append(sections, renderStatusSuggestions(suggestions, statusSuggestionIndex))
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

func renderReplyPane(content string, width int) string {
	if width <= 0 {
		return content
	}
	style := replyPaneStyle.Copy().Width(width)
	frame := style.GetHorizontalFrameSize()
	innerWidth := width - frame
	if innerWidth < 1 {
		innerWidth = 1
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = padOrTrim(line, innerWidth)
	}
	return style.Render(strings.Join(lines, "\n"))
}

func renderDetailContent(state Model, maxHeight int, currentState State, replyInput textarea.Model, width int) string {
	thread, ok := state.SelectedThread()
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(detailStyle.Render(fmt.Sprintf("%s:%v %s", thread.Path, firstNonNilString(thread.Line, thread.OriginalLine), detailStatus(thread))))
	b.WriteString("\n")
	if len(thread.Comments) == 0 {
		return b.String()
	}

	maxComments := vmax(1, maxHeight/6)
	if !state.detailExpanded {
		maxComments = vmin(len(thread.Comments), vmax(3, maxComments))
	}
	if maxComments > len(thread.Comments) {
		maxComments = len(thread.Comments)
	}
	selected := clamp(state.selectedComment, 0, len(thread.Comments)-1)
	start := 0
	if selected >= maxComments {
		start = selected - maxComments + 1
	}
	end := vmin(len(thread.Comments), start+maxComments)

	for i := start; i < end; i++ {
		comment := thread.Comments[i]
		header := fmt.Sprintf("%s %s", displayAuthor(comment.Author), timeStyle.Render("at "+comment.CreatedAt))
		if comment.URL != "" {
			header = fmt.Sprintf("%s (%s)", header, linkStyle.Render(comment.URL))
		}
		headerMarker := "  "
		if i == state.selectedComment {
			headerMarker = selectedMarkerStyle.Render(" >")
		}
		indentWidth := vmax(2, lipgloss.Width(xansi.Strip(headerMarker)))
		bodyMarker := strings.Repeat(" ", indentWidth)
		if i == state.selectedComment {
			b.WriteString(headerMarker)
			b.WriteString(commentHighlightStyle.Render(header))
		} else {
			b.WriteString(headerMarker)
			b.WriteString(commentHeaderStyle.Render(header))
		}
		b.WriteString("\n")
		body := strings.TrimSpace(comment.Body)
		for _, line := range renderCommentMarkdown(body) {
			b.WriteString(bodyMarker)
			b.WriteString(commentBodyStyle.Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if currentState == StateReply && i == state.selectedComment {
			header := strings.TrimSpace(renderReplyTarget(state))
			if header != "" {
				b.WriteString(header)
				b.WriteString("\n")
			}
			b.WriteString(renderReplyPane(replyInput.View(), width))
			b.WriteString("\n\n")
		}
		insertBlankAfterSnippet := false
		if comment.Snippet != nil && i == 0 {
			snippetLines := snippetDisplayLines(comment.Snippet)
			for offset, line := range snippetLines {
				lineNo := comment.Snippet.StartLine + offset
				marker := "  "
				lineLabel := lineNumberStyle.Render(fmt.Sprintf("%5d", lineNo))
				if lineNo == comment.Snippet.HighlightLine {
					marker = " >"
					lineLabel = lineNumberHighlight.Render(fmt.Sprintf("%5d", lineNo))
				}
				b.WriteString(fmt.Sprintf("%s %s  %s\n", marker, lineLabel, line))
			}
			insertBlankAfterSnippet = true
		}
		if state.detailMode == detailDiff && comment.DiffHunk != "" {
			if insertBlankAfterSnippet {
				b.WriteString("\n")
			}
			diffLines := formatDiffHunk(comment.DiffHunk, comment.Line, comment.OriginalLine)
			for _, line := range diffLines {
				b.WriteString(line)
				b.WriteString("\n")
			}
			insertBlankAfterSnippet = true
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

func renderAuthorSuggestions(matches []string, selected int) string {
	if len(matches) == 0 {
		return ""
	}
	if selected >= len(matches) {
		selected = -1
	}
	var b strings.Builder
	b.WriteString("Matching authors:\n")
	for idx, name := range matches {
		prefix := "  "
		if idx == selected {
			prefix = "> "
		}
		b.WriteString(prefix + name + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderStatusSuggestions(matches []statusOption, selected int) string {
	if len(matches) == 0 {
		return ""
	}
	if selected >= len(matches) {
		selected = -1
	}
	var b strings.Builder
	b.WriteString("Statuses:\n")
	for idx, option := range matches {
		prefix := "  "
		if idx == selected {
			prefix = "> "
		}
		b.WriteString(prefix + option.label + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderHelp(width int) string {
	type helpEntry struct {
		keys     []string
		desc     string
		boldKeys bool
	}
	entries := []helpEntry{
		{keys: []string{"j", "k", "↑", "↓"}, desc: "Move between threads", boldKeys: false},
		{keys: []string{"h", "←"}, desc: "Previous comment", boldKeys: true},
		{keys: []string{"l", "→"}, desc: "Next comment", boldKeys: true},
		{keys: []string{"enter", "space"}, desc: "Toggle detail", boldKeys: true},
		{keys: []string{"d"}, desc: "Toggle diff view", boldKeys: true},
		{keys: []string{"/"}, desc: "Text filter", boldKeys: true},
		{keys: []string{"a"}, desc: "Author filter", boldKeys: true},
		{keys: []string{"s"}, desc: "Status filter (a/r/u)", boldKeys: true},
		{keys: []string{"f"}, desc: "Cycle status filter", boldKeys: true},
		{keys: []string{"r"}, desc: "Reply to selected comment", boldKeys: true},
		{keys: []string{"S"}, desc: "Set resolved/unresolved", boldKeys: true},
		{keys: []string{"R"}, desc: "Refresh data", boldKeys: true},
		{keys: []string{"?"}, desc: "Close help / show help", boldKeys: true},
		{keys: []string{"q", "ctrl+c"}, desc: "Quit", boldKeys: true},
	}

	var b strings.Builder
	b.WriteString(helpTitleStyle.Render("Help / Keybindings"))
	b.WriteString("\n\n")
	for idx, entry := range entries {
		keyParts := make([]string, len(entry.keys))
		for i, key := range entry.keys {
			if entry.boldKeys {
				keyParts[i] = helpKeyStyle.Render(key)
			} else {
				keyParts[i] = key
			}
		}
		line := fmt.Sprintf("%-18s %s", strings.Join(keyParts, " / "), entry.desc)
		b.WriteString(line)
		if idx != len(entries)-1 {
			b.WriteString("\n")
		}
	}
	content := b.String()
	boxWidth := vmax(30, vmin(width-4, maxLineWidth(content)+4))
	return helpBoxStyle.Width(boxWidth).Render(content)
}

func maxLineWidth(text string) int {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	maxWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

func centerBlock(text string, width int, height int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	blockHeight := len(lines)
	topPadding := 0
	if height > blockHeight {
		topPadding = (height - blockHeight) / 2
	}
	for i := 0; i < topPadding; i++ {
		lines = append([]string{""}, lines...)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		lineWidth := lipgloss.Width(line)
		leftPad := 0
		if width > lineWidth {
			leftPad = (width - lineWidth) / 2
		}
		padded := strings.Repeat(" ", leftPad) + line
		lines[i] = padOrTrim(padded, width)
	}
	return strings.Join(lines, "\n")
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

func detailStatus(thread *threads.ReviewThread) string {
	if thread != nil && thread.IsResolved {
		return "✅"
	}
	return "⬜ unresolved"
}

func renderReplyTarget(state Model) string {
	thread, ok := state.SelectedThread()
	if !ok || len(thread.Comments) == 0 {
		return ""
	}
	idx := clamp(state.selectedComment, 0, len(thread.Comments)-1)
	comment := thread.Comments[idx]

	header := fmt.Sprintf("Replying to comment #%d by %s", idx+1, strings.TrimSpace(xansi.Strip(displayAuthor(comment.Author))))
	if comment.URL != "" {
		header = fmt.Sprintf("%s (%s)", header, linkStyle.Render(comment.URL))
	}
	return replyHeaderStyle.Render(header)
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

func sectionHeights(total int, listLines int) (int, int) {
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
	maxList := vmax(1, listLines)
	list = vmin(list, maxList)
	if list >= content {
		list = vmax(1, content-1)
	}
	detail := content - list
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
