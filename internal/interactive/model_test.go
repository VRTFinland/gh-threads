package interactive

import (
	"reflect"
	"testing"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

func TestNewModelExpandsDetailByDefault(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	if !model.detailExpanded {
		t.Fatalf("expected new model to default to expanded detail view")
	}
}

func TestKnownAuthorsCollectsAndSorts(t *testing.T) {
	model := Model{
		conversation: []threads.ConversationComment{
			{Author: "Alice"},
			{Author: "bob"},
		},
		threads: []threads.ReviewThread{
			{
				Comments: []threads.ThreadComment{
					{Author: "ALICE"},
					{Author: "carol"},
				},
			},
		},
	}
	got := model.KnownAuthors()
	expected := []string{"Alice", "bob", "carol"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestAuthorSuggestionsUsesFuzzyMatching(t *testing.T) {
	model := Model{
		conversation: []threads.ConversationComment{
			{Author: "Alice"},
			{Author: "Bob"},
			{Author: "Charlie"},
		},
	}
	suggestions := model.AuthorSuggestions("li", 5)
	expected := []string{"Alice", "Charlie"}
	if !reflect.DeepEqual(suggestions, expected) {
		t.Fatalf("expected suggestions %v, got %v", expected, suggestions)
	}
	subsequence := model.AuthorSuggestions("ce", 5)
	if len(subsequence) == 0 || subsequence[0] != "Alice" {
		t.Fatalf("expected subsequence match to include Alice, got %v", subsequence)
	}
}
