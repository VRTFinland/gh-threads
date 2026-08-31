package interactive

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/VRTFinland/gh-threads/internal/diff"
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

var aiAuthorAliases = map[string][]string{
	"codex":    {"codex"},
	"co-pilot": {"copilot", "co-pilot", "co pilot", "github copilot"},
	"claude":   {"claude"},
	"gemini":   {"gemini", "google gemini"},
}

// viewInputs carries the widgets and cursors that live on the tea model rather
// than in Model. Grouping them keeps the render signatures short and, unlike a
// positional list of bools and ints, makes a call site self-describing.
type viewInputs struct {
	reply                 textarea.Model
	filter                textinput.Model
	purpose               string
	statusIndex           int
	authorSuggestionIndex int
	statusSuggestionIndex int
}

func RenderView(state Model, width, height, listHeight, detailHeight int, in viewInputs) string {
	if height <= 0 {
		height = 60
	}
	if width <= 0 {
		width = 80
	}
	listHeight = max(1, listHeight)
	detailHeight = max(1, detailHeight)

	var b strings.Builder
	b.WriteString(renderTopBar(state, width))
	b.WriteString("\n")
	b.WriteString(renderThreadList(state, listHeight, width))
	b.WriteString("\n")
	b.WriteString(renderDivider(width))
	b.WriteString("\n")
	detail := renderDetailBlock(state, width, detailHeight, in)
	b.WriteString(detail)
	b.WriteString("\n")
	b.WriteString(renderBottomBar(state, width))
	b.WriteString(sweepScreenSeq)
	return b.String()
}

func renderThreadList(state Model, height int, width int) string {
	list := state.FilteredThreads()
	if len(list) == 0 {
		return normalizeBlock("No threads match current filters.", width, height)
	}
	height = max(1, height)
	pathCounts := summarizePaths(list)
	start := threadListStart(list, state.listOffset, state.selectedThread, height)
	lines := buildThreadListLines(list, start, height, width, state.selectedThread, pathCounts)
	return normalizeBlock(strings.Join(lines, "\n"), width, height)
}

// The thread list is priced in whole lines: an entry takes one, and the path
// header above the first entry of each path takes one more.
const (
	threadRowLines  = 1
	pathHeaderLines = 1
)

// listRowCost is what drawing the entry at idx costs when the window starts at
// start. Everything that measures the list goes through here -- the estimate
// that sizes the pane, the scroll calculation, and the draw itself -- because
// the three drifting apart would leave the pane's height quietly disagreeing
// with what is drawn in it.
func listRowCost(list []threads.ReviewThread, idx, start int) int {
	if idx == start || list[idx].Path != list[idx-1].Path {
		return threadRowLines + pathHeaderLines
	}
	return threadRowLines
}

// listLineEstimate is how tall the thread list would be with nothing clipping
// it, which is what the pane asks for before it is told its share.
func listLineEstimate(list []threads.ReviewThread) int {
	if len(list) == 0 {
		return 1 // the "no threads" placeholder still needs its line
	}
	lines := 0
	for idx := range list {
		lines += listRowCost(list, idx, 0)
	}
	return lines
}

// threadListStart picks the first thread to draw so the selected one always
// fits within height lines, scrolling back no further than the model's offset.
// Line cost is monotone in start, so a single backward walk from the selection
// is exact -- the old code re-rendered the whole window on every retry, and
// could never satisfy a height that left no room for a path header.
func threadListStart(list []threads.ReviewThread, desired, selected, height int) int {
	selected = clamp(selected, 0, len(list)-1)
	// A stale offset past the selection must never win, or the selected thread
	// scrolls off the top and the pane looks frozen.
	desired = clamp(desired, 0, selected)
	start := selected
	used := min(listRowCost(list, selected, selected), height)
	for candidate := selected - 1; candidate >= desired; candidate-- {
		// Prepending the candidate costs its own row and header, and refunds
		// the header the entry below no longer opens when they share a path.
		next := used + listRowCost(list, candidate, candidate)
		if list[candidate].Path == list[candidate+1].Path {
			next -= pathHeaderLines
		}
		if next > height {
			break
		}
		used, start = next, candidate
	}
	return start
}

func buildThreadListLines(list []threads.ReviewThread, start int, height int, width int, selectedThread int, pathCounts map[string]pathSummary) []string {
	lines := make([]string, 0, height)
	for idx := start; idx < len(list) && len(lines) < height; idx++ {
		thread := list[idx]
		if listRowCost(list, idx, start) > threadRowLines {
			// A header only earns a line when the entry it introduces fits too.
			// The first one may be dropped -- at height 1 there is room for the
			// selected entry only -- but a mid-list one may not, or the entry
			// below would appear to belong to the previous path.
			fits := len(lines)+pathHeaderLines < height
			if !fits && idx != start {
				break
			}
			if fits {
				counts := pathCounts[thread.Path]
				header := fmt.Sprintf("%s [%d resolved, %d unresolved]", thread.Path, counts.resolved, counts.unresolved)
				lines = append(lines, pathHeaderStyle.Render(padListLine(header, width)))
			}
		}
		isLastInPath := idx == len(list)-1 || list[idx+1].Path != thread.Path
		lines = append(lines, renderThreadListEntry(thread, isLastInPath, selectedThread == idx, width))
	}
	return lines
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

// formatThreadLines renders the thread's position in the list, preferring the
// current diff: that is where the reader is looking, and an outdated thread
// falls back to the commit it was written against.
func formatThreadLines(thread threads.ReviewThread) string {
	anchor := thread.Anchor(threads.CurrentSpace)
	if !anchor.Valid() {
		return "?"
	}
	return anchor.String()
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
	sanitized := threads.StripSuggestions(body)
	suggestionOnly := threads.HasSuggestion(body) && strings.TrimSpace(sanitized) == ""
	rendered := cachedCommentMarkdown(sanitized)
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

func renderDetailBlock(state Model, width, height int, in viewInputs) string {
	if state.state == StateHelp {
		return centerBlock(renderHelp(width), width, height)
	}
	detail, anchor := buildDetailContent(state, width, height, in.reply)

	extras := make([]string, 0, 3)
	if state.state == StateFilter {
		extras = append(extras, in.filter.View())
	}
	if state.state == StateFilter && in.purpose == "author" {
		if suggestions := state.AuthorSuggestions(in.filter.Value(), authorSuggestionLimit); len(suggestions) > 0 {
			extras = append(extras, renderAuthorSuggestions(suggestions, in.authorSuggestionIndex))
		}
	}
	if state.state == StateFilter && in.purpose == "status" {
		if suggestions := statusSuggestions(in.filter.Value()); len(suggestions) > 0 {
			extras = append(extras, renderStatusSuggestions(suggestions, in.statusSuggestionIndex))
		}
	}
	if state.state == StateStatus {
		extras = append(extras, renderStatusPicker(in.statusIndex))
	}
	extrasBlock := strings.Join(filterEmptySections(extras), "\n\n")

	// The prompt claims the pane before the detail does: the user is typing into
	// it, so it must stay visible even in a short pane. Keeping the head of the
	// block keeps the input itself, trimming its suggestions first.
	extrasHeight := 0
	if extrasBlock != "" {
		extrasLines := strings.Split(strings.TrimRight(extrasBlock, "\n"), "\n")
		if maxExtras := max(1, height-2); len(extrasLines) > maxExtras {
			extrasLines = extrasLines[:maxExtras]
			extrasBlock = strings.Join(extrasLines, "\n")
		}
		extrasHeight = len(extrasLines) + 1 // plus the blank separator line
	}
	// Never leave the detail without a line: at zero or less windowDetailBlock
	// passes the content through unwindowed, and normalizeBlock would then keep
	// its head and cut the prompt off the bottom.
	detail = windowDetailBlock(detail, max(1, height-extrasHeight), anchor, state.state == StateReply)

	content := strings.Join(filterEmptySections([]string{detail, extrasBlock}), "\n\n")
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

// detailAnchor marks the line range of the detail block that must stay on
// screen: the selected comment, extended to cover the reply editor while
// replying.
type detailAnchor struct {
	start int
	end   int
}

func buildDetailContent(state Model, width, maxHeight int, replyInput textarea.Model) (string, detailAnchor) {
	anchor := detailAnchor{start: -1, end: -1}
	thread, ok := state.SelectedThread()
	if !ok {
		return "", anchor
	}
	var b strings.Builder
	lineCount := func() int { return strings.Count(b.String(), "\n") }
	b.WriteString(detailStyle.Render(fmt.Sprintf("%s:%s %s", thread.Path, formatThreadLines(*thread), detailStatus(thread))))
	b.WriteString("\n")
	if len(thread.Comments) == 0 {
		return b.String(), anchor
	}

	maxComments := detailCommentBudget(maxHeight, state.detailExpanded, len(thread.Comments))
	selected := clamp(state.selectedComment, 0, len(thread.Comments)-1)
	start := 0
	if selected >= maxComments {
		start = selected - maxComments + 1
	}
	end := min(len(thread.Comments), start+maxComments)

	for i := start; i < end; i++ {
		comment := thread.Comments[i]
		header := fmt.Sprintf("%s %s", displayAuthor(comment.Author), timeStyle.Render("at "+comment.CreatedAt))
		if comment.URL != "" {
			header = fmt.Sprintf("%s (%s)", header, linkStyle.Render(comment.URL))
		}
		headerMarker := "  "
		if i == selected {
			headerMarker = selectedMarkerStyle.Render(" >")
		}
		indentWidth := max(2, lipgloss.Width(xansi.Strip(headerMarker)))
		bodyMarker := strings.Repeat(" ", indentWidth)
		if i == selected {
			anchor.start = lineCount()
			b.WriteString(headerMarker)
			b.WriteString(commentHighlightStyle.Render(header))
		} else {
			b.WriteString(headerMarker)
			b.WriteString(commentHeaderStyle.Render(header))
		}
		b.WriteString("\n")
		body := strings.TrimSpace(comment.Body)
		for _, line := range cachedCommentMarkdown(body) {
			b.WriteString(bodyMarker)
			b.WriteString(commentBodyStyle.Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if i == selected {
			anchor.end = lineCount()
		}
		if state.state == StateReply && i == selected {
			header := strings.TrimSpace(renderReplyTarget(state))
			if header != "" {
				b.WriteString(header)
				b.WriteString("\n")
			}
			b.WriteString(renderReplyPane(replyInput.View(), width))
			b.WriteString("\n\n")
			anchor.end = lineCount()
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
	return b.String(), anchor
}

// detailCommentBudget reports how many comments the detail pane may draw.
// Expanded fits as many as the pane height allows; collapsed is a compact peek
// at the selected comment, and must never show more than expanded would.
func detailCommentBudget(maxHeight int, expanded bool, total int) int {
	if !expanded {
		return 1
	}
	return max(1, min(total, max(1, maxHeight/6)))
}

// windowDetailBlock scrolls content so the anchored range stays visible within
// height lines. When the range is taller than the window, preferEnd keeps its
// end on screen (the reply editor, where the cursor sits) instead of its start.
func windowDetailBlock(content string, height int, anchor detailAnchor, preferEnd bool) string {
	if height <= 0 || anchor.start < 0 {
		return content
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) <= height {
		return content
	}
	end := min(anchor.end, len(lines))
	start := end - height
	if start > anchor.start && !preferEnd {
		start = anchor.start
	}
	start = clamp(start, 0, len(lines)-height)
	return strings.Join(lines[start:start+height], "\n")
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
	boxWidth := max(30, min(width-4, lipgloss.Width(content)+4))
	return helpBoxStyle.Width(boxWidth).Render(content)
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
	// Slicing runes here counted ANSI escapes as content and wide runes as one
	// column: it could cut mid escape sequence, bleeding colour into the rest of
	// the screen, and left CJK or emoji lines wider than the pane.
	tail := "..."
	if width <= 3 {
		tail = ""
	}
	trimmed := xansi.Truncate(line, width, tail)
	// A wide rune cannot always land on the limit exactly, so pad the remainder
	// to keep every pane line the same width.
	if short := width - lipgloss.Width(trimmed); short > 0 {
		trimmed += strings.Repeat(" ", short)
	}
	return trimmed
}

func sanitizeLine(text string) string {
	return strings.ReplaceAll(text, "\n", " ")
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

// snippetDisplayLines syntax-highlights a snippet. The render is a chroma lex
// plus a goldmark parse -- by far the most expensive thing in a frame -- and it
// ran again on every keystroke, so it is memoised on the snippet's content just
// like comment markdown.
func snippetDisplayLines(snippet *threads.HistoricalSnippet) []string {
	if snippet == nil || len(snippet.Lines) == 0 {
		return nil
	}
	key := snippet.Path + "\x00" + strings.Join(snippet.Lines, "\n")
	return snippetCache.get(key, func() []string {
		if lines, ok := highlightSnippet(snippet); ok {
			return lines
		}
		copyLines := make([]string, len(snippet.Lines))
		copy(copyLines, snippet.Lines)
		return copyLines
	})
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

const (
	markdownCacheLimit = 1024
	snippetCacheLimit  = 256
)

// renderCache memoises expensive glamour renders by content. Both renderers are
// package-level singletons with a fixed wrap and colour profile, so their output
// depends on nothing but the key -- and keying on content rather than an ID
// means an edited comment can never serve a stale render. A size cap bounds
// growth over a session.
type renderCache struct {
	mu      sync.RWMutex
	entries map[string][]string
	limit   int
}

func newRenderCache(limit int) *renderCache {
	return &renderCache{entries: make(map[string][]string, 64), limit: limit}
}

func (c *renderCache) get(key string, render func() []string) []string {
	c.mu.RLock()
	lines, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		lines = render()
		c.mu.Lock()
		if len(c.entries) >= c.limit {
			c.entries = make(map[string][]string, 64)
		}
		c.entries[key] = lines
		c.mu.Unlock()
	}
	// Callers receive a []string they may retain or mutate; never hand out the
	// cached backing array.
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

func (c *renderCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *renderCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string][]string, 64)
}

var (
	markdownCache = newRenderCache(markdownCacheLimit)
	snippetCache  = newRenderCache(snippetCacheLimit)
)

// cachedCommentMarkdown memoises renderCommentMarkdown, which costs a full
// goldmark parse and ran once per visible thread and comment on every frame.
func cachedCommentMarkdown(body string) []string {
	return markdownCache.get(body, func() []string { return renderCommentMarkdown(body) })
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

// formatDiffHunk marks the line the comment is anchored to and clips the hunk
// to a readable window. The hunk's own line numbering comes from internal/diff,
// which the summary renderer reads the same way.
func formatDiffHunk(hunk string, targetNew *int, targetOld *int) []string {
	clean := strings.Trim(strings.ReplaceAll(hunk, "\r", ""), "\n")
	if clean == "" {
		return nil
	}
	parsed := diff.ParseHunk(clean)
	entries := make([]diffEntry, 0, len(parsed))
	for _, line := range parsed {
		var highlight bool
		switch line.Kind {
		case diff.Header:
		case diff.Added:
			highlight = line.At(diff.Added, targetNew)
		case diff.Removed:
			highlight = line.At(diff.Removed, targetOld)
		default:
			highlight = line.At(diff.Added, targetNew) || line.At(diff.Removed, targetOld)
		}
		text := line.Text
		if highlight {
			text = ">> " + text
		}
		entries = append(entries, diffEntry{line: text, highlight: highlight})
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
	end := min(len(entries), 15)
	if len(highlightIdx) > 0 {
		start = max(0, highlightIdx[0]-7)
		end = min(len(entries), highlightIdx[len(highlightIdx)-1]+8)
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

func sectionHeights(total int, listLines int) (int, int) {
	if total <= 2 {
		return 1, max(1, total-2)
	}
	content := total - 2
	divider := 1
	if content <= divider {
		return 1, max(1, content-divider)
	}
	content -= divider
	if content < 2 {
		return 1, 1
	}
	list := max(3, content*3/10)
	if list > content-3 {
		list = max(1, content-3)
	}
	maxList := max(1, listLines)
	list = min(list, maxList)
	if list >= content {
		list = max(1, content-1)
	}
	detail := content - list
	return list, detail
}
