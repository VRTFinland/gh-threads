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
