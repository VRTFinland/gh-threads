package interactive

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

	if tm.state.state != StateHelp || !tm.showHelp {
		t.Fatalf("expected help state, got state=%s showHelp=%v", tm.state.state, tm.showHelp)
	}
	out := tm.View()
	if !strings.Contains(out, "Help / Keybindings") || !strings.Contains(out, "j / k / ↑ / ↓") || !strings.Contains(out, "h / ←") {
		t.Fatalf("expected help text with key lines in view, got %q", out)
	}

	tm.updateHelp(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if tm.state.state != StateView || tm.showHelp {
		t.Fatalf("expected help to close on ?, state=%s showHelp=%v", tm.state.state, tm.showHelp)
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
