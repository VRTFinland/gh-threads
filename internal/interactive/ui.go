package interactive

import (
	"context"
	"strings"

	"github.com/VRTFinland/gh-threads/internal/threads"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
}

func Run(cfg ProgramConfig) error {
	model := NewModel(cfg.Conversation, cfg.Threads, cfg.Info, cfg.Context, cfg.Service)
	p := tea.NewProgram(newTeaModel(model, cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type teaModel struct {
	cfg   ProgramConfig
	state Model

	input                 textinput.Model
	inputPurpose          string // "reply", "filter", "author"
	statusIndex           int
	showStatus            bool
	showHelp              bool
	showFilterMenu        bool
	filterIndex           int
	authorSuggestionIndex int
	statusSuggestionIndex int
	loading               bool
	viewportHeight        int
	viewportWidth         int
}

func newTeaModel(m Model, cfg ProgramConfig) *teaModel {
	ti := textinput.New()
	ti.Placeholder = "Type here"
	ti.CharLimit = 4000
	defaultHeight := 60
	listHeight, _ := sectionHeights(defaultHeight, listLineEstimate(m.FilteredThreads()))
	m.SetListWindowSize(listHeight)
	return &teaModel{
		cfg:                   cfg,
		state:                 m,
		input:                 ti,
		authorSuggestionIndex: -1,
		statusSuggestionIndex: -1,
		viewportHeight:        defaultHeight,
		viewportWidth:         80,
	}
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
		case StateFilterMenu:
			return m.updateFilterMenu(msg)
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
		m.showStatus = false
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
		m.showHelp = true
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
			m.input.Placeholder = "Reply text"
			m.input.SetValue("")
			m.input.Focus()
		}
	case "S":
		if thread, ok := m.state.SelectedThread(); ok {
			m.showStatus = true
			if thread.IsResolved {
				m.statusIndex = 0
			} else {
				m.statusIndex = 1
			}
			m.state.state = StateStatus
		}
	case "s":
		m.state.errMessage = ""
		m.state.state = StateFilter
		m.inputPurpose = "status"
		m.input.Placeholder = "Filter by status (all/resolved/unresolved)"
		m.input.SetValue("")
		m.authorSuggestionIndex = -1
		m.statusSuggestionIndex = -1
		m.input.Focus()
		return m, textinput.Blink
	case "/":
		m.state.errMessage = ""
		m.state.state = StateFilter
		m.inputPurpose = "text"
		m.input.Placeholder = "Filter by text"
		m.input.SetValue(m.state.filters.Text)
		m.authorSuggestionIndex = -1
		m.statusSuggestionIndex = -1
		m.input.Focus()
		return m, textinput.Blink
	case "a":
		m.state.errMessage = ""
		m.state.state = StateFilter
		m.inputPurpose = "author"
		m.input.Placeholder = "Filter by author"
		m.input.SetValue("")
		m.authorSuggestionIndex = -1
		m.statusSuggestionIndex = -1
		m.input.Focus()
		return m, textinput.Blink
	case "f":
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
		return m, nil
	case "ctrl+d":
		fallthrough
	case "enter":
		text := m.input.Value()
		thread, ok := m.state.SelectedThread()
		if !ok || strings.TrimSpace(text) == "" {
			m.state.state = StateView
			return m, nil
		}
		m.loading = true
		commentIdx := m.state.selectedComment
		cmd := replyCmd(m.cfg.Ctx, m.state.service, thread, commentIdx, text)
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
		value := m.input.Value()
		switch m.inputPurpose {
		case "author":
			suggestions := m.state.AuthorSuggestions(value, authorSuggestionLimit)
			switch {
			case m.authorSuggestionIndex >= 0 && m.authorSuggestionIndex < len(suggestions):
				value = suggestions[m.authorSuggestionIndex]
				m.input.SetValue(value)
			case len(suggestions) == 1:
				value = suggestions[0]
				m.input.SetValue(value)
			}
			m.state.SetFilterAuthor(value)
			m.authorSuggestionIndex = -1
		case "status":
			filter, chosen := m.chooseStatusFilter(value)
			if chosen {
				m.state.filters.Status = filter
				m.statusSuggestionIndex = -1
				m.state.applyFilters()
			}
		default:
			m.state.SetFilterText(value)
		}
		m.state.state = StateView
		return m, nil
	}
	var cmd tea.Cmd
	prev := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.inputPurpose == "author" && m.input.Value() != prev {
		m.authorSuggestionIndex = -1
	}
	if m.inputPurpose == "status" && m.input.Value() != prev {
		m.statusSuggestionIndex = -1
	}
	return m, cmd
}

func (m *teaModel) cycleAuthorSuggestion(delta int) bool {
	suggestions := m.state.AuthorSuggestions(m.input.Value(), authorSuggestionLimit)
	if len(suggestions) == 0 {
		m.authorSuggestionIndex = -1
		return false
	}
	if m.authorSuggestionIndex == -1 {
		if delta > 0 {
			m.authorSuggestionIndex = 0
		} else {
			m.authorSuggestionIndex = len(suggestions) - 1
		}
		return true
	}
	m.authorSuggestionIndex = (m.authorSuggestionIndex + delta + len(suggestions)) % len(suggestions)
	return true
}

func (m *teaModel) cycleStatusSuggestion(delta int) bool {
	suggestions := statusSuggestions(m.input.Value())
	if len(suggestions) == 0 {
		m.statusSuggestionIndex = -1
		return false
	}
	if m.statusSuggestionIndex == -1 {
		if delta > 0 {
			m.statusSuggestionIndex = 0
		} else {
			m.statusSuggestionIndex = len(suggestions) - 1
		}
		return true
	}
	m.statusSuggestionIndex = (m.statusSuggestionIndex + delta + len(suggestions)) % len(suggestions)
	return true
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
		m.showStatus = false
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
			m.showStatus = false
			m.state.state = StateView
			return m, nil
		}
		resolved := m.statusIndex == 0
		m.loading = true
		return m, statusCmd(m.cfg.Ctx, m.state.service, thread, resolved)
	}
	return m, nil
}

func (m *teaModel) updateFilterMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showFilterMenu = false
		m.state.state = StateView
	case "j", "down":
		if m.filterIndex < 2 {
			m.filterIndex++
		}
	case "k", "up":
		if m.filterIndex > 0 {
			m.filterIndex--
		}
	case "enter":
		switch m.filterIndex {
		case 1:
			m.state.filters.Status = threads.StatusResolved
		case 2:
			m.state.filters.Status = threads.StatusUnresolved
		default:
			m.state.filters.Status = threads.StatusAll
		}
		m.state.applyFilters()
		m.showFilterMenu = false
		m.state.state = StateView
	}
	return m, nil
}

func (m *teaModel) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	default:
		m.showHelp = false
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
	return RenderView(m.state, width, height, listHeight, detailHeight, m.showStatus, m.statusIndex, m.showFilterMenu, m.filterIndex, m.showHelp, m.state.state, m.input, m.inputPurpose, m.authorSuggestionIndex, m.statusSuggestionIndex)
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

func listLineEstimate(threads []threads.ReviewThread) int {
	if len(threads) == 0 {
		return 1
	}
	lines := len(threads) + 1 // first path header
	for i := 1; i < len(threads); i++ {
		if threads[i].Path != threads[i-1].Path {
			lines++
		}
	}
	return lines
}
