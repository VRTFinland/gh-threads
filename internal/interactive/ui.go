package interactive

import (
	"context"
	"fmt"
	"strings"

	"github.com/VRTFinland/gh-threads/internal/threads"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const authorSuggestionLimit = 6

var statusOptions = []statusOption{
	{label: "all", filter: threads.StatusAll},
	{label: "resolved", filter: threads.StatusResolved},
	{label: "unresolved", filter: threads.StatusUnresolved},
}

type statusOption struct {
	label  string
	filter threads.StatusFilter
}

type ProgramConfig struct {
	Conversation []threads.ConversationComment
	Threads      []threads.ReviewThread
	Service      Service
	Refresh      func(force bool) (threads.PullRequestInfo, []threads.ConversationComment, []threads.ReviewThread, error)
	Ctx          context.Context
	Info         threads.PullRequestInfo
	Context      threads.Context
	// Filters and ShowDiff carry the command line's opening state into the
	// TUI, which owns both from then on.
	Filters  Filters
	ShowDiff bool
}

func Run(cfg ProgramConfig) error {
	p := tea.NewProgram(newTeaModel(initialModel(cfg), cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type teaModel struct {
	cfg   ProgramConfig
	state Model

	replyInput            textarea.Model
	filterInput           textinput.Model
	inputPurpose          string // "reply", "filter", "author"
	statusIndex           int
	authorSuggestionIndex int
	statusSuggestionIndex int
	loading               bool
	viewportHeight        int
	viewportWidth         int
}

func newTeaModel(m Model, cfg ProgramConfig) *teaModel {
	filter := textinput.New()
	filter.Placeholder = "Type here"
	filter.CharLimit = 4000

	reply := textarea.New()
	reply.Placeholder = "Reply (enter for newline, ctrl+d to send)"
	reply.CharLimit = 4000
	reply.SetHeight(1)
	reply.MaxHeight = 12
	reply.SetWidth(80)
	reply.Prompt = ""
	reply.ShowLineNumbers = false
	reply.EndOfBufferCharacter = ' '
	baseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231"))
	replyStyle := textarea.Style{
		Base:             baseStyle,
		CursorLine:       baseStyle,
		EndOfBuffer:      baseStyle,
		LineNumber:       baseStyle,
		CursorLineNumber: baseStyle,
		Placeholder:      baseStyle.Foreground(lipgloss.Color("239")),
		Text:             baseStyle,
		Prompt:           baseStyle,
	}
	reply.FocusedStyle = replyStyle
	reply.BlurredStyle = replyStyle

	defaultHeight := 60
	listHeight, _ := sectionHeights(defaultHeight, listLineEstimate(m.FilteredThreads()))
	m.SetListWindowSize(listHeight)
	return &teaModel{
		cfg:                   cfg,
		state:                 m,
		replyInput:            reply,
		filterInput:           filter,
		authorSuggestionIndex: -1,
		statusSuggestionIndex: -1,
		viewportHeight:        defaultHeight,
		viewportWidth:         80,
	}
}

// initialModel opens the TUI on the view the command line asked for.
func initialModel(cfg ProgramConfig) Model {
	m := NewModel(cfg.Conversation, cfg.Threads, cfg.Info, cfg.Context, cfg.Service)
	m.filters = cfg.Filters
	if m.filters.Status == "" {
		m.filters.Status = threads.StatusAll
	}
	if cfg.ShowDiff {
		m.detailMode = detailDiff
	}
	m.applyFilters()
	return m
}

func (m *teaModel) Init() tea.Cmd { return nil }

func (m *teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.loading {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch m.state.state {
		case StateReply:
			return m.updateReply(msg)
		case StateFilter:
			return m.updateFilter(msg)
		case StateStatus:
			return m.updateStatus(msg)
		case StateHelp:
			return m.updateHelp(msg)
		default:
			return m.updateView(msg)
		}
	case replyFinished:
		m.loading = false
		if msg.err != nil {
			m.state.errMessage = msg.err.Error()
		} else {
			m.state.infoMessage = "Reply posted"
		}
		m.state.state = StateView
		m.state.applyFilters()
		return m, nil
	case statusFinished:
		m.loading = false
		if msg.err != nil {
			m.state.errMessage = msg.err.Error()
		} else {
			m.state.infoMessage = "Thread status updated"
		}
		m.state.state = StateView
		m.state.applyFilters()
		return m, nil
	case refreshFinished:
		m.loading = false
		if msg.err != nil {
			m.state.errMessage = msg.err.Error()
		} else {
			m.state.UpdateThreads(msg.conversation, msg.threads)
			m.state.UpdatePRInfo(msg.info)
			m.state.infoMessage = "Data refreshed"
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.viewportHeight = msg.Height
		m.viewportWidth = msg.Width
		listHeight, _ := sectionHeights(m.viewportHeight, listLineEstimate(m.state.FilteredThreads()))
		m.state.SetListWindowSize(listHeight)
		return m, nil
	}
	return m, nil
}

func (m *teaModel) updateView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "j", "down":
		m.state.MoveSelection(1)
	case "k", "up":
		m.state.MoveSelection(-1)
	case "enter", " ":
		m.state.ToggleDetail()
	case "d":
		if m.state.detailMode == detailSnippet {
			m.state.detailMode = detailDiff
		} else {
			m.state.detailMode = detailSnippet
		}
	case "?":
		m.state.state = StateHelp
	case "h", "left":
		if m.state.selectedComment > 0 {
			m.state.selectedComment--
		}
	case "l", "right":
		if thread, ok := m.state.SelectedThread(); ok {
			if m.state.selectedComment < len(thread.Comments)-1 {
				m.state.selectedComment++
			}
		}
	case "r":
		if _, ok := m.state.SelectedThread(); ok {
			m.state.errMessage = ""
			m.state.infoMessage = ""
			m.state.state = StateReply
			m.inputPurpose = "reply"
			m.replyInput.SetValue("")
			m.replyInput.CursorEnd()
			m.replyInput.Focus()
			m.adjustReplyHeight()
		}
	case "S":
		if thread, ok := m.state.SelectedThread(); ok {
			if thread.IsResolved {
				m.statusIndex = 0
			} else {
				m.statusIndex = 1
			}
			m.state.state = StateStatus
		}
	case "s":
		return m, m.openFilterPrompt("status", "Filter by status (all/resolved/unresolved)", "")
	case "/":
		return m, m.openFilterPrompt("text", "Filter by text", m.state.filters.Text)
	case "a":
		return m, m.openFilterPrompt("author", "Filter by author", "")
	case "f":
		m.state.errMessage = ""
		m.state.CycleStatusFilter()
	case "R":
		m.loading = true
		return m, refreshCmd(m.cfg)
	}
	return m, nil
}

func (m *teaModel) updateReply(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state.state = StateView
		m.replyInput.Blur()
		return m, nil
	case "ctrl+d":
		text := m.replyInput.Value()
		thread, ok := m.state.SelectedThread()
		if !ok || strings.TrimSpace(text) == "" {
			m.state.state = StateView
			m.replyInput.Blur()
			return m, nil
		}
		m.loading = true
		commentIdx := m.state.selectedComment
		cmd := replyCmd(m.cfg.Ctx, m.state.service, thread, commentIdx, text)
		return m, cmd
	}
	var cmd tea.Cmd
	m.replyInput, cmd = m.replyInput.Update(msg)
	m.adjustReplyHeight()
	return m, cmd
}

// openFilterPrompt puts the shared filter input into one of its three modes.
func (m *teaModel) openFilterPrompt(purpose, placeholder, value string) tea.Cmd {
	m.state.errMessage = ""
	m.state.state = StateFilter
	m.inputPurpose = purpose
	m.filterInput.Placeholder = placeholder
	m.filterInput.SetValue(value)
	m.authorSuggestionIndex = -1
	m.statusSuggestionIndex = -1
	m.filterInput.Focus()
	return textinput.Blink
}

func (m *teaModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state.state = StateView
		return m, nil
	case "down", "tab":
		if m.inputPurpose == "author" && m.cycleAuthorSuggestion(1) {
			return m, nil
		}
		if m.inputPurpose == "status" && m.cycleStatusSuggestion(1) {
			return m, nil
		}
	case "up", "shift+tab":
		if m.inputPurpose == "author" && m.cycleAuthorSuggestion(-1) {
			return m, nil
		}
		if m.inputPurpose == "status" && m.cycleStatusSuggestion(-1) {
			return m, nil
		}
	case "enter":
		value := m.filterInput.Value()
		switch m.inputPurpose {
		case "author":
			suggestions := m.state.AuthorSuggestions(value, authorSuggestionLimit)
			switch {
			case m.authorSuggestionIndex >= 0 && m.authorSuggestionIndex < len(suggestions):
				value = suggestions[m.authorSuggestionIndex]
				m.filterInput.SetValue(value)
			// Only auto-accept a lone suggestion the user actually narrowed to.
			// An empty query matches every author, so in a single-author PR this
			// would set the filter on the very keystrokes that clear it.
			case strings.TrimSpace(value) != "" && len(suggestions) == 1:
				value = suggestions[0]
				m.filterInput.SetValue(value)
			}
			m.state.SetFilterAuthor(value)
			m.authorSuggestionIndex = -1
		case "status":
			// Empty input cancels, like esc -- unless a suggestion is highlighted,
			// which an empty query still lists.
			if strings.TrimSpace(value) == "" && m.statusSuggestionIndex < 0 {
				m.state.state = StateView
				return m, nil
			}
			filter, chosen := m.chooseStatusFilter(value)
			if !chosen {
				// Stay in the prompt: the bottom bar is drawn in every state, so
				// the user sees the message and can fix the typo in place.
				m.state.errMessage = fmt.Sprintf("unknown status %q: use all, resolved or unresolved", strings.TrimSpace(value))
				return m, nil
			}
			m.state.errMessage = ""
			m.state.filters.Status = filter
			m.statusSuggestionIndex = -1
			m.state.applyFilters()
		default:
			m.state.SetFilterText(value)
		}
		m.state.state = StateView
		return m, nil
	}
	var cmd tea.Cmd
	prev := m.filterInput.Value()
	m.filterInput, cmd = m.filterInput.Update(msg)
	if m.inputPurpose == "author" && m.filterInput.Value() != prev {
		m.authorSuggestionIndex = -1
	}
	if m.inputPurpose == "status" && m.filterInput.Value() != prev {
		m.statusSuggestionIndex = -1
	}
	return m, cmd
}

// cycleIndex moves a suggestion cursor by delta, wrapping around, and reports
// whether a suggestion is now highlighted. Entering from -1 lands on the first
// or last entry depending on direction.
func cycleIndex(index *int, count, delta int) bool {
	if count == 0 {
		*index = -1
		return false
	}
	if *index == -1 {
		if delta > 0 {
			*index = 0
		} else {
			*index = count - 1
		}
		return true
	}
	*index = (*index + delta + count) % count
	return true
}

func (m *teaModel) cycleAuthorSuggestion(delta int) bool {
	return cycleIndex(&m.authorSuggestionIndex, len(m.state.AuthorSuggestions(m.filterInput.Value(), authorSuggestionLimit)), delta)
}

func (m *teaModel) cycleStatusSuggestion(delta int) bool {
	return cycleIndex(&m.statusSuggestionIndex, len(statusSuggestions(m.filterInput.Value())), delta)
}

func (m *teaModel) chooseStatusFilter(value string) (threads.StatusFilter, bool) {
	suggestions := statusSuggestions(value)
	if m.statusSuggestionIndex >= 0 && m.statusSuggestionIndex < len(suggestions) {
		return suggestions[m.statusSuggestionIndex].filter, true
	}
	if len(suggestions) == 1 {
		return suggestions[0].filter, true
	}
	return parseStatusInput(value)
}

func statusSuggestions(query string) []statusOption {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := make([]statusOption, 0, len(statusOptions))
	for _, option := range statusOptions {
		if query == "" || strings.HasPrefix(option.label, query) {
			matches = append(matches, option)
		}
	}
	return matches
}

func parseStatusInput(value string) (threads.StatusFilter, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "u", "unresolved":
		return threads.StatusUnresolved, true
	case "r", "resolved":
		return threads.StatusResolved, true
	case "a", "all":
		return threads.StatusAll, true
	default:
		return "", false
	}
}

func (m *teaModel) updateStatus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state.state = StateView
	case "j", "down":
		if m.statusIndex < 1 {
			m.statusIndex++
		}
	case "k", "up":
		if m.statusIndex > 0 {
			m.statusIndex--
		}
	case "enter":
		thread, ok := m.state.SelectedThread()
		if !ok {
			m.state.state = StateView
			return m, nil
		}
		resolved := m.statusIndex == 0
		m.loading = true
		return m, statusCmd(m.cfg.Ctx, m.state.service, thread, resolved)
	}
	return m, nil
}

func (m *teaModel) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	default:
		m.state.state = StateView
		return m, nil
	}
}

func (m *teaModel) View() string {
	if m.loading {
		return "Loading..."
	}
	height := m.viewportHeight
	if height <= 0 {
		height = 60
	}
	width := m.viewportWidth
	if width <= 0 {
		width = 80
	}
	listHeight, detailHeight := sectionHeights(height, listLineEstimate(m.state.FilteredThreads()))
	m.state.SetListWindowSize(listHeight)
	if width > 2 {
		m.replyInput.SetWidth(width - 2)
		m.adjustReplyHeight()
	}
	return RenderView(m.state, width, height, listHeight, detailHeight, m.inputs())
}

func (m *teaModel) inputs() viewInputs {
	return viewInputs{
		reply:                 m.replyInput,
		filter:                m.filterInput,
		purpose:               m.inputPurpose,
		statusIndex:           m.statusIndex,
		authorSuggestionIndex: m.authorSuggestionIndex,
		statusSuggestionIndex: m.statusSuggestionIndex,
	}
}

func (m *teaModel) adjustReplyHeight() {
	content := m.replyInput.Value()
	lines := strings.Count(content, "\n") + 1
	if content != "" && lines < 2 {
		lines = 2 // leave room for the newline enter will insert
	}
	m.replyInput.SetHeight(min(lines, m.replyInput.MaxHeight))
}

type replyFinished struct{ err error }
type statusFinished struct{ err error }
type refreshFinished struct {
	info         threads.PullRequestInfo
	conversation []threads.ConversationComment
	threads      []threads.ReviewThread
	err          error
}

func replyCmd(ctx context.Context, service Service, thread *threads.ReviewThread, commentIdx int, body string) tea.Cmd {
	return func() tea.Msg {
		_, err := service.ReplyToThread(ctx, thread, commentIdx, body)
		return replyFinished{err: err}
	}
}

func statusCmd(ctx context.Context, service Service, thread *threads.ReviewThread, resolved bool) tea.Cmd {
	return func() tea.Msg {
		err := service.SetThreadStatus(ctx, thread, resolved)
		return statusFinished{err: err}
	}
}

func refreshCmd(cfg ProgramConfig) tea.Cmd {
	return func() tea.Msg {
		info, convo, threads, err := cfg.Refresh(true)
		return refreshFinished{info: info, conversation: convo, threads: threads, err: err}
	}
}
