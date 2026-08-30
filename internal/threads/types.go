package threads

import "fmt"

type Context struct {
	Owner       string
	Repo        string
	PullRequest int
}

type ConversationComment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
	URL       string `json:"url"`
}

type ThreadComment struct {
	ID                string             `json:"id"`
	DatabaseID        int                `json:"database_id,omitempty"`
	Author            string             `json:"author"`
	Body              string             `json:"body"`
	CreatedAt         string             `json:"created_at"`
	UpdatedAt         string             `json:"updated_at,omitempty"`
	Path              string             `json:"path"`
	Line              *int               `json:"line"`
	OriginalLine      *int               `json:"original_line"`
	StartLine         *int               `json:"start_line"`
	OriginalStartLine *int               `json:"original_start_line"`
	DiffHunk          string             `json:"diff_hunk"`
	URL               string             `json:"url"`
	CommitSHA         string             `json:"commit_sha,omitempty"`
	Snippet           *HistoricalSnippet `json:"historical_snippet,omitempty"`
}

type ReviewThread struct {
	ThreadID     string          `json:"thread_id"`
	Path         string          `json:"path"`
	Line         *int            `json:"line"`
	OriginalLine *int            `json:"original_line"`
	StartLine    *int            `json:"start_line"`
	IsResolved   bool            `json:"is_resolved"`
	IsOutdated   bool            `json:"is_outdated"`
	Comments     []ThreadComment `json:"comments"`
	repoOwner    string
	repoName     string
	pullRequest  int
}

type Payload struct {
	Repository               string                `json:"repository"`
	PullRequest              int                   `json:"pull_request"`
	ConversationCommentCount int                   `json:"conversation_comment_count"`
	ReviewThreadCount        int                   `json:"review_thread_count"`
	ConversationComments     []ConversationComment `json:"conversation_comments"`
	ReviewThreads            []ReviewThread        `json:"review_threads"`
}

type PullRequestInfo struct {
	URL       string `json:"url"`
	Author    string `json:"author"`
	Mergeable string `json:"mergeable"`
}

type StatusFilter string

const (
	StatusAll        StatusFilter = "all"
	StatusResolved   StatusFilter = "resolved"
	StatusUnresolved StatusFilter = "unresolved"
)

type HistoricalSnippet struct {
	Commit        string   `json:"commit"`
	Path          string   `json:"path"`
	StartLine     int      `json:"start_line"`
	HighlightLine int      `json:"highlight_line"`
	Lines         []string `json:"lines"`
}

func BuildPayload(ctx Context, comments []ConversationComment, threads []ReviewThread) Payload {
	for i := range threads {
		threads[i].repoOwner = ctx.Owner
		threads[i].repoName = ctx.Repo
		threads[i].pullRequest = ctx.PullRequest
	}
	return Payload{
		Repository:               fmt.Sprintf("%s/%s", ctx.Owner, ctx.Repo),
		PullRequest:              ctx.PullRequest,
		ConversationCommentCount: len(comments),
		ReviewThreadCount:        len(threads),
		ConversationComments:     comments,
		ReviewThreads:            threads,
	}
}

func (t ReviewThread) RepoOwner() string { return t.repoOwner }
func (t ReviewThread) RepoName() string  { return t.repoName }
func (t ReviewThread) PullNumber() int   { return t.pullRequest }
