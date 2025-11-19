package interactive

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

func TestRenderDetailCollapsedShowsFirstCommentOnly(t *testing.T) {
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
	out := renderDetailContent(model, 5)
	if !strings.Contains(out, "first") {
		t.Fatalf("expected first comment to be shown: %s", out)
	}
	if strings.Contains(out, "second") || strings.Contains(out, "third") {
		t.Fatalf("collapsed view should not show later comments: %s", out)
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
	out := renderDetailContent(model, 6)
	shown := strings.Count(out, "body-")
	if shown > 2 {
		t.Fatalf("expected at most 2 comments in viewport, got %d", shown)
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
	out := stripANSI(renderDetailContent(model, 20))
	if !strings.Contains(out, "first snippet line") {
		t.Fatalf("expected first snippet to be rendered: %s", out)
	}
	if strings.Contains(out, "second snippet line") {
		t.Fatalf("second snippet should not be rendered: %s", out)
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
	if !strings.Contains(out, "second body") {
		t.Fatalf("expected preview of body, got: %s", out)
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
