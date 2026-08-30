package render

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

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
	markdownLines := renderCommentSnippet(commentBody, true, width, nil, nil, nil, false)
	plainLines := renderCommentSnippet(commentBody, false, width, nil, nil, nil, false)
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
	start := 11
	end := 11
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

	lines := renderCommentSnippet(body, false, 80, snippet, &start, &end, false)
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
	start := 5
	end := 5
	result := replaceSuggestionBlocks(body, nil, &start, &end, false, true)
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
	rendered := renderCommentSnippet(body, false, 60, nil, nil, nil, false)
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
	if printCommentBlock(&buf, "   ", 100, false, false, nil, nil, nil) {
		t.Fatal("expected an all-blank body to report nothing printed")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for an all-blank body, got %q", buf.String())
	}
	if !printCommentBlock(&buf, "real text", 100, false, false, nil, nil, nil) {
		t.Fatal("expected a real body to report it printed")
	}
}

func intPtr(v int) *int { return &v }

func TestCommentLineRange(t *testing.T) {
	cases := []struct {
		name       string
		comment    threads.ThreadComment
		start, end *int
	}{
		{
			name: "prefers the original pair over the current one",
			comment: threads.ThreadComment{
				Line: intPtr(118), StartLine: intPtr(116),
				OriginalLine: intPtr(120), OriginalStartLine: intPtr(115),
			},
			start: intPtr(115), end: intPtr(120),
		},
		{
			name: "outdated comment with no current lines",
			comment: threads.ThreadComment{
				OriginalLine: intPtr(120), OriginalStartLine: intPtr(115),
			},
			start: intPtr(115), end: intPtr(120),
		},
		{
			name:    "falls back to the current pair without an original line",
			comment: threads.ThreadComment{Line: intPtr(40), StartLine: intPtr(38)},
			start:   intPtr(38), end: intPtr(40),
		},
		{
			name:    "single line comment",
			comment: threads.ThreadComment{OriginalLine: intPtr(12)},
			start:   intPtr(12), end: intPtr(12),
		},
		{
			name: "derives the start from the span for a cached comment",
			comment: threads.ThreadComment{
				Line: intPtr(118), StartLine: intPtr(116),
				OriginalLine: intPtr(120),
			},
			start: intPtr(118), end: intPtr(120),
		},
		{
			name:    "no line information at all",
			comment: threads.ThreadComment{},
			start:   nil, end: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := commentLineRange(tc.comment)
			if !reflect.DeepEqual(gotStart, tc.start) || !reflect.DeepEqual(gotEnd, tc.end) {
				t.Fatalf("got (%v, %v), want (%v, %v)",
					derefOrNil(gotStart), derefOrNil(gotEnd), derefOrNil(tc.start), derefOrNil(tc.end))
			}
		})
	}
}

func derefOrNil(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// TestRenderCommentSnippet_MultiLineSuggestionUsesOriginalRange proves the
// coordinate fix end to end: the removed lines must come from the anchor
// expressed in the snippet's own (original commit) space.
func TestRenderCommentSnippet_MultiLineSuggestionUsesOriginalRange(t *testing.T) {
	body := "```suggestion\nreplacement\n```"
	comment := threads.ThreadComment{
		Line: intPtr(17), StartLine: intPtr(16),
		OriginalLine: intPtr(12), OriginalStartLine: intPtr(11),
	}
	snippet := &threads.HistoricalSnippet{
		Commit: "abcdef123", Path: "file.go", StartLine: 10, HighlightLine: 12,
		Lines: []string{"first", "second", "third", "fourth"}, // lines 10..13
	}

	start, end := commentLineRange(comment)
	lines := renderCommentSnippet(body, false, 80, snippet, start, end, false)

	expected := []string{"- second", "- third", "+ replacement"}
	if !reflect.DeepEqual(lines, expected) {
		t.Fatalf("expected the original-space range 11-12 to be removed, got %v", lines)
	}
}
