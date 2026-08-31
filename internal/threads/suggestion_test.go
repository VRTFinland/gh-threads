package threads

import "testing"

func TestReplaceSuggestions(t *testing.T) {
	body := "before\n```suggestion\nnew line\n```\nafter\n```suggestion\nsecond\n```"

	var seen []string
	got := ReplaceSuggestions(body, func(suggested string) (string, bool) {
		seen = append(seen, suggested)
		return "<" + suggested + ">", true
	})

	if len(seen) != 2 || seen[0] != "new line" || seen[1] != "second" {
		t.Fatalf("expected both fences' contents, got %v", seen)
	}
	if got != "before\n<new line>\nafter\n<second>" {
		t.Fatalf("unexpected rewrite: %q", got)
	}
}

func TestReplaceSuggestionsKeepsTheFenceWhenDeclined(t *testing.T) {
	body := "```suggestion\nnew line\n```"

	if got := ReplaceSuggestions(body, func(string) (string, bool) { return "ignored", false }); got != body {
		t.Fatalf("expected the fence to survive untouched, got %q", got)
	}
}

func TestStripSuggestions(t *testing.T) {
	if got := StripSuggestions("keep\n```suggestion\ndrop\n```\nkeep too"); got != "keep\n\nkeep too" {
		t.Fatalf("unexpected strip result: %q", got)
	}
	if got := StripSuggestions("no fences here"); got != "no fences here" {
		t.Fatalf("a body without fences must come back unchanged, got %q", got)
	}
}

func TestHasSuggestionSeesAnUnclosedFence(t *testing.T) {
	if !HasSuggestion("```SUGGESTION\nhalf written") {
		t.Fatal("an unclosed fence still marks the comment as a suggestion")
	}
	if HasSuggestion("```go\ncode\n```") {
		t.Fatal("an ordinary code fence is not a suggestion")
	}
}
