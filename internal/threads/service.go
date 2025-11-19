package threads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/VRTFinland/gh-threads/internal/ghcli"
	"github.com/VRTFinland/gh-threads/internal/gitlocal"
	"github.com/VRTFinland/gh-threads/internal/gitremote"
)

type Service struct {
	client            GitHubClient
	localRepo         *gitlocal.Repo
	remoteCache       *gitremote.Cache
	cache             Cache
	logWriter         io.Writer
	fetchConversation func(context.Context, Context) ([]ConversationComment, error)
	fetchReview       func(context.Context, Context, bool) ([]ReviewThread, error)
}

type GitHubClient interface {
	CallGraphQL(ctx context.Context, query string, variables map[string]string, target any) error
	HasIssueCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error)
	HasReviewCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error)
	PostREST(ctx context.Context, method, path string, body map[string]string, target any) error
}

func NewService(client GitHubClient, localRepo *gitlocal.Repo, cacheManager Cache, logWriter io.Writer) *Service {
	var remote *gitremote.Cache
	if real, ok := client.(*ghcli.Client); ok {
		remote = gitremote.New(real)
	}
	svc := &Service{
		client:      client,
		localRepo:   localRepo,
		remoteCache: remote,
		cache:       cacheManager,
		logWriter:   logWriter,
	}
	svc.fetchConversation = svc.FetchConversationComments
	svc.fetchReview = svc.FetchReviewThreads
	return svc
}

func (s *Service) logf(format string, args ...any) {
	if s.logWriter == nil {
		return
	}
	fmt.Fprintf(s.logWriter, format+"\n", args...)
}

const reviewThreadsQuery = `
query(
  $owner: String!
  $repo: String!
  $prNumber: Int!
  $cursor: String
) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $prNumber) {
      reviewThreads(first: 100, after: $cursor) {
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          originalLine
          startLine
          comments(first: 100) {
            nodes {
              id
              databaseId
              body
              createdAt
              updatedAt
              diffHunk
              line
              originalLine
              path
              startLine
              url
              author { login }
              originalCommit { oid }
            }
            pageInfo { hasNextPage endCursor }
          }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
`

const threadCommentsQuery = `
query($threadId: ID!, $cursor: String) {
  node(id: $threadId) {
    ... on PullRequestReviewThread {
      comments(first: 100, after: $cursor) {
        nodes {
          id
          databaseId
          body
          createdAt
          updatedAt
          diffHunk
          line
          originalLine
          path
          startLine
          url
          author { login }
          originalCommit { oid }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
`

const threadStatusQuery = `
query(
  $owner: String!
  $repo: String!
  $prNumber: Int!
  $cursor: String
) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $prNumber) {
      reviewThreads(first: 100, after: $cursor) {
        nodes {
          id
          isResolved
          isOutdated
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
`

const pullRequestCommentsQuery = `
query($owner: String!, $repo: String!, $prNumber: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $prNumber) {
      comments(first: 100, after: $cursor) {
        nodes {
          id
          body
          createdAt
          updatedAt
          url
          author { login }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
`

const pullRequestInfoQuery = `
query($owner: String!, $repo: String!, $prNumber: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $prNumber) {
      url
      mergeable
      author { login }
    }
  }
}
`

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

type ghComment struct {
	ID             string    `json:"id"`
	DatabaseID     int       `json:"databaseId"`
	Body           string    `json:"body"`
	CreatedAt      string    `json:"createdAt"`
	UpdatedAt      string    `json:"updatedAt"`
	DiffHunk       string    `json:"diffHunk"`
	Line           *int      `json:"line"`
	OriginalLine   *int      `json:"originalLine"`
	StartLine      *int      `json:"startLine"`
	Path           string    `json:"path"`
	URL            string    `json:"url"`
	Author         *ghAuthor `json:"author"`
	OriginalCommit struct {
		OID string `json:"oid"`
	} `json:"originalCommit"`
}

type ghThread struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"originalLine"`
	StartLine    *int   `json:"startLine"`
	IsResolved   bool   `json:"isResolved"`
	IsOutdated   bool   `json:"isOutdated"`
	Comments     struct {
		Nodes    []ghComment `json:"nodes"`
		PageInfo pageInfo    `json:"pageInfo"`
	} `json:"comments"`
}

type reviewThreadsData struct {
	Repository struct {
		PullRequest struct {
			ReviewThreads struct {
				Nodes    []ghThread `json:"nodes"`
				PageInfo pageInfo   `json:"pageInfo"`
			} `json:"reviewThreads"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type pullRequestCommentsData struct {
	Repository struct {
		PullRequest struct {
			Comments struct {
				Nodes []struct {
					ID        string    `json:"id"`
					Body      string    `json:"body"`
					CreatedAt string    `json:"createdAt"`
					UpdatedAt string    `json:"updatedAt"`
					URL       string    `json:"url"`
					Author    *ghAuthor `json:"author"`
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"comments"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type pullRequestInfoData struct {
	Repository struct {
		PullRequest struct {
			URL       string    `json:"url"`
			Mergeable string    `json:"mergeable"`
			Author    *ghAuthor `json:"author"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type threadCommentsData struct {
	Node struct {
		Comments struct {
			Nodes    []ghComment `json:"nodes"`
			PageInfo pageInfo    `json:"pageInfo"`
		} `json:"comments"`
	} `json:"node"`
}

type threadStatusData struct {
	Repository struct {
		PullRequest struct {
			ReviewThreads struct {
				Nodes []struct {
					ID         string `json:"id"`
					IsResolved bool   `json:"isResolved"`
					IsOutdated bool   `json:"isOutdated"`
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"reviewThreads"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

func (s *Service) FetchData(
	ctx context.Context,
	ghCtx Context,
	includeHistory bool,
	forceRefresh bool,
) ([]ConversationComment, []ReviewThread, error) {
	if forceRefresh {
		s.logf("Forced refresh requested; ignoring cache.")
	}
	var entry *Entry
	if s.cache != nil && !forceRefresh {
		var err error
		entry, err = s.cache.Load(ghCtx)
		if err != nil {
			s.logf("Warning: failed to load cache (%v).", err)
		}
	}

	conversation := make([]ConversationComment, 0)
	review := make([]ReviewThread, 0)

	if entry != nil {
		conversation = entry.ConversationComments
		review = entry.ReviewThreads
		injectRepoMetadata(review, ghCtx.Owner, ghCtx.Repo, ghCtx.PullRequest)
	}

	sinceConversation := ""
	sinceReview := ""
	if entry != nil {
		sinceConversation = entry.LastConversationSync
		sinceReview = entry.LastReviewSync
	}
	if sinceConversation == "" {
		sinceConversation = LatestTimestamp(conversation)
	}
	if sinceReview == "" {
		sinceReview = LatestThreadTimestamp(review)
	}
	sinceConversationQuery := nextTimestamp(sinceConversation)
	sinceReviewQuery := nextTimestamp(sinceReview)

	needConversation := entry == nil || sinceConversation == ""
	if !needConversation && sinceConversationQuery != "" {
		updated, err := s.client.HasIssueCommentUpdates(ctx, ghCtx.Owner, ghCtx.Repo, ghCtx.PullRequest, sinceConversationQuery)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check conversation updates: %w", err)
		}
		needConversation = updated
	}

	needReview := entry == nil || sinceReview == ""
	if !needReview && sinceReviewQuery != "" {
		updated, err := s.client.HasReviewCommentUpdates(ctx, ghCtx.Owner, ghCtx.Repo, ghCtx.PullRequest, sinceReviewQuery)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check review updates: %w", err)
		}
		needReview = updated
	}
	if !needReview && missingDatabaseIDs(review) {
		s.logf("Cached threads missing comment identifiers; refetching review threads.")
		needReview = true
	}

	if needConversation {
		s.logf("Fetching conversation comments...")
		comments, err := s.fetchConversation(ctx, ghCtx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch conversation comments: %w", err)
		}
		conversation = comments
		sinceConversation = LatestTimestamp(conversation)
	} else {
		s.logf("Using cached conversation comments.")
	}
	sinceConversation = time.Now().UTC().Format(time.RFC3339)

	if needReview {
		s.logf("Fetching review threads...")
		threads, err := s.fetchReview(ctx, ghCtx, includeHistory)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch review threads: %w", err)
		}
		review = threads
		sinceReview = LatestThreadTimestamp(review)
	} else {
		s.logf("No new review comments; refreshing thread statuses...")
		if err := s.refreshThreadStatuses(ctx, ghCtx, review); err != nil {
			return nil, nil, fmt.Errorf("failed to refresh thread statuses: %w", err)
		}
		if includeHistory {
			if err := s.attachHistoricalSnippets(ctx, ghCtx, review); err != nil {
				s.logf("Warning: failed to refresh snippets (%v).", err)
			}
		}
	}

	sinceReview = time.Now().UTC().Format(time.RFC3339)

	injectRepoMetadata(review, ghCtx.Owner, ghCtx.Repo, ghCtx.PullRequest)

	if s.cache != nil {
		saveEntry := &Entry{
			ConversationComments: conversation,
			ReviewThreads:        review,
			LastConversationSync: sinceConversation,
			LastReviewSync:       sinceReview,
			LastStatusSync:       time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.cache.Save(ghCtx, saveEntry); err != nil {
			s.logf("Warning: failed to save cache (%v).", err)
		}
	}

	return conversation, review, nil
}

func (s *Service) FetchReviewThreads(ctx context.Context, ghCtx Context, includeHistory bool) ([]ReviewThread, error) {
	threads := make([]ReviewThread, 0)
	var cursor string
	for {
		data := reviewThreadsData{}
		variables := map[string]string{
			"owner":    ghCtx.Owner,
			"repo":     ghCtx.Repo,
			"prNumber": strconv.Itoa(ghCtx.PullRequest),
			"cursor":   cursorValue(cursor),
		}
		if err := s.client.CallGraphQL(ctx, reviewThreadsQuery, variables, &data); err != nil {
			return nil, err
		}

		nodes := data.Repository.PullRequest.ReviewThreads.Nodes
		for _, node := range nodes {
			comments := make([]ThreadComment, 0, len(node.Comments.Nodes))
			for _, comment := range node.Comments.Nodes {
				comments = append(comments, convertComment(comment))
			}
			if node.Comments.PageInfo.HasNextPage {
				extra, err := s.paginateThreadComments(ctx, node.ID, node.Comments.PageInfo.EndCursor)
				if err != nil {
					return nil, err
				}
				comments = append(comments, extra...)
			}
			threads = append(threads, ReviewThread{
				ThreadID:     node.ID,
				Path:         node.Path,
				Line:         node.Line,
				OriginalLine: node.OriginalLine,
				StartLine:    node.StartLine,
				IsResolved:   node.IsResolved,
				IsOutdated:   node.IsOutdated,
				Comments:     comments,
			})
		}

		if !data.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		cursor = data.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor
	}

	if includeHistory {
		if err := s.attachHistoricalSnippets(ctx, ghCtx, threads); err != nil {
			return nil, err
		}
	}

	return threads, nil
}

func (s *Service) refreshThreadStatuses(ctx context.Context, ghCtx Context, threads []ReviewThread) error {
	if len(threads) == 0 {
		return nil
	}
	index := make(map[string]*ReviewThread, len(threads))
	for i := range threads {
		index[threads[i].ThreadID] = &threads[i]
	}

	var cursor string
	for {
		data := threadStatusData{}
		variables := map[string]string{
			"owner":    ghCtx.Owner,
			"repo":     ghCtx.Repo,
			"prNumber": strconv.Itoa(ghCtx.PullRequest),
			"cursor":   cursorValue(cursor),
		}
		if err := s.client.CallGraphQL(ctx, threadStatusQuery, variables, &data); err != nil {
			return err
		}
		nodes := data.Repository.PullRequest.ReviewThreads.Nodes
		for _, node := range nodes {
			if thread, ok := index[node.ID]; ok {
				thread.IsResolved = node.IsResolved
				thread.IsOutdated = node.IsOutdated
			}
		}
		page := data.Repository.PullRequest.ReviewThreads.PageInfo
		if !page.HasNextPage {
			break
		}
		cursor = page.EndCursor
	}
	return nil
}

func (s *Service) paginateThreadComments(ctx context.Context, threadID string, cursor string) ([]ThreadComment, error) {
	comments := make([]ThreadComment, 0)
	pageCursor := cursor
	for {
		data := threadCommentsData{}
		variables := map[string]string{
			"threadId": threadID,
			"cursor":   cursorValue(pageCursor),
		}
		if err := s.client.CallGraphQL(ctx, threadCommentsQuery, variables, &data); err != nil {
			return nil, err
		}
		nodes := data.Node.Comments.Nodes
		for _, node := range nodes {
			comments = append(comments, convertComment(node))
		}
		if !data.Node.Comments.PageInfo.HasNextPage {
			break
		}
		pageCursor = data.Node.Comments.PageInfo.EndCursor
	}
	return comments, nil
}

func (s *Service) FetchConversationComments(ctx context.Context, ghCtx Context) ([]ConversationComment, error) {
	comments := make([]ConversationComment, 0)
	var cursor string
	for {
		data := pullRequestCommentsData{}
		variables := map[string]string{
			"owner":    ghCtx.Owner,
			"repo":     ghCtx.Repo,
			"prNumber": strconv.Itoa(ghCtx.PullRequest),
			"cursor":   cursorValue(cursor),
		}
		if err := s.client.CallGraphQL(ctx, pullRequestCommentsQuery, variables, &data); err != nil {
			return nil, err
		}
		nodes := data.Repository.PullRequest.Comments.Nodes
		for _, node := range nodes {
			comments = append(comments, ConversationComment{
				ID:        node.ID,
				Author:    normaliseAuthor(node.Author),
				Body:      node.Body,
				CreatedAt: node.CreatedAt,
				UpdatedAt: node.UpdatedAt,
				URL:       node.URL,
			})
		}
		if !data.Repository.PullRequest.Comments.PageInfo.HasNextPage {
			break
		}
		cursor = data.Repository.PullRequest.Comments.PageInfo.EndCursor
	}
	return comments, nil
}

func (s *Service) FetchPullRequestInfo(ctx context.Context, ghCtx Context) (PullRequestInfo, error) {
	data := pullRequestInfoData{}
	variables := map[string]string{
		"owner":    ghCtx.Owner,
		"repo":     ghCtx.Repo,
		"prNumber": strconv.Itoa(ghCtx.PullRequest),
	}
	if err := s.client.CallGraphQL(ctx, pullRequestInfoQuery, variables, &data); err != nil {
		return PullRequestInfo{}, err
	}
	pr := data.Repository.PullRequest
	info := PullRequestInfo{
		URL:       pr.URL,
		Author:    normaliseAuthor(pr.Author),
		Mergeable: pr.Mergeable,
	}
	return info, nil
}

func convertComment(comment ghComment) ThreadComment {
	return ThreadComment{
		ID:           comment.ID,
		DatabaseID:   comment.DatabaseID,
		Author:       normaliseAuthor(comment.Author),
		Body:         comment.Body,
		CreatedAt:    comment.CreatedAt,
		UpdatedAt:    comment.UpdatedAt,
		Path:         comment.Path,
		Line:         comment.Line,
		OriginalLine: comment.OriginalLine,
		StartLine:    comment.StartLine,
		DiffHunk:     comment.DiffHunk,
		URL:          comment.URL,
		CommitSHA:    comment.OriginalCommit.OID,
	}
}

func normaliseAuthor(author *ghAuthor) string {
	if author == nil {
		return ""
	}
	return author.Login
}

func cursorValue(value string) string {
	if value == "" {
		return "null"
	}
	return value
}

const blobQuery = `
query($owner: String!, $repo: String!, $expression: String!) {
  repository(owner: $owner, name: $repo) {
    object(expression: $expression) {
      __typename
      ... on Blob {
        text
      }
    }
  }
}
`

type blobData struct {
	Repository struct {
		Object struct {
			Text string `json:"text"`
		} `json:"object"`
	} `json:"repository"`
}

type fileKey struct {
	commit string
	path   string
}

func (s *Service) attachHistoricalSnippets(ctx context.Context, ghCtx Context, threads []ReviewThread) error {
	cache := make(map[fileKey][]string)
	for threadIdx := range threads {
		for commentIdx := range threads[threadIdx].Comments {
			comment := &threads[threadIdx].Comments[commentIdx]
			if comment.Snippet != nil && len(comment.Snippet.Lines) > 0 {
				continue
			}
			linePtr := comment.OriginalLine
			if linePtr == nil {
				linePtr = comment.Line
			}
			if comment.CommitSHA == "" || comment.Path == "" || linePtr == nil {
				continue
			}
			key := fileKey{commit: comment.CommitSHA, path: comment.Path}
			lines, ok := cache[key]
			if !ok {
				var err error
				lines, err = s.fetchLocalOrRemote(ctx, ghCtx, key.commit, key.path)
				if err != nil {
					return err
				}
				cache[key] = lines
			}
			if len(lines) == 0 {
				continue
			}
			if snippet := buildSnippet(lines, *linePtr, comment.CommitSHA, comment.Path); snippet != nil {
				comment.Snippet = snippet
			}
		}
	}
	return nil
}

func (s *Service) fetchLocalOrRemote(ctx context.Context, ghCtx Context, commit, path string) ([]string, error) {
	if s.localRepo != nil && s.localRepo.Available() {
		lines, err := s.localRepo.FileLines(ctx, commit, path)
		if err == nil && len(lines) > 0 {
			return lines, nil
		}
	}
	if s.remoteCache != nil {
		lines, err := s.remoteCache.GetLines(ctx, ghCtx.Owner, ghCtx.Repo, commit, path)
		if err == nil && len(lines) > 0 {
			return lines, nil
		}
	}
	return s.fetchFileLines(ctx, ghCtx, commit, path)
}

func (s *Service) fetchFileLines(ctx context.Context, ghCtx Context, commit, path string) ([]string, error) {
	data := blobData{}
	expression := fmt.Sprintf("%s:%s", commit, path)
	variables := map[string]string{
		"owner":      ghCtx.Owner,
		"repo":       ghCtx.Repo,
		"expression": expression,
	}
	if err := s.client.CallGraphQL(ctx, blobQuery, variables, &data); err != nil {
		return nil, err
	}
	text := data.Repository.Object.Text
	if text == "" {
		return nil, nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n"), nil
}

type snippetBlock struct {
	startLine     int
	highlightLine int
	lines         []string
}

func buildSnippet(lines []string, targetLine int, commit, path string) *HistoricalSnippet {
	block := snippetAround(lines, targetLine)
	if block == nil {
		return nil
	}
	return &HistoricalSnippet{
		Commit:        commit,
		Path:          path,
		StartLine:     block.startLine,
		HighlightLine: block.highlightLine,
		Lines:         block.lines,
	}
}

func snippetAround(lines []string, targetLine int) *snippetBlock {
	if len(lines) == 0 {
		return nil
	}
	if targetLine < 1 {
		targetLine = 1
	}
	if targetLine > len(lines) {
		targetLine = len(lines)
	}
	index := targetLine - 1
	start := index - 7
	if start < 0 {
		start = 0
	}
	end := index + 8
	if end > len(lines) {
		end = len(lines)
	}
	block := make([]string, end-start)
	copy(block, lines[start:end])
	return &snippetBlock{
		startLine:     start + 1,
		highlightLine: targetLine,
		lines:         block,
	}
}

func injectRepoMetadata(threads []ReviewThread, owner, repo string, prNumber int) {
	for i := range threads {
		threads[i].repoOwner = owner
		threads[i].repoName = repo
		threads[i].pullRequest = prNumber
	}
}

func missingDatabaseIDs(threads []ReviewThread) bool {
	for _, thread := range threads {
		for _, comment := range thread.Comments {
			if comment.DatabaseID == 0 {
				return true
			}
		}
	}
	return false
}

const (
	resolveThreadMutation   = "mutation($threadId: ID!) { resolveReviewThread(input:{threadId:$threadId}) { thread { isResolved } } }"
	unresolveThreadMutation = "mutation($threadId: ID!) { unresolveReviewThread(input:{threadId:$threadId}) { thread { isResolved } } }"
)

func (s *Service) ReplyToThread(ctx context.Context, thread *ReviewThread, commentIdx int, body string) (*ThreadComment, error) {
	if thread == nil {
		return nil, errors.New("thread is nil")
	}
	if len(thread.Comments) == 0 {
		return nil, errors.New("thread has no comments to reply to")
	}
	if commentIdx < 0 || commentIdx >= len(thread.Comments) {
		commentIdx = len(thread.Comments) - 1
	}
	targetComment := thread.Comments[commentIdx]
	if targetComment.DatabaseID == 0 {
		return nil, errors.New("missing database ID for target comment")
	}

	owner, repo := thread.repoOwner, thread.repoName
	if owner == "" || repo == "" {
		return nil, errors.New("missing repository metadata for thread")
	}

	path := fmt.Sprintf("repos/%s/%s/pulls/%d/comments/%d/replies", owner, repo, thread.pullRequest, targetComment.DatabaseID)
	var resp struct {
		ID        int64  `json:"id"`
		NodeID    string `json:"node_id"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		URL       string `json:"html_url"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := s.client.PostREST(ctx, "POST", path, map[string]string{"body": body}, &resp); err != nil {
		return nil, err
	}

	comment := ThreadComment{
		ID:         resp.NodeID,
		DatabaseID: int(resp.ID),
		Author:     resp.User.Login,
		Body:       resp.Body,
		CreatedAt:  resp.CreatedAt,
		UpdatedAt:  resp.UpdatedAt,
		URL:        resp.URL,
	}
	thread.Comments = append(thread.Comments, comment)
	return &comment, nil
}

func (s *Service) SetThreadStatus(ctx context.Context, thread *ReviewThread, resolved bool) error {
	if thread == nil {
		return errors.New("thread is nil")
	}
	query := resolveThreadMutation
	if !resolved {
		query = unresolveThreadMutation
	}
	if err := s.client.CallGraphQL(ctx, query, map[string]string{"threadId": thread.ThreadID}, &struct{}{}); err != nil {
		return err
	}
	thread.IsResolved = resolved
	return nil
}
