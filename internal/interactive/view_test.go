package interactive

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

func TestRenderDetailCollapsedKeepsFirstCommentVisible(t *testing.T) {
	thread := threads.ReviewThread{
		ThreadID: "t1",
		Path:     "file.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: "first"},
			{ID: "c2", Author: "bob", Body: "second"},
			{ID: "c3", Author: "carol", Body: "third"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		detailMode:      detailSnippet,
	}
	out := renderDetailContent(model, 5, StateView, textarea.New(), 80)
	if !strings.Contains(out, "first") {
		t.Fatalf("expected the first comment to be shown: %s", out)
	}
}

func TestRenderDetailCollapsedShowsSelectedComment(t *testing.T) {
	thread := threads.ReviewThread{
		ThreadID: "t1",
		Path:     "file.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: "first"},
			{ID: "c2", Author: "bob", Body: "second"},
			{ID: "c3", Author: "carol", Body: "third"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		selectedComment: 2,
		detailExpanded:  false,
		detailMode:      detailSnippet,
	}
	out := stripANSI(renderDetailContent(model, 5, StateView, textarea.New(), 80))
	if !strings.Contains(out, "third") {
		t.Fatalf("expected the selected comment to be shown, got %s", out)
	}
}

func TestHighlightStylesDifferFromTopBar(t *testing.T) {
	if threadHighlightStyle.GetBackground() == topBarStyle.GetBackground() {
		t.Fatalf("expected thread highlight background to differ from top bar, both are %v", threadHighlightStyle.GetBackground())
	}
	if commentHighlightStyle.GetBackground() == topBarStyle.GetBackground() {
		t.Fatalf("expected comment highlight background to differ from top bar, both are %v", commentHighlightStyle.GetBackground())
	}
	if threadHighlightStyle.GetBackground() == commentHighlightStyle.GetBackground() {
		t.Fatalf("expected comment highlight to differ from thread highlight")
	}
}

func TestRenderDetailExpandedRespectsHeightWindow(t *testing.T) {
	comments := make([]threads.ThreadComment, 0, 6)
	for i := 0; i < 6; i++ {
		comments = append(comments, threads.ThreadComment{ID: fmt.Sprintf("c%d", i), Author: fmt.Sprintf("u%d", i), Body: fmt.Sprintf("body-%d", i)})
	}
	thread := threads.ReviewThread{
		ThreadID: "t2",
		Path:     "file.go",
		Comments: comments,
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}
	out := renderDetailContent(model, 6, StateView, textarea.New(), 80)
	shown := strings.Count(out, "body-")
	if shown > 2 {
		t.Fatalf("expected at most 2 comments in viewport, got %d", shown)
	}
}

func TestRenderDetailKeepsSelectionInBounds(t *testing.T) {
	comments := []threads.ThreadComment{
		{ID: "c1", Body: "first"},
		{ID: "c2", Body: "second"},
		{ID: "c3", Body: "third"},
	}
	thread := threads.ReviewThread{
		ThreadID: "t-window",
		Path:     "file.go",
		Comments: comments,
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		selectedComment: 2,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}
	out := renderDetailContent(model, 8, StateView, textarea.New(), 80)
	if !strings.Contains(out, "third") {
		t.Fatalf("expected selected final comment to be rendered, got %s", out)
	}
}

func TestRenderDetailHighlightsSelectionMarker(t *testing.T) {
	thread := threads.ReviewThread{
		ThreadID: "t-marker",
		Path:     "file.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: "first"},
			{ID: "c2", Author: "bob", Body: "second"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		selectedComment: 1,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}
	out := stripANSI(renderDetailContent(model, 10, StateView, textarea.New(), 80))
	if !strings.Contains(out, " > bob at") {
		t.Fatalf("expected selection marker before selected comment, got %q", out)
	}
}

func TestRenderDetailShowsSnippetOnlyForFirstComment(t *testing.T) {
	firstSnippet := &threads.HistoricalSnippet{
		Path:          "main.go",
		StartLine:     10,
		HighlightLine: 11,
		Lines:         []string{"first snippet line"},
	}
	secondSnippet := &threads.HistoricalSnippet{
		Path:          "main.go",
		StartLine:     20,
		HighlightLine: 21,
		Lines:         []string{"second snippet line"},
	}
	thread := threads.ReviewThread{
		ThreadID: "snippet",
		Path:     "foo.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: "first", Snippet: firstSnippet},
			{ID: "c2", Author: "bob", Body: "second", Snippet: secondSnippet},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}
	out := stripANSI(renderDetailContent(model, 20, StateView, textarea.New(), 80))
	if !strings.Contains(out, "first snippet line") {
		t.Fatalf("expected first snippet to be rendered: %s", out)
	}
	if strings.Contains(out, "second snippet line") {
		t.Fatalf("second snippet should not be rendered: %s", out)
	}
}

func TestRenderDetailShowsSnippetEvenInDiffMode(t *testing.T) {
	snippet := &threads.HistoricalSnippet{
		Path:          "foo.go",
		StartLine:     30,
		HighlightLine: 31,
		Lines:         []string{"snippet body"},
	}
	thread := threads.ReviewThread{
		ThreadID: "snippet-diff",
		Path:     "foo.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: "first", Snippet: snippet, DiffHunk: "@@ -1,1 +1,1 @@"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		detailExpanded:  true,
		detailMode:      detailDiff,
	}
	out := stripANSI(renderDetailContent(model, 10, StateView, textarea.New(), 80))
	if !strings.Contains(out, "snippet body") {
		t.Fatalf("expected snippet to remain visible in diff mode, got %s", out)
	}
}

func TestRenderReplyTargetShowsSelectedComment(t *testing.T) {
	thread := threads.ReviewThread{
		ThreadID: "t3",
		Path:     "foo/bar.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: "first body"},
			{ID: "c2", Author: "bob", Body: "second body\nwith more info", URL: "https://example.com"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		selectedComment: 1,
	}
	out := renderReplyTarget(model)
	if !strings.Contains(out, "bob") {
		t.Fatalf("expected author in reply target, got: %s", out)
	}
	if !strings.Contains(out, "(https://example.com)") {
		t.Fatalf("expected link rendering, got: %s", out)
	}
}

func TestRenderBottomBarFillsWidth(t *testing.T) {
	model := Model{
		filters: Filters{
			Author: "alice",
			Status: threads.StatusResolved,
			Text:   "panic",
		},
		infoMessage: "syncing data",
	}
	width := 60
	out := renderBottomBar(model, width)
	if got := lipgloss.Width(stripANSI(out)); got != width {
		t.Fatalf("expected width %d, got %d in %q", width, got, out)
	}
}

func TestRenderBottomBarKeepsHelpTextVisible(t *testing.T) {
	model := Model{
		filters: Filters{
			Author: strings.Repeat("long-author", 5),
			Status: threads.StatusUnresolved,
			Text:   strings.Repeat("filter", 4),
		},
		errMessage: strings.Repeat("failure ", 3),
	}
	width := 50
	out := renderBottomBar(model, width)
	plain := strings.TrimRight(stripANSI(out), " ")
	if !strings.HasSuffix(plain, "Press ? for help") {
		t.Fatalf("expected help text to be visible, got %q", plain)
	}
}

func TestRenderBottomBarShowsHelpTextWhenLeftCollapsed(t *testing.T) {
	model := Model{
		filters: Filters{
			Author: strings.Repeat("user", 5),
			Status: threads.StatusResolved,
			Text:   strings.Repeat("needle", 5),
		},
	}
	right := "Press ? for help"
	width := lipgloss.Width(right) + 1
	out := renderBottomBar(model, width)
	plain := stripANSI(out)
	if strings.Contains(plain, "filters:") {
		t.Fatalf("expected filters section to be omitted when width exhausted, got %q", plain)
	}
	if !strings.HasSuffix(strings.TrimRight(plain, " "), right) {
		t.Fatalf("expected help text to remain even when left collapsed, got %q", plain)
	}
	if got := lipgloss.Width(plain); got != width {
		t.Fatalf("expected width %d, got %d in %q", width, got, plain)
	}
}

func TestRenderBottomBarSurvivesAnsiTruncate(t *testing.T) {
	model := Model{
		filters: Filters{
			Status: threads.StatusAll,
		},
	}
	width := 80
	out := renderBottomBar(model, width)
	truncated := ansi.Truncate(out, width, "")
	if !strings.Contains(stripANSI(truncated), "Press ? for help") {
		t.Fatalf("truncated output lost help text: %q", truncated)
	}
	if got := lipgloss.Width(stripANSI(truncated)); got != width {
		t.Fatalf("expected truncated width %d, got %d", width, got)
	}
}

func TestRenderAuthorSuggestionsHighlightsSelection(t *testing.T) {
	names := []string{"alice", "bob", "carol"}
	out := renderAuthorSuggestions(names, 1)
	if strings.Count(out, ">") != 1 {
		t.Fatalf("expected exactly one highlighted suggestion, got %q", out)
	}
	if !strings.Contains(out, "> bob") {
		t.Fatalf("expected bob to be highlighted, got %q", out)
	}
	if strings.Contains(out, "> alice") || strings.Contains(out, "> carol") {
		t.Fatalf("unexpected highlights in %q", out)
	}
}

func TestRenderStatusSuggestionsHighlightsSelection(t *testing.T) {
	options := []statusOption{
		{label: "all"},
		{label: "resolved"},
		{label: "unresolved"},
	}
	out := renderStatusSuggestions(options, 2)
	if !strings.Contains(out, "> unresolved") {
		t.Fatalf("expected unresolved to be highlighted, got %q", out)
	}
	if strings.Count(out, ">") != 1 {
		t.Fatalf("expected only one highlight, got %q", out)
	}
}

func TestFormatDiffHunkHighlightsAndTrims(t *testing.T) {
	var b strings.Builder
	b.WriteString("@@ -1,30 +1,30 @@\n")
	for i := 1; i <= 30; i++ {
		b.WriteString(fmt.Sprintf(" line-%02d\n", i))
	}
	target := 12
	lines := formatDiffHunk(b.String(), &target, nil)
	if len(lines) > 15 {
		t.Fatalf("expected at most 15 lines, got %d", len(lines))
	}
	found := false
	for _, line := range lines {
		if strings.Contains(line, "line-12") && strings.HasPrefix(line, ">>") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected highlighted target line in %v", lines)
	}
}

func TestFormatDiffHunkLimitsWhenNoHighlight(t *testing.T) {
	var b strings.Builder
	b.WriteString("@@ -1,40 +1,40 @@\n")
	for i := 1; i <= 40; i++ {
		b.WriteString(fmt.Sprintf(" line-%02d\n", i))
	}
	lines := formatDiffHunk(b.String(), nil, nil)
	if len(lines) != 15 {
		t.Fatalf("expected 15 lines when trimming without highlight, got %d", len(lines))
	}
}

func TestSectionHeightsCapsListAtThirtyPercent(t *testing.T) {
	totalHeight := 60
	listHeight, detailHeight := sectionHeights(totalHeight, 50)
	contentHeight := totalHeight - 3
	expectedList := vmax(3, contentHeight*3/10)
	if listHeight != expectedList {
		t.Fatalf("expected list height %d with many threads, got %d", expectedList, listHeight)
	}
	if detailHeight != contentHeight-listHeight {
		t.Fatalf("expected detail height %d, got %d", contentHeight-listHeight, detailHeight)
	}
}

func TestSectionHeightsShrinksWhenFewThreads(t *testing.T) {
	totalHeight := 60
	listHeight, detailHeight := sectionHeights(totalHeight, 2)
	contentHeight := totalHeight - 3
	if listHeight != 2 {
		t.Fatalf("expected list height to shrink to 2 threads, got %d", listHeight)
	}
	if detailHeight != contentHeight-listHeight {
		t.Fatalf("expected detail height %d, got %d", contentHeight-listHeight, detailHeight)
	}
}

func TestRenderThreadListGroupsByPath(t *testing.T) {
	threads := []threads.ReviewThread{
		{
			ThreadID:   "t1",
			Path:       "path/a.go",
			Line:       intPtr(3),
			IsResolved: false,
			Comments:   []threads.ThreadComment{{ID: "c1", Author: "alice", Body: "first"}},
		},
		{
			ThreadID:   "t2",
			Path:       "path/a.go",
			Line:       intPtr(5),
			IsResolved: true,
			Comments:   []threads.ThreadComment{{ID: "c2", Author: "bob", Body: "second"}},
		},
		{
			ThreadID:   "t3",
			Path:       "path/b.go",
			Line:       intPtr(9),
			IsResolved: false,
			Comments:   []threads.ThreadComment{{ID: "c3", Author: "carol", Body: "third"}},
		},
	}
	model := Model{
		threads:         threads,
		filteredIndexes: []int{0, 1, 2},
		selectedThread:  1,
	}
	out := stripANSI(renderThreadList(model, 10, 120))
	if strings.Count(out, " path/a.go [1 resolved, 1 unresolved]") != 1 {
		t.Fatalf("expected single header for path/a.go, got %q", out)
	}
	if !strings.Contains(out, " ├─ ⬜ [L3] - alice: first") {
		t.Fatalf("expected unresolved entry with line numbers, got %q", out)
	}
	if !strings.Contains(out, " ╰─ ✅ [L5] - bob: second") {
		t.Fatalf("expected resolved entry with closing branch, got %q", out)
	}
	if !strings.Contains(out, " path/b.go [0 resolved, 1 unresolved]") {
		t.Fatalf("expected second path header, got %q", out)
	}
}

func TestRenderThreadListShowsSuggestionPreview(t *testing.T) {
	body := "Please change\n```suggestion\nnew content\n```"
	threads := []threads.ReviewThread{
		{
			ThreadID:   "t-suggestion",
			Path:       "path/a.go",
			Line:       intPtr(10),
			IsResolved: false,
			Comments:   []threads.ThreadComment{{ID: "c1", Author: "alice", Body: body}},
		},
	}
	model := Model{
		threads:         threads,
		filteredIndexes: []int{0},
		selectedThread:  0,
	}
	out := stripANSI(renderThreadList(model, 5, 120))
	if strings.Contains(out, "suggestion") {
		t.Fatalf("expected non-suggestion text to be shown when present, got %q", out)
	}
	if !strings.Contains(out, "Please change") {
		t.Fatalf("expected original text to be kept when suggestion is not the only content, got %q", out)
	}
}

func TestDisplayAuthorReplacesAINameInDetail(t *testing.T) {
	thread := threads.ReviewThread{
		ThreadID: "ai-detail",
		Path:     "foo.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "GitHub Copilot", Body: "hi"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}
	out := stripANSI(renderDetailContent(model, 5, StateView, textarea.New(), 80))
	if !strings.Contains(out, "🤖 co-pilot at") {
		t.Fatalf("expected AI author label, got %q", out)
	}
	if strings.Contains(out, "GitHub Copilot") {
		t.Fatalf("expected original AI name to be replaced, got %q", out)
	}
}

func TestDisplayAuthorReplacesAINameInList(t *testing.T) {
	thread := threads.ReviewThread{
		ThreadID:   "ai-list",
		Path:       "foo.go",
		Line:       intPtr(12),
		IsResolved: false,
		Comments:   []threads.ThreadComment{{ID: "c1", Author: "Codex", Body: "hi"}},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
	}
	out := stripANSI(renderThreadList(model, 5, 80))
	if !strings.Contains(out, "🤖 codex") {
		t.Fatalf("expected AI label in list, got %q", out)
	}
	if strings.Contains(out, "Codex") {
		t.Fatalf("expected original AI name to be replaced in list, got %q", out)
	}
}

func TestSnippetLanguageMapping(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"component.tsx", "tsx"},
		{"unknown", ""},
	}
	for _, tc := range cases {
		if got := snippetLanguage(tc.path); got != tc.want {
			t.Fatalf("snippetLanguage(%q)=%q want %q", tc.path, got, tc.want)
		}
	}
}

func TestSnippetHighlightAddsColor(t *testing.T) {
	if _, err := loadSnippetRenderer(); err != nil {
		t.Fatalf("failed to init glamour renderer: %v", err)
	}
	snippet := &threads.HistoricalSnippet{
		Path:          "main.go",
		StartLine:     1,
		HighlightLine: 2,
		Lines: []string{
			"package main",
			"func main() {",
			"    fmt.Println(\"hi\")",
			"}",
		},
	}
	lines, ok := highlightSnippet(snippet)
	if !ok {
		t.Fatalf("expected glamour highlighter to succeed")
	}
	if len(lines) != len(snippet.Lines) {
		t.Fatalf("expected %d lines, got %d", len(snippet.Lines), len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "\x1b[") {
		t.Fatalf("expected ANSI escape sequences in %q", joined)
	}
}

func TestCleanupHighlightedLinesPreservesLeadingBlanks(t *testing.T) {
	original := []string{"", "", "line"}
	rendered := "\n\x1b[0m\n\x1b[38;5;39mline\x1b[0m"
	lines := cleanupHighlightedLines(rendered, original)
	if lines == nil {
		t.Fatalf("expected lines to be returned")
	}
	if len(lines) != len(original) {
		t.Fatalf("expected %d lines, got %d", len(original), len(lines))
	}
	if strings.TrimSpace(ansi.Strip(lines[0])) != "" || strings.TrimSpace(ansi.Strip(lines[1])) != "" {
		t.Fatalf("expected first lines to remain blank: %q", lines)
	}
}

func TestRenderCommentMarkdownReturnsLines(t *testing.T) {
	lines := renderCommentMarkdown("hello\nworld")
	if len(lines) < 2 {
		t.Fatalf("expected at least two lines, got %v", lines)
	}
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

func intPtr(v int) *int {
	return &v
}

func TestRenderDetailBlockKeepsReplyEditorVisible(t *testing.T) {
	longBody := strings.Repeat("padding line\n", 24)
	thread := threads.ReviewThread{
		ThreadID: "t1",
		Path:     "file.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: longBody},
			{ID: "c2", Author: "bob", Body: "second body", URL: "https://example.com"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		selectedComment: 1,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}
	reply := textarea.New()
	reply.SetWidth(60)
	reply.SetValue("my draft reply")

	out := stripANSI(renderDetailBlock(model, 20, 80, StateReply, reply, textinput.New(), "reply", false, 0, false, -1, -1))

	if !strings.Contains(out, "Replying to") {
		t.Fatalf("expected reply target header to stay on screen, got:\n%s", out)
	}
	if !strings.Contains(out, "my draft reply") {
		t.Fatalf("expected reply editor content to stay on screen, got:\n%s", out)
	}
	if got := len(strings.Split(out, "\n")); got != 20 {
		t.Fatalf("expected detail block to fill exactly 20 lines, got %d", got)
	}
}

func TestRenderDetailBlockKeepsSelectionVisibleWhenScrolled(t *testing.T) {
	longBody := strings.Repeat("padding line\n", 24)
	thread := threads.ReviewThread{
		ThreadID: "t1",
		Path:     "file.go",
		Comments: []threads.ThreadComment{
			{ID: "c1", Author: "alice", Body: longBody},
			{ID: "c2", Author: "bob", Body: "second body"},
		},
	}
	model := Model{
		threads:         []threads.ReviewThread{thread},
		filteredIndexes: []int{0},
		selectedThread:  0,
		selectedComment: 1,
		detailExpanded:  true,
		detailMode:      detailSnippet,
	}

	out := stripANSI(renderDetailBlock(model, 20, 80, StateView, textarea.New(), textinput.New(), "", false, 0, false, -1, -1))

	if !strings.Contains(out, "> bob at") {
		t.Fatalf("expected selected comment header to stay on screen, got:\n%s", out)
	}
	if !strings.Contains(out, "second body") {
		t.Fatalf("expected selected comment body to stay on screen, got:\n%s", out)
	}
}

func listModel(n int) Model {
	list := make([]threads.ReviewThread, 0, n)
	indexes := make([]int, 0, n)
	for i := 0; i < n; i++ {
		list = append(list, threads.ReviewThread{
			ThreadID: fmt.Sprintf("t%d", i),
			Path:     fmt.Sprintf("pkg/file%d.go", i/2),
			Comments: []threads.ThreadComment{{ID: fmt.Sprintf("c%d", i), Author: "alice", Body: fmt.Sprintf("body-%d", i)}},
		})
		indexes = append(indexes, i)
	}
	return Model{threads: list, filteredIndexes: indexes}
}

func TestRenderThreadListShowsSelectionAtEveryHeight(t *testing.T) {
	model := listModel(6)
	for _, height := range []int{1, 2, 3, 5} {
		for selected := 0; selected < 6; selected++ {
			model.selectedThread = selected
			model.listOffset = 0
			out := stripANSI(renderThreadList(model, height, 100))
			if got := len(strings.Split(out, "\n")); got != height {
				t.Fatalf("height=%d selected=%d: expected %d lines, got %d", height, selected, height, got)
			}
			want := fmt.Sprintf("body-%d", selected)
			if !strings.Contains(out, want) {
				t.Fatalf("height=%d selected=%d: expected %q on screen, got:\n%s", height, selected, want, out)
			}
		}
	}
}

func TestRenderThreadListIgnoresStaleOffsetPastSelection(t *testing.T) {
	model := listModel(6)
	model.selectedThread = 0
	model.listOffset = 5

	out := stripANSI(renderThreadList(model, 4, 100))

	if !strings.Contains(out, "body-0") {
		t.Fatalf("expected the selection to win over a stale offset, got:\n%s", out)
	}
}

func TestThreadListStartStaysWithinBounds(t *testing.T) {
	model := listModel(6)
	for _, height := range []int{1, 2, 3, 5, 20} {
		for selected := 0; selected < 6; selected++ {
			for _, offset := range []int{0, 3, 5} {
				got := threadListStart(model.threads, offset, selected, height)
				if got > selected {
					t.Fatalf("h=%d sel=%d off=%d: start %d scrolled past the selection", height, selected, offset, got)
				}
				if got < 0 || got >= len(model.threads) {
					t.Fatalf("h=%d sel=%d off=%d: start %d out of range", height, selected, offset, got)
				}
				if height <= 2 && got != selected {
					t.Fatalf("h=%d sel=%d: no room for context, expected start=%d got %d", height, selected, selected, got)
				}
			}
		}
	}
}

func BenchmarkRenderThreadList(b *testing.B) {
	model := listModel(400)
	model.selectedThread = 399
	model.listOffset = 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderThreadList(model, 12, 120)
	}
}

func detailModel(comments int) Model {
	list := make([]threads.ThreadComment, 0, comments)
	for i := 0; i < comments; i++ {
		list = append(list, threads.ThreadComment{
			ID: fmt.Sprintf("c%d", i), Author: "alice", Body: fmt.Sprintf("body-%d", i),
		})
	}
	return Model{
		threads:         []threads.ReviewThread{{ThreadID: "t1", Path: "file.go", Comments: list}},
		filteredIndexes: []int{0},
		selectedThread:  0,
		detailMode:      detailSnippet,
	}
}

func TestRenderDetailCollapsedNeverShowsMoreThanExpanded(t *testing.T) {
	model := detailModel(8)
	for height := 1; height <= 60; height++ {
		model.detailExpanded = true
		expanded := strings.Count(stripANSI(renderDetailContent(model, height, StateView, textarea.New(), 80)), "body-")
		model.detailExpanded = false
		collapsed := strings.Count(stripANSI(renderDetailContent(model, height, StateView, textarea.New(), 80)), "body-")

		if collapsed > expanded {
			t.Fatalf("height=%d: collapsed showed %d comments, expanded only %d", height, collapsed, expanded)
		}
		if collapsed < 1 {
			t.Fatalf("height=%d: collapsed must still show the selected comment", height)
		}
	}
}
