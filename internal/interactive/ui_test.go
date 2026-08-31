package interactive

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

func TestUpdateFilterUsesSelectedAuthorSuggestion(t *testing.T) {
	conversation := []threads.ConversationComment{
		{Author: "Aaron"},
		{Author: "Alice"},
		{Author: "Bob"},
	}
	model := NewModel(conversation, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state = model
	tm.state.state = StateFilter
	tm.inputPurpose = "author"
	tm.filterInput.SetValue("a")
	tm.authorSuggestionIndex = 1

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.filters.Author != "Alice" {
		t.Fatalf("expected author filter to pick highlighted suggestion, got %q", tm.state.filters.Author)
	}
	if tm.filterInput.Value() != "Alice" {
		t.Fatalf("expected input value to reflect selected suggestion, got %q", tm.filterInput.Value())
	}
	if tm.state.state != StateView {
		t.Fatalf("expected state to return to view after applying filter, got %s", tm.state.state)
	}
}

func TestUpdateFilterAppliesSingleAuthorSuggestionWithoutSelection(t *testing.T) {
	conversation := []threads.ConversationComment{
		{Author: "Alice"},
		{Author: "Bob"},
	}
	model := NewModel(conversation, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state = model
	tm.state.state = StateFilter
	tm.inputPurpose = "author"
	tm.filterInput.SetValue("ali")

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.filters.Author != "Alice" {
		t.Fatalf("expected the lone matching author to be applied, got %q", tm.state.filters.Author)
	}
}

func TestUpdateFilterAcceptsStatusShortcuts(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state = model
	tm.state.state = StateFilter
	tm.inputPurpose = "status"
	tm.filterInput.SetValue("u")

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.filters.Status != threads.StatusUnresolved {
		t.Fatalf("expected status filter to resolve \"u\" to unresolved, got %s", tm.state.filters.Status)
	}
	if tm.state.state != StateView {
		t.Fatalf("expected state to return to view after applying filter, got %s", tm.state.state)
	}
}

func TestHelpModalTogglesOnQuestionMark(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})

	tm.updateView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}, Alt: false})

	if tm.state.state != StateHelp {
		t.Fatalf("expected help state, got %s", tm.state.state)
	}
	out := tm.View()
	if !strings.Contains(out, "Help / Keybindings") || !strings.Contains(out, "j / k / ↑ / ↓") || !strings.Contains(out, "h / ←") {
		t.Fatalf("expected help text with key lines in view, got %q", out)
	}

	tm.updateHelp(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if tm.state.state != StateView {
		t.Fatalf("expected help to close on ?, got %s", tm.state.state)
	}
}

func TestCommentNavigationWithLeftRightKeys(t *testing.T) {
	threadsData := []threads.ReviewThread{
		{
			ThreadID: "t1",
			Comments: []threads.ThreadComment{
				{ID: "c1"},
				{ID: "c2"},
			},
		},
	}
	model := NewModel(nil, threadsData, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state = model

	tm.updateView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if tm.state.selectedComment != 1 {
		t.Fatalf("expected l to move to next comment, got %d", tm.state.selectedComment)
	}

	tm.updateView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if tm.state.selectedComment != 0 {
		t.Fatalf("expected h to move back to previous comment, got %d", tm.state.selectedComment)
	}

	tm.updateView(tea.KeyMsg{Type: tea.KeyLeft})
	if tm.state.selectedComment != 0 {
		t.Fatalf("expected left arrow to clamp at first comment, got %d", tm.state.selectedComment)
	}
}

func TestUpdateReplyKeepsCursorPositionWithinText(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state.state = StateReply
	tm.inputPurpose = "reply"
	tm.replyInput.Focus()

	for _, r := range "ab" {
		tm.updateReply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	tm.updateReply(tea.KeyMsg{Type: tea.KeyLeft})
	tm.updateReply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	if got := tm.replyInput.Value(); got != "aXb" {
		t.Fatalf("expected cursor to stay where the user moved it, got %q", got)
	}
}

func TestUpdateFilterRejectsUnknownStatus(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state.state = StateFilter
	tm.inputPurpose = "status"
	tm.filterInput.SetValue("x")

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.state != StateFilter {
		t.Fatalf("expected to stay in the prompt so the typo can be fixed, got %s", tm.state.state)
	}
	if tm.state.filters.Status != threads.StatusAll {
		t.Fatalf("expected the filter to be unchanged, got %q", tm.state.filters.Status)
	}
	if !strings.Contains(tm.state.errMessage, "unknown status") {
		t.Fatalf("expected an error message, got %q", tm.state.errMessage)
	}
	if bar := ansi.Strip(renderBottomBar(tm.state, 200)); !strings.Contains(bar, "unknown status") {
		t.Fatalf("expected the message to be visible in the bottom bar, got %q", bar)
	}
}

func TestUpdateFilterEmptyStatusCancels(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state.state = StateFilter
	tm.inputPurpose = "status"
	tm.filterInput.SetValue("")

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.state != StateView {
		t.Fatalf("expected empty input to cancel, got %s", tm.state.state)
	}
	if tm.state.errMessage != "" {
		t.Fatalf("expected no error for a plain cancel, got %q", tm.state.errMessage)
	}
}

func TestUpdateFilterEmptyStatusWithHighlightedSuggestionApplies(t *testing.T) {
	model := NewModel(nil, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state.state = StateFilter
	tm.inputPurpose = "status"
	tm.filterInput.SetValue("")
	tm.statusSuggestionIndex = 2

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.state != StateView {
		t.Fatalf("expected a highlighted suggestion to apply, got %s", tm.state.state)
	}
	if tm.state.filters.Status == threads.StatusAll {
		t.Fatalf("expected the highlighted suggestion to change the filter, got %q", tm.state.filters.Status)
	}
}

// Pressing a clears the input, and an empty query matches every author. In a PR
// with a single commenting author that made a + enter -- the natural way to
// clear the filter -- set it instead.
func TestUpdateFilterEmptyAuthorClearsFilterWithSingleAuthor(t *testing.T) {
	conversation := []threads.ConversationComment{{Author: "Alice"}, {Author: "Alice"}}
	model := NewModel(conversation, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state.SetFilterAuthor("Alice")
	tm.state.state = StateFilter
	tm.inputPurpose = "author"
	tm.filterInput.SetValue("")

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.filters.Author != "" {
		t.Fatalf("expected the author filter to be cleared, got %q", tm.state.filters.Author)
	}
}

func TestUpdateFilterEmptyAuthorClearsFilterWithManyAuthors(t *testing.T) {
	conversation := []threads.ConversationComment{{Author: "Alice"}, {Author: "Bob"}}
	model := NewModel(conversation, nil, threads.PullRequestInfo{}, threads.Context{}, nil)
	tm := newTeaModel(model, ProgramConfig{})
	tm.state.SetFilterAuthor("Alice")
	tm.state.state = StateFilter
	tm.inputPurpose = "author"
	tm.filterInput.SetValue("")

	tm.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})

	if tm.state.filters.Author != "" {
		t.Fatalf("expected the author filter to be cleared, got %q", tm.state.filters.Author)
	}
}

func TestInitialModelAppliesConfig(t *testing.T) {
	list := []threads.ReviewThread{
		{ThreadID: "t1", Path: "a.go", IsResolved: true, Comments: []threads.ThreadComment{{ID: "c1", Author: "alice", Body: "one"}}},
		{ThreadID: "t2", Path: "a.go", Comments: []threads.ThreadComment{{ID: "c2", Author: "bob", Body: "two"}}},
	}

	m := initialModel(ProgramConfig{
		Threads:  list,
		Filters:  Filters{Status: threads.StatusUnresolved, Author: "bob"},
		ShowDiff: true,
	})

	visible := m.FilteredThreads()
	if len(visible) != 1 || visible[0].ThreadID != "t2" {
		t.Fatalf("expected the command line's filters to be in force, got %d threads", len(visible))
	}
	if m.detailMode != detailDiff {
		t.Fatal("expected --show-diff to open the diff view")
	}

	plain := initialModel(ProgramConfig{Threads: list})
	if len(plain.FilteredThreads()) != 2 {
		t.Fatal("expected an unset status filter to show every thread")
	}
	if plain.detailMode != detailSnippet {
		t.Fatal("expected the snippet view by default")
	}
}

// BenchmarkView measures a whole frame, which is what a keystroke costs.
func BenchmarkView(b *testing.B) {
	body := strings.Repeat("This is a **realistic** review comment with `code` and a [link](http://x). ", 8)
	model := listModel(400)
	for i := range model.threads {
		model.threads[i].Comments[0].Body = fmt.Sprintf("%s (%d)", body, i)
	}
	model.selectedThread = 399
	tm := newTeaModel(model, ProgramConfig{})
	tm.viewportHeight = 50
	tm.viewportWidth = 120
	resetMarkdownCache()
	previewCache.reset()
	tm.View() // warm the markdown cache; a session pays this once
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.View()
	}
}
