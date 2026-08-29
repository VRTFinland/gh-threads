package interactive

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/VRTFinland/gh-threads/internal/threads"
)

type State string

const (
	StateView   State = "view"
	StateReply  State = "reply"
	StateStatus State = "status"
	StateFilter State = "filter"
	StateHelp   State = "help"
)

type Service interface {
	ReplyToThread(ctx context.Context, thread *threads.ReviewThread, commentIndex int, body string) (*threads.ThreadComment, error)
	SetThreadStatus(ctx context.Context, thread *threads.ReviewThread, resolved bool) error
}

// Model keeps track of interactive state.
type Model struct {
	threads      []threads.ReviewThread
	conversation []threads.ConversationComment

	filteredIndexes []int
	selectedThread  int
	selectedComment int
	listOffset      int

	detailExpanded bool
	detailMode     detailMode
	errMessage     string
	infoMessage    string

	filters Filters
	state   State

	service    Service
	prInfo     threads.PullRequestInfo
	context    threads.Context
	windowSize int
}

type detailMode int

const (
	detailSnippet detailMode = iota
	detailDiff
)

// Filters control which threads are visible.
type Filters struct {
	Author string
	Status threads.StatusFilter
	Text   string
}

func NewModel(conversation []threads.ConversationComment, reviewThreads []threads.ReviewThread, info threads.PullRequestInfo, ctx threads.Context, service Service) Model {
	m := Model{
		threads:      reviewThreads,
		conversation: conversation,
		filters: Filters{
			Status: threads.StatusAll,
		},
		state:          StateView,
		detailMode:     detailSnippet,
		detailExpanded: true,
		service:        service,
		prInfo:         info,
		context:        ctx,
	}
	m.applyFilters()
	return m
}

func (m *Model) applyFilters() {
	indexes := make([]int, 0, len(m.threads))
	for idx, thread := range m.threads {
		if m.filters.Status == threads.StatusResolved && !thread.IsResolved {
			continue
		}
		if m.filters.Status == threads.StatusUnresolved && thread.IsResolved {
			continue
		}
		if m.filters.Author != "" && !threadHasAuthor(thread, m.filters.Author) {
			continue
		}
		if m.filters.Text != "" && !threadMatchesText(thread, m.filters.Text) {
			continue
		}
		indexes = append(indexes, idx)
	}
	m.filteredIndexes = indexes
	if m.selectedThread >= len(indexes) {
		m.selectedThread = len(indexes) - 1
	}
	if m.selectedThread < 0 {
		m.selectedThread = 0
	}
	window := max(1, m.windowSize)
	m.listOffset = clamp(m.listOffset, 0, max(0, len(indexes)-window))
	m.selectedComment = 0
}

func threadHasAuthor(thread threads.ReviewThread, author string) bool {
	authorLower := strings.ToLower(author)
	for _, comment := range thread.Comments {
		if strings.ToLower(comment.Author) == authorLower {
			return true
		}
	}
	return false
}

func threadMatchesText(thread threads.ReviewThread, text string) bool {
	textLower := strings.ToLower(text)
	if strings.Contains(strings.ToLower(thread.Path), textLower) {
		return true
	}
	for _, comment := range thread.Comments {
		if strings.Contains(strings.ToLower(comment.Body), textLower) {
			return true
		}
	}
	return false
}

func (m Model) FilteredThreads() []threads.ReviewThread {
	result := make([]threads.ReviewThread, 0, len(m.filteredIndexes))
	for _, idx := range m.filteredIndexes {
		result = append(result, m.threads[idx])
	}
	return result
}

func (m Model) SelectedThread() (*threads.ReviewThread, bool) {
	if len(m.filteredIndexes) == 0 || m.selectedThread < 0 || m.selectedThread >= len(m.filteredIndexes) {
		return nil, false
	}
	return &m.threads[m.filteredIndexes[m.selectedThread]], true
}

func (m *Model) MoveSelection(delta int) {
	if len(m.filteredIndexes) == 0 {
		return
	}
	m.selectedThread += delta
	if m.selectedThread < 0 {
		m.selectedThread = 0
	}
	if m.selectedThread >= len(m.filteredIndexes) {
		m.selectedThread = len(m.filteredIndexes) - 1
	}
	m.ensureSelectionVisible()
	m.selectedComment = 0
}

func (m *Model) ToggleDetail() {
	m.detailExpanded = !m.detailExpanded
}

func (m *Model) SetFilterAuthor(author string) {
	m.filters.Author = strings.TrimSpace(author)
	m.applyFilters()
}

func (m *Model) SetFilterText(text string) {
	m.filters.Text = strings.TrimSpace(text)
	m.applyFilters()
}

func (m *Model) CycleStatusFilter() {
	switch m.filters.Status {
	case threads.StatusAll:
		m.filters.Status = threads.StatusResolved
	case threads.StatusResolved:
		m.filters.Status = threads.StatusUnresolved
	default:
		m.filters.Status = threads.StatusAll
	}
	m.applyFilters()
}

func (m *Model) UpdateThreads(conversation []threads.ConversationComment, threads []threads.ReviewThread) {
	m.conversation = conversation
	m.threads = threads
	m.applyFilters()
}

func (m *Model) UpdateThread(thread threads.ReviewThread) {
	for idx := range m.threads {
		if m.threads[idx].ThreadID == thread.ThreadID {
			m.threads[idx] = thread
			break
		}
	}
	m.applyFilters()
}

func (m *Model) Page(delta int, window int) {
	if len(m.filteredIndexes) == 0 {
		return
	}
	m.selectedThread += delta * window
	if m.selectedThread < 0 {
		m.selectedThread = 0
	}
	if m.selectedThread >= len(m.filteredIndexes) {
		m.selectedThread = len(m.filteredIndexes) - 1
	}
	m.ensureSelectionVisible()
}

func (m *Model) ensureSelectionVisible() {
	window := max(1, m.windowSize)
	if m.selectedThread < m.listOffset {
		m.listOffset = m.selectedThread
	} else if m.selectedThread >= m.listOffset+window {
		m.listOffset = m.selectedThread - window + 1
	}
	m.listOffset = clamp(m.listOffset, 0, max(0, len(m.filteredIndexes)-window))
}

func clamp(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) SetListWindowSize(size int) {
	if size < 1 {
		size = 1
	}
	m.windowSize = size
	m.ensureSelectionVisible()
}

func (m *Model) UpdatePRInfo(info threads.PullRequestInfo) {
	m.prInfo = info
}

func (m Model) TotalCommentCount() int {
	count := len(m.conversation)
	for _, thread := range m.threads {
		count += len(thread.Comments)
	}
	return count
}

func (m Model) PRURL() string {
	if m.prInfo.URL != "" {
		return m.prInfo.URL
	}
	if m.context.Owner != "" && m.context.Repo != "" && m.context.PullRequest > 0 {
		return fmt.Sprintf("https://github.com/%s/%s/pull/%d", m.context.Owner, m.context.Repo, m.context.PullRequest)
	}
	return ""
}

func (m Model) PRAuthor() string {
	if strings.TrimSpace(m.prInfo.Author) == "" {
		return "unknown"
	}
	return m.prInfo.Author
}

func (m Model) MergeableState() string {
	if strings.TrimSpace(m.prInfo.Mergeable) == "" {
		return "unknown"
	}
	return strings.ToLower(m.prInfo.Mergeable)
}

func (m Model) KnownAuthors() []string {
	seen := make(map[string]string)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		lower := strings.ToLower(name)
		if _, ok := seen[lower]; !ok {
			seen[lower] = name
		}
	}
	for _, comment := range m.conversation {
		add(comment.Author)
	}
	for _, thread := range m.threads {
		for _, comment := range thread.Comments {
			add(comment.Author)
		}
	}
	names := make([]string, 0, len(seen))
	for _, original := range seen {
		names = append(names, original)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func (m Model) AuthorSuggestions(query string, limit int) []string {
	candidates := m.KnownAuthors()
	type suggestion struct {
		name  string
		score int
	}
	matches := make([]suggestion, 0, len(candidates))
	for _, name := range candidates {
		if score, ok := fuzzyScore(query, name); ok {
			matches = append(matches, suggestion{name: name, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return strings.ToLower(matches[i].name) < strings.ToLower(matches[j].name)
		}
		return matches[i].score < matches[j].score
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	result := make([]string, len(matches))
	for i, item := range matches {
		result[i] = item.name
	}
	return result
}

func fuzzyScore(query string, candidate string) (int, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	candidateLower := strings.ToLower(strings.TrimSpace(candidate))
	if candidateLower == "" {
		return 0, false
	}
	if query == "" {
		return 0, true
	}
	if idx := strings.Index(candidateLower, query); idx >= 0 {
		return idx, true
	}
	queryRunes := []rune(query)
	candidateRunes := []rune(candidateLower)
	qi := 0
	score := 0
	last := -1
	for pos, r := range candidateRunes {
		if qi < len(queryRunes) && r == queryRunes[qi] {
			if last == -1 {
				score += pos
			} else {
				score += pos - last - 1
			}
			last = pos
			qi++
			if qi == len(queryRunes) {
				return 100 + score, true
			}
		}
	}
	return 0, false
}
