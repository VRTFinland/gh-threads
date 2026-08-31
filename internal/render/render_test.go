package render

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

func TestPrintSummary_SkipsDuplicateCommentWhenSnippetShown(t *testing.T) {
	commentBody := "Needs change"
	line := 12
	makePayload := func() threads.Payload {
		return threads.BuildPayload(
			threads.Context{Owner: "o", Repo: "r", PullRequest: 1},
			nil,
			[]threads.ReviewThread{
				{
					Path:       "file.go",
					Line:       &line,
					IsResolved: false,
					Comments: []threads.ThreadComment{
						{
							Author:    "alice",
							Body:      commentBody,
							CreatedAt: "2024-01-02T15:04:05Z",
							Path:      "file.go",
							Line:      &line,
							Snippet: &threads.HistoricalSnippet{
								Commit:        "abcdef123",
								Path:          "file.go",
								StartLine:     10,
								HighlightLine: line,
								Lines: []string{
									"first",
									"second",
									"target",
								},
							},
						},
					},
				},
			},
		)
	}

	testCases := []struct {
		name     string
		showDiff bool
	}{
		{name: "with show diff", showDiff: true},
		{name: "without show diff", showDiff: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintSummary(&buf, makePayload(), Options{
				Colour:   false,
				ShowDiff: tc.showDiff,
				Markdown: false,
				Width:    80,
			})

			output := buf.String()
			if count := strings.Count(output, commentBody); count != 1 {
				t.Fatalf("expected comment body once, got %d occurrences; output:\n%s", count, output)
			}
		})
	}
}

func TestPrintSummary_RendersMarkdownInSnippetBlock(t *testing.T) {
	commentBody := "**bold** text"
	width := 64
	markdownLines := renderCommentSnippet(commentBody, true, width, nil, threads.LineAnchor{}, false)
	plainLines := renderCommentSnippet(commentBody, false, width, nil, threads.LineAnchor{}, false)
	if reflect.DeepEqual(markdownLines, plainLines) {
		t.Fatalf("expected markdown rendering to differ from plain snippet lines; got %v", markdownLines)
	}

	expected := renderCommentBody(commentBody, true, width+12)
	if !reflect.DeepEqual(markdownLines, expected) {
		t.Fatalf("expected markdown snippet rendering to match comment renderer; got %v want %v", markdownLines, expected)
	}
}

func TestRenderCommentSnippet_ReplacesSuggestionWithDiff(t *testing.T) {
	body := "```suggestion\nnew content\n```"
	anchor := threads.LineAnchor{Start: 11, End: 11, Space: threads.SnippetSpace}
	snippet := &threads.HistoricalSnippet{
		Commit:        "abcdef123",
		Path:          "file.go",
		StartLine:     10,
		HighlightLine: 11,
		Lines: []string{
			"first",
			"old content",
			"last",
		},
	}

	lines := renderCommentSnippet(body, false, 80, snippet, anchor, false)
	expected := []string{
		"- old content",
		"+ new content",
	}
	if !reflect.DeepEqual(lines, expected) {
		t.Fatalf("expected suggestion diff %v, got %v", expected, lines)
	}
}

func TestReplaceSuggestionBlocks_FallsBackWithoutSnippet(t *testing.T) {
	body := "Please change\n```suggestion\nnew content\n```"
	anchor := threads.LineAnchor{Start: 5, End: 5, Space: threads.SnippetSpace}
	result := replaceSuggestionBlocks(body, nil, anchor, false, true)
	if !strings.Contains(result, "```suggestion") {
		t.Fatalf("expected suggestion block to remain when snippet missing; got %q", result)
	}
}

func TestDumpJSON_ColourisedAndPlain(t *testing.T) {
	payload := threads.Payload{
		Repository:  "o/r",
		PullRequest: 1,
		ReviewThreads: []threads.ReviewThread{
			{ThreadID: "t", IsResolved: true},
		},
	}

	coloured, err := DumpJSON(payload, true)
	if err != nil {
		t.Fatalf("DumpJSON returned error: %v", err)
	}
	if !strings.Contains(coloured, "\x1b[") {
		t.Fatalf("expected coloured JSON output, got %q", coloured)
	}

	plain, err := DumpJSON(payload, false)
	if err != nil {
		t.Fatalf("DumpJSON returned error: %v", err)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("expected plain JSON output without colour codes, got %q", plain)
	}
}

// TestPrintSummary_RendersBodyWhenSnippetMissing locks in the fallback that the
// non-fatal snippet fetching in threads.attachHistoricalSnippets relies on: when
// a snippet could not be loaded, the comment itself must still be printed.
func TestPrintSummary_RendersBodyWhenSnippetMissing(t *testing.T) {
	line := 12
	payload := threads.BuildPayload(
		threads.Context{Owner: "o", Repo: "r", PullRequest: 1},
		nil,
		[]threads.ReviewThread{
			{
				Path: "file.go",
				Line: &line,
				Comments: []threads.ThreadComment{
					{
						Author:    "alice",
						Body:      "Needs change",
						CreatedAt: "2024-01-02T15:04:05Z",
						Path:      "file.go",
						Line:      &line,
						Snippet:   nil,
					},
				},
			},
		},
	)

	for _, showDiff := range []bool{true, false} {
		var buf bytes.Buffer
		PrintSummary(&buf, payload, Options{Width: 80, ShowDiff: showDiff})
		if !strings.Contains(buf.String(), "Needs change") {
			t.Fatalf("showDiff=%v: expected the body to be printed without a snippet, got:\n%s", showDiff, buf.String())
		}
	}
}

func snippetPayload(body string, snippet *threads.HistoricalSnippet) threads.Payload {
	line := 12
	return threads.BuildPayload(
		threads.Context{Owner: "o", Repo: "r", PullRequest: 1},
		nil,
		[]threads.ReviewThread{{
			Path: "file.go",
			Line: &line,
			Comments: []threads.ThreadComment{{
				Author:    "alice",
				Body:      body,
				CreatedAt: "2024-01-02T15:04:05Z",
				Path:      "file.go",
				Line:      &line,
				Snippet:   snippet,
			}},
		}},
	)
}

func TestCompactSnippetLines_TrimsOnlyEdges(t *testing.T) {
	got := compactSnippetLines([]string{"", "  ", "a", "", "b", "", ""})
	want := []string{"a", "", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected only edge padding to be trimmed, got %q", got)
	}
}

func TestCompactSnippetLines_AllBlankReturnsNil(t *testing.T) {
	if got := compactSnippetLines([]string{"", "   ", "\x1b[0m"}); got != nil {
		t.Fatalf("expected nil for an all-blank render, got %q", got)
	}
}

func TestCompactSnippetLines_StripsAnsiOnlyPadding(t *testing.T) {
	got := compactSnippetLines([]string{"\x1b[0m", "text", "\x1b[38;5;252m  \x1b[0m"})
	want := []string{"text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected ANSI-only padding to count as blank, got %q", got)
	}
}

func TestCompactSnippetLines_PreservesBlankInsideFencedCode(t *testing.T) {
	body := "```go\nfunc a() {\n\n\tb()\n}\n```"
	rendered := renderCommentSnippet(body, false, 60, nil, threads.LineAnchor{}, false)
	got := compactSnippetLines(rendered)

	blanks := 0
	for i, line := range got {
		if strings.TrimSpace(line) == "" && i > 0 && i < len(got)-1 {
			blanks++
		}
	}
	if blanks == 0 {
		t.Fatalf("expected the blank line inside the fenced block to survive, got %q", got)
	}
}

func TestPrintSummary_BoxKeepsParagraphBreaks(t *testing.T) {
	snippet := &threads.HistoricalSnippet{
		Commit: "abcdef123", Path: "file.go", StartLine: 10, HighlightLine: 12,
		Lines: []string{"first", "second", "target"},
	}
	var buf bytes.Buffer
	PrintSummary(&buf, snippetPayload("First para.\n\nSecond para.", snippet), Options{Width: 100})

	if !strings.Contains(buf.String(), "First para.") || !strings.Contains(buf.String(), "Second para.") {
		t.Fatalf("expected both paragraphs, got:\n%s", buf.String())
	}
	blankRow := false
	for _, line := range strings.Split(buf.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "│") && strings.TrimSpace(strings.Trim(trimmed, "│")) == "" {
			blankRow = true
		}
	}
	if !blankRow {
		t.Fatalf("expected the paragraph break to survive as a blank box row, got:\n%s", buf.String())
	}
}

func TestPrintSummary_RendersBodyWhenHighlightOutsideSnippet(t *testing.T) {
	snippet := &threads.HistoricalSnippet{
		Commit: "abcdef123", Path: "file.go", StartLine: 10, HighlightLine: 99,
		Lines: []string{"first", "second", "target"},
	}
	var buf bytes.Buffer
	PrintSummary(&buf, snippetPayload("Needs change", snippet), Options{Width: 100})

	if got := strings.Count(buf.String(), "Needs change"); got != 1 {
		t.Fatalf("expected the body exactly once when the highlight is out of range, got %d:\n%s", got, buf.String())
	}
}

func TestPrintSummary_RendersBodyWhenHighlightLineZero(t *testing.T) {
	snippet := &threads.HistoricalSnippet{
		Commit: "abcdef123", Path: "file.go", StartLine: 10, HighlightLine: 0,
		Lines: []string{"first", "second", "target"},
	}
	var buf bytes.Buffer
	PrintSummary(&buf, snippetPayload("Needs change", snippet), Options{Width: 100})

	if got := strings.Count(buf.String(), "Needs change"); got != 1 {
		t.Fatalf("expected the body exactly once for an unset highlight line, got %d:\n%s", got, buf.String())
	}
}

func TestPrintCommentBlock_ReportsWhetherItPrinted(t *testing.T) {
	var buf bytes.Buffer
	if printCommentBlock(&buf, "   ", 100, false, false, nil, threads.LineAnchor{}) {
		t.Fatal("expected an all-blank body to report nothing printed")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for an all-blank body, got %q", buf.String())
	}
	if !printCommentBlock(&buf, "real text", 100, false, false, nil, threads.LineAnchor{}) {
		t.Fatal("expected a real body to report it printed")
	}
}

// TestRenderCommentSnippet_MultiLineSuggestionUsesOriginalRange proves the
// coordinate fix end to end: the removed lines must come from the anchor
// expressed in the snippet's own (original commit) space.
func TestRenderCommentSnippet_MultiLineSuggestionUsesOriginalRange(t *testing.T) {
	body := "```suggestion\nreplacement\n```"
	comment := threads.ThreadComment{
		Line: ptr(17), StartLine: ptr(16),
		OriginalLine: ptr(12), OriginalStartLine: ptr(11),
	}
	snippet := &threads.HistoricalSnippet{
		Commit: "abcdef123", Path: "file.go", StartLine: 10, HighlightLine: 12,
		Lines: []string{"first", "second", "third", "fourth"}, // lines 10..13
	}

	lines := renderCommentSnippet(body, false, 80, snippet, comment.Anchor(threads.SnippetSpace), false)

	expected := []string{"- second", "- third", "+ replacement"}
	if !reflect.DeepEqual(lines, expected) {
		t.Fatalf("expected the original-space range 11-12 to be removed, got %v", lines)
	}
}

// boxRowWidths returns the visible width of every "│ … │" row in the output.
func boxRowWidths(out string) []int {
	var widths []int
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "│") && strings.HasSuffix(trimmed, "│") {
			widths = append(widths, lipgloss.Width(trimmed))
		}
	}
	return widths
}

func TestPrintCommentBlock_BorderAlignsWithWideRunes(t *testing.T) {
	var buf bytes.Buffer
	printCommentBlock(&buf, "plain line\n\n🤖 emoji line and 日本語 text\n\nlast", 100, false, false, nil, threads.LineAnchor{})

	widths := boxRowWidths(buf.String())
	if len(widths) < 3 {
		t.Fatalf("expected several box rows, got %d:\n%s", len(widths), buf.String())
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Fatalf("row %d is %d columns wide, first row is %d:\n%s", i, w, widths[0], buf.String())
		}
	}
}

func TestPrintCommentBlock_BorderAlignsAtNarrowWidth(t *testing.T) {
	for _, width := range []int{40, 52, 60, 92, 200} {
		var buf bytes.Buffer
		printCommentBlock(&buf, "A reasonably long comment body that will need to be wrapped by the renderer at some point.", width, false, true, nil, threads.LineAnchor{})

		widths := boxRowWidths(buf.String())
		for i, w := range widths {
			if w != widths[0] {
				t.Fatalf("width=%d row %d is %d columns, first row is %d:\n%s", width, i, w, widths[0], buf.String())
			}
		}
	}
}

func TestWrapCommentSnippetLines_PreservesIndentation(t *testing.T) {
	body := "- \tif x {\n+ \t\tif y {"
	got := wrapCommentSnippetLines(body, 80)

	for _, line := range got {
		if strings.Contains(line, "if x {") && !strings.Contains(line, "\t") {
			t.Fatalf("expected the proposed indentation to survive, got %q", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected two lines, got %q", got)
	}
}

func TestWrapPlainLineMeasuresVisibleWidth(t *testing.T) {
	coloured := "\x1b[32mone two three four five six\x1b[0m"
	got := wrapPlainLine(coloured, 40)
	if len(got) != 1 {
		t.Fatalf("visible text is 26 columns and fits in 40; ANSI must not count, got %d lines: %q", len(got), got)
	}
}

func ptr(value int) *int {
	return &value
}
