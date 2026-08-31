package threads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/VRTFinland/gh-threads/internal/gitlocal"
	"github.com/VRTFinland/gh-threads/internal/gitremote"
)

type Service struct {
	client            GitHubClient
	localRepo         *gitlocal.Repo
	remoteCache       *gitremote.Cache
	cache             Cache
	logMu             sync.Mutex
	logWriter         io.Writer
	fetchConversation func(context.Context, Context) ([]ConversationComment, error)
	fetchReview       func(context.Context, Context, bool) ([]ReviewThread, error)
}

type GitHubClient interface {
	CallGraphQL(ctx context.Context, query string, variables map[string]string, target any) error
	FileLines(ctx context.Context, owner, repo, commit, path string) ([]string, error)
	HasIssueCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error)
	HasReviewCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error)
	PostREST(ctx context.Context, method, path string, body map[string]string, target any) error
}

func NewService(client GitHubClient, localRepo *gitlocal.Repo, cacheManager Cache, logWriter io.Writer) *Service {
	svc := &Service{
		client:      client,
		localRepo:   localRepo,
		remoteCache: gitremote.New(client, snippetRadius),
		cache:       cacheManager,
		logWriter:   logWriter,
	}
	svc.fetchConversation = svc.FetchConversationComments
	svc.fetchReview = svc.FetchReviewThreads
	return svc
}

// SetLogWriter redirects diagnostic output. The interactive UI owns the
// terminal via the alternate screen, so writing warnings to stderr while it
// runs would corrupt the rendering; callers mute the service for its duration.
func (s *Service) SetLogWriter(w io.Writer) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.logWriter = w
}

func (s *Service) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
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
          originalStartLine
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
              originalStartLine
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
          originalStartLine
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
	ID                string    `json:"id"`
	DatabaseID        int       `json:"databaseId"`
	Body              string    `json:"body"`
	CreatedAt         string    `json:"createdAt"`
	UpdatedAt         string    `json:"updatedAt"`
	DiffHunk          string    `json:"diffHunk"`
	Line              *int      `json:"line"`
	OriginalLine      *int      `json:"originalLine"`
	StartLine         *int      `json:"startLine"`
	OriginalStartLine *int      `json:"originalStartLine"`
	Path              string    `json:"path"`
	URL               string    `json:"url"`
	Author            *ghAuthor `json:"author"`
	OriginalCommit    struct {
		OID string `json:"oid"`
	} `json:"originalCommit"`
}

type ghThread struct {
	ID                string `json:"id"`
	Path              string `json:"path"`
	Line              *int   `json:"line"`
	OriginalLine      *int   `json:"originalLine"`
	StartLine         *int   `json:"startLine"`
	OriginalStartLine *int   `json:"originalStartLine"`
	IsResolved        bool   `json:"isResolved"`
	IsOutdated        bool   `json:"isOutdated"`
	Comments          struct {
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
	needReview := entry == nil || sinceReview == ""
	checkConversation := !needConversation && sinceConversationQuery != ""
	checkReview := !needReview && sinceReviewQuery != ""

	// Two independent probes, one gh round trip each, on the path every cached
	// run takes. They write separate variables, so only the Wait below is
	// needed to publish them.
	if checkConversation || checkReview {
		group, groupCtx := errgroup.WithContext(ctx)
		if checkConversation {
			group.Go(func() error {
				updated, err := s.client.HasIssueCommentUpdates(groupCtx, ghCtx.Owner, ghCtx.Repo, ghCtx.PullRequest, sinceConversationQuery)
				if err != nil {
					return fmt.Errorf("failed to check conversation updates: %w", err)
				}
				needConversation = updated
				return nil
			})
		}
		if checkReview {
			group.Go(func() error {
				updated, err := s.client.HasReviewCommentUpdates(groupCtx, ghCtx.Owner, ghCtx.Repo, ghCtx.PullRequest, sinceReviewQuery)
				if err != nil {
					return fmt.Errorf("failed to check review updates: %w", err)
				}
				needReview = updated
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return nil, nil, err
		}
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
			s.attachHistoricalSnippets(ctx, ghCtx, review)
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
				ThreadID:          node.ID,
				Path:              node.Path,
				Line:              node.Line,
				OriginalLine:      node.OriginalLine,
				StartLine:         node.StartLine,
				OriginalStartLine: node.OriginalStartLine,
				IsResolved:        node.IsResolved,
				IsOutdated:        node.IsOutdated,
				Comments:          comments,
			})
		}

		if !data.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		cursor = data.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor
	}

	if includeHistory {
		s.attachHistoricalSnippets(ctx, ghCtx, threads)
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
		ID:                comment.ID,
		DatabaseID:        comment.DatabaseID,
		Author:            normaliseAuthor(comment.Author),
		Body:              comment.Body,
		CreatedAt:         comment.CreatedAt,
		UpdatedAt:         comment.UpdatedAt,
		Path:              comment.Path,
		Line:              comment.Line,
		OriginalLine:      comment.OriginalLine,
		StartLine:         comment.StartLine,
		OriginalStartLine: comment.OriginalStartLine,
		DiffHunk:          comment.DiffHunk,
		URL:               comment.URL,
		CommitSHA:         comment.OriginalCommit.OID,
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

// blobFetchConcurrency bounds the fan-out. GitHub's secondary rate limits
// discourage many concurrent requests, so this is deliberately modest.
const blobFetchConcurrency = 8

// snippetRadius is how many lines of context a snippet carries on either side
// of the commented line.
const snippetRadius = 7

type blobResult struct {
	blocks map[int]gitremote.Block
	err    error
}

// snippetTarget is one comment waiting for the file its snippet is cut from.
type snippetTarget struct {
	comment *ThreadComment
	line    int
	key     gitremote.FileKey
}

// fileRequest is one file together with every line a comment anchors to in it,
// so the file is read once and cut for all of them.
type fileRequest struct {
	key   gitremote.FileKey
	lines []int
}

// attachHistoricalSnippets attaches code context to comments. Snippets are
// decoration: a failed fetch degrades the output but is never fatal, so the
// number of (commit, path) pairs that could not be read is reported alongside
// the first error instead of aborting the whole command.
func (s *Service) attachHistoricalSnippets(ctx context.Context, ghCtx Context, threads []ReviewThread) {
	targets, requests := collectSnippetTargets(threads)
	if len(requests) == 0 {
		return
	}

	// Fetch phase. Each file is a gh subprocess plus a network round trip, and
	// they are independent, so they run concurrently. Every goroutine owns one
	// result slot, which means nothing here is shared and nothing needs a lock.
	// Errors are recorded rather than returned: a snippet is decoration, and an
	// errgroup context would cancel the siblings of the first failure.
	results := make([]blobResult, len(requests))
	var g errgroup.Group
	g.SetLimit(blobFetchConcurrency)
	for i, request := range requests {
		g.Go(func() error {
			blocks, err := s.fetchLocalOrRemote(ctx, ghCtx, request)
			results[i] = blobResult{blocks: blocks, err: err}
			return nil
		})
	}
	_ = g.Wait()

	// Build phase, back on one goroutine: comment.Snippet writes need no lock,
	// and walking the results in request order keeps the reported error the same
	// from run to run.
	blocks := make(map[gitremote.FileKey]map[int]gitremote.Block, len(requests))
	var (
		failed   int
		firstErr error
	)
	for i, request := range requests {
		if err := results[i].err; err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		blocks[request.key] = results[i].blocks
	}
	for _, target := range targets {
		block, ok := blocks[target.key][target.line]
		if !ok {
			continue
		}
		target.comment.Snippet = &HistoricalSnippet{
			Commit:        target.key.Commit,
			Path:          target.key.Path,
			StartLine:     block.StartLine,
			HighlightLine: block.HighlightLine,
			Lines:         block.Lines,
		}
	}

	if failed > 0 {
		s.logf("Warning: could not load code context for %d file(s): %v", failed, firstErr)
	}
}

// collectSnippetTargets pairs every comment that still needs a snippet with the
// file it is cut from, and returns the distinct files in encounter order, each
// carrying its distinct lines, so the fetch fans out over each file exactly
// once.
func collectSnippetTargets(threads []ReviewThread) ([]snippetTarget, []fileRequest) {
	var (
		targets  []snippetTarget
		requests []fileRequest
	)
	index := make(map[gitremote.FileKey]int)
	type fileLine struct {
		key  gitremote.FileKey
		line int
	}
	seen := make(map[fileLine]bool)
	for threadIdx := range threads {
		for commentIdx := range threads[threadIdx].Comments {
			comment := &threads[threadIdx].Comments[commentIdx]
			if comment.Snippet != nil && len(comment.Snippet.Lines) > 0 {
				continue
			}
			// The snippet is cut in SnippetSpace, so the line it is centred on
			// must be read in that space too.
			anchor := comment.Anchor(SnippetSpace)
			if comment.CommitSHA == "" || comment.Path == "" || !anchor.Valid() {
				continue
			}
			key := gitremote.FileKey{Commit: comment.CommitSHA, Path: comment.Path}
			targets = append(targets, snippetTarget{comment: comment, line: anchor.End, key: key})

			at, ok := index[key]
			if !ok {
				at = len(requests)
				index[key] = at
				requests = append(requests, fileRequest{key: key})
			}
			if a := (fileLine{key: key, line: anchor.End}); !seen[a] {
				seen[a] = true
				requests[at].lines = append(requests[at].lines, anchor.End)
			}
		}
	}
	return targets, requests
}

// fetchLocalOrRemote cuts one block per requested line of a file, preferring a
// local checkout of the commit over a remote read.
func (s *Service) fetchLocalOrRemote(ctx context.Context, ghCtx Context, request fileRequest) (map[int]gitremote.Block, error) {
	if s.localRepo != nil && s.localRepo.Available() {
		if lines, err := s.localRepo.FileLines(ctx, request.key.Commit, request.key.Path); err == nil && len(lines) > 0 {
			return cutAll(lines, request.lines), nil
		}
	}
	// A file absent at this commit comes back as an empty result, not an error:
	// retrying the same query would only repeat the miss.
	return s.remoteCache.Blocks(ctx, ghCtx.Owner, ghCtx.Repo, request.key.Commit, request.key.Path, request.lines)
}

func cutAll(content []string, lines []int) map[int]gitremote.Block {
	blocks := make(map[int]gitremote.Block, len(lines))
	for _, line := range lines {
		if block, ok := gitremote.Cut(content, line, snippetRadius); ok {
			blocks[line] = block
		}
	}
	return blocks
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
