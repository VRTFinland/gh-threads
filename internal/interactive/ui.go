package interactive

import (
	"context"
	"strings"

	"github.com/VRTFinland/gh-threads/internal/threads"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

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

	input          textinput.Model
	inputPurpose   string // "reply", "filter", "author"
	statusIndex    int
	showStatus     bool
	showFilterMenu bool
	filterIndex    int
	loading        bool
	viewportHeight int
	viewportWidth  int
}

func newTeaModel(m Model, cfg ProgramConfig) *teaModel {
	ti := textinput.New()
	ti.Placeholder = "Type here"
	ti.CharLimit = 4000
	defaultHeight := 60
	listHeight, _ := sectionHeights(defaultHeight)
	m.SetListWindowSize(listHeight)
	return &teaModel{
		cfg:            cfg,
		state:          m,
		input:          ti,
		viewportHeight: defaultHeight,
		viewportWidth:  80,
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
		listHeight, _ := sectionHeights(m.viewportHeight)
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
		m.showFilterMenu = true
		switch m.state.filters.Status {
		case threads.StatusResolved:
			m.filterIndex = 1
		case threads.StatusUnresolved:
			m.filterIndex = 2
		default:
			m.filterIndex = 0
		}
		m.state.state = StateFilterMenu
	case "/":
		m.state.errMessage = ""
		m.state.state = StateFilter
		m.inputPurpose = "text"
		m.input.Placeholder = "Filter by text"
		m.input.SetValue(m.state.filters.Text)
		m.input.Focus()
		return m, textinput.Blink
	case "a":
		m.state.errMessage = ""
		m.state.state = StateFilter
		m.inputPurpose = "author"
		m.input.Placeholder = "Filter by author"
		m.input.SetValue(m.state.filters.Author)
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
	case "enter":
		value := m.input.Value()
		if m.inputPurpose == "author" {
			m.state.SetFilterAuthor(value)
		} else {
			m.state.SetFilterText(value)
		}
		m.state.state = StateView
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	listHeight, detailHeight := sectionHeights(height)
	m.state.SetListWindowSize(listHeight)
	return RenderView(m.state, width, height, listHeight, detailHeight, m.showStatus, m.statusIndex, m.showFilterMenu, m.filterIndex, m.state.state, m.input, m.inputPurpose)
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
