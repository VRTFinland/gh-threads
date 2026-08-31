package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
)

type stubCache struct {
	entry      *Entry
	saveCalled bool
}

func (s *stubCache) Load(Context) (*Entry, error) {
	return s.entry, nil
}

func (s *stubCache) Save(Context, *Entry) error {
	s.saveCalled = true
	return nil
}

type fakeClient struct {
	issueUpdates   bool
	reviewUpdates  bool
	graphQLCalls   []string
	restPath       string
	restBody       map[string]string
	fileLinesCalls []string
	fileLinesFn    func(commit, path string) ([]string, error)
	graphQLJSON    string
}

func (f *fakeClient) CallGraphQL(ctx context.Context, query string, variables map[string]string, target any) error {
	f.graphQLCalls = append(f.graphQLCalls, query)
	if f.graphQLJSON != "" && target != nil {
		return json.Unmarshal([]byte(f.graphQLJSON), target)
	}
	return nil
}
func (f *fakeClient) HasIssueCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error) {
	return f.issueUpdates, nil
}
func (f *fakeClient) HasReviewCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error) {
	return f.reviewUpdates, nil
}

func (f *fakeClient) FileLines(ctx context.Context, owner, repo, commit, path string) ([]string, error) {
	f.fileLinesCalls = append(f.fileLinesCalls, commit+":"+path)
	if f.fileLinesFn != nil {
		return f.fileLinesFn(commit, path)
	}
	return nil, nil
}

func TestFetchDataUsesCacheWhenNoUpdates(t *testing.T) {
	ctx := context.Background()
	cacheEntry := &Entry{
		ConversationComments: []ConversationComment{{ID: "c1", Body: "cached", CreatedAt: "2025-05-01T12:00:00Z"}},
		ReviewThreads:        []ReviewThread{{ThreadID: "t1", Comments: []ThreadComment{{ID: "rc1", DatabaseID: 99, CreatedAt: "2025-05-01T12:00:00Z"}}}},
	}
	svc := &Service{
		client: &fakeClient{issueUpdates: false, reviewUpdates: false},
		cache:  &stubCache{entry: cacheEntry},
		fetchConversation: func(context.Context, Context) ([]ConversationComment, error) {
			return nil, errors.New("should not fetch conversation")
		},
		fetchReview: func(context.Context, Context, bool) ([]ReviewThread, error) {
			return nil, errors.New("should not fetch review")
		},
	}

	gotConversation, gotReview, err := svc.FetchData(ctx, Context{Owner: "o", Repo: "r", PullRequest: 1}, false, false)
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if len(gotConversation) != 1 || gotConversation[0].ID != "c1" {
		t.Fatalf("expected cached conversation, got %+v", gotConversation)
	}
	if len(gotReview) != 1 || gotReview[0].ThreadID != "t1" {
		t.Fatalf("expected cached review, got %+v", gotReview)
	}
}

func TestFetchDataRefetchesWhenUpdatesDetected(t *testing.T) {
	ctx := context.Background()
	cacheEntry := &Entry{}
	convoFetched := false
	reviewFetched := false
	svc := &Service{
		client: &fakeClient{issueUpdates: true, reviewUpdates: true},
		cache:  &stubCache{entry: cacheEntry},
		fetchConversation: func(context.Context, Context) ([]ConversationComment, error) {
			convoFetched = true
			return []ConversationComment{{ID: "c2"}}, nil
		},
		fetchReview: func(context.Context, Context, bool) ([]ReviewThread, error) {
			reviewFetched = true
			return []ReviewThread{{ThreadID: "t2"}}, nil
		},
	}

	gotConversation, gotReview, err := svc.FetchData(ctx, Context{Owner: "o", Repo: "r", PullRequest: 1}, false, false)
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if !convoFetched || !reviewFetched {
		t.Fatalf("expected fetch functions to be called")
	}
	if len(gotConversation) != 1 || gotConversation[0].ID != "c2" {
		t.Fatalf("expected new conversation, got %+v", gotConversation)
	}
	if len(gotReview) != 1 || gotReview[0].ThreadID != "t2" {
		t.Fatalf("expected new review, got %+v", gotReview)
	}
}

func TestFetchDataRefetchesWhenDatabaseIDsMissing(t *testing.T) {
	ctx := context.Background()
	cacheEntry := &Entry{
		ConversationComments: []ConversationComment{{ID: "c1", Body: "cached", CreatedAt: "2025-05-01T12:00:00Z"}},
		ReviewThreads: []ReviewThread{{
			ThreadID: "t-missing",
			Comments: []ThreadComment{{ID: "rc1", CreatedAt: "2025-05-01T12:00:00Z"}},
		}},
	}
	reviewFetched := false
	svc := &Service{
		client: &fakeClient{issueUpdates: false, reviewUpdates: false},
		cache:  &stubCache{entry: cacheEntry},
		fetchConversation: func(context.Context, Context) ([]ConversationComment, error) {
			return nil, errors.New("should not fetch conversation")
		},
		fetchReview: func(context.Context, Context, bool) ([]ReviewThread, error) {
			reviewFetched = true
			return []ReviewThread{{
				ThreadID: "t-missing",
				Comments: []ThreadComment{{ID: "rc1", DatabaseID: 123, CreatedAt: "2025-05-02T12:00:00Z"}},
			}}, nil
		},
	}

	_, gotReview, err := svc.FetchData(ctx, Context{Owner: "o", Repo: "r", PullRequest: 1}, false, false)
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if !reviewFetched {
		t.Fatalf("expected review fetch due to missing database IDs")
	}
	if len(gotReview) != 1 || len(gotReview[0].Comments) != 1 || gotReview[0].Comments[0].DatabaseID != 123 {
		t.Fatalf("expected fetched review to include database IDs, got %+v", gotReview)
	}
}

func TestFetchDataInjectsRepoMetadata(t *testing.T) {
	ctx := context.Background()
	svc := &Service{
		client: &fakeClient{issueUpdates: true, reviewUpdates: true},
		cache:  &stubCache{},
		fetchConversation: func(context.Context, Context) ([]ConversationComment, error) {
			return []ConversationComment{}, nil
		},
		fetchReview: func(context.Context, Context, bool) ([]ReviewThread, error) {
			return []ReviewThread{{ThreadID: "t3"}}, nil
		},
	}

	_, gotReview, err := svc.FetchData(ctx, Context{Owner: "o", Repo: "r", PullRequest: 42}, false, false)
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if len(gotReview) != 1 {
		t.Fatalf("expected one review thread, got %+v", gotReview)
	}
	thread := gotReview[0]
	if thread.RepoOwner() != "o" || thread.RepoName() != "r" || thread.PullNumber() != 42 {
		t.Fatalf("expected repo metadata to be injected, got owner=%s repo=%s pr=%d", thread.RepoOwner(), thread.RepoName(), thread.PullNumber())
	}
}

func TestReplyToThreadUsesDatabaseID(t *testing.T) {
	ctx := context.Background()
	fake := &fakeClient{}
	svc := &Service{
		client: fake,
	}
	thread := &ReviewThread{
		ThreadID: "t4",
		Comments: []ThreadComment{
			{ID: "c-global", DatabaseID: 987, Author: "alice"},
		},
		repoOwner:   "o",
		repoName:    "r",
		pullRequest: 5,
	}

	if _, err := svc.ReplyToThread(ctx, thread, 0, "hello"); err != nil {
		t.Fatalf("ReplyToThread returned error: %v", err)
	}
	expectedPath := "repos/o/r/pulls/5/comments/987/replies"
	if fake.restPath != expectedPath {
		t.Fatalf("expected REST path %s, got %s", expectedPath, fake.restPath)
	}
	if fake.restBody["body"] != "hello" {
		t.Fatalf("expected body to be posted, got %+v", fake.restBody)
	}
}
func (f *fakeClient) PostREST(ctx context.Context, method, path string, body map[string]string, target any) error {
	f.restPath = path
	f.restBody = body
	return nil
}

func threadWithComments(n int, commit, path string) []ReviewThread {
	comments := make([]ThreadComment, 0, n)
	for i := 0; i < n; i++ {
		line := i + 1
		comments = append(comments, ThreadComment{
			ID:           "c" + strconv.Itoa(i),
			CommitSHA:    commit,
			Path:         path,
			OriginalLine: &line,
		})
	}
	return []ReviewThread{{ThreadID: "t1", Path: path, Comments: comments}}
}

func TestFetchLocalOrRemoteQueriesMissingFileOnce(t *testing.T) {
	fake := &fakeClient{fileLinesFn: func(string, string) ([]string, error) { return nil, nil }}
	svc := NewService(fake, nil, &stubCache{}, io.Discard)
	reviewThreads := threadWithComments(5, "abc", "gone.go")

	svc.attachHistoricalSnippets(context.Background(), Context{Owner: "o", Repo: "r"}, reviewThreads)

	if got := len(fake.fileLinesCalls); got != 1 {
		t.Fatalf("expected a missing file to be fetched once, got %d calls: %v", got, fake.fileLinesCalls)
	}
}

func TestFetchLocalOrRemoteReusesCacheAcrossFetches(t *testing.T) {
	fake := &fakeClient{fileLinesFn: func(string, string) ([]string, error) { return nil, nil }}
	svc := NewService(fake, nil, &stubCache{}, io.Discard)
	ghCtx := Context{Owner: "o", Repo: "r"}

	for i := 0; i < 3; i++ {
		svc.attachHistoricalSnippets(context.Background(), ghCtx, threadWithComments(2, "abc", "gone.go"))
	}

	if got := len(fake.fileLinesCalls); got != 1 {
		t.Fatalf("expected the negative result to be cached across refreshes, got %d calls", got)
	}
}

func TestAttachHistoricalSnippetsSkipsMissingBlobWithoutError(t *testing.T) {
	logs := &bytes.Buffer{}
	fake := &fakeClient{fileLinesFn: func(string, string) ([]string, error) { return nil, nil }}
	svc := NewService(fake, nil, &stubCache{}, logs)
	reviewThreads := threadWithComments(1, "abc", "gone.go")

	svc.attachHistoricalSnippets(context.Background(), Context{Owner: "o", Repo: "r"}, reviewThreads)

	if logs.Len() != 0 {
		t.Fatalf("a missing blob is not a failure and must not warn, got %q", logs.String())
	}
	if reviewThreads[0].Comments[0].Snippet != nil {
		t.Fatalf("expected no snippet for a missing blob")
	}
}

func TestAttachHistoricalSnippetsSurvivesFetchFailure(t *testing.T) {
	fake := &fakeClient{fileLinesFn: func(commit, path string) ([]string, error) {
		if path == "bad.go" {
			return nil, errors.New("API rate limit exceeded")
		}
		return []string{"one", "two", "three"}, nil
	}}
	logs := &bytes.Buffer{}
	svc := NewService(fake, nil, &stubCache{}, logs)
	line := 2
	reviewThreads := []ReviewThread{{ThreadID: "t1", Comments: []ThreadComment{
		{ID: "bad", CommitSHA: "abc", Path: "bad.go", OriginalLine: &line},
		{ID: "good", CommitSHA: "abc", Path: "good.go", OriginalLine: &line},
	}}}

	svc.attachHistoricalSnippets(context.Background(), Context{Owner: "o", Repo: "r"}, reviewThreads)

	if !strings.Contains(logs.String(), "could not load code context for 1 file(s)") {
		t.Fatalf("expected the failure to be reported once, got %q", logs.String())
	}
	if reviewThreads[0].Comments[0].Snippet != nil {
		t.Fatalf("expected no snippet for the failed file")
	}
	if reviewThreads[0].Comments[1].Snippet == nil {
		t.Fatalf("expected the healthy file to still get a snippet")
	}
}

func TestFetchReviewThreadsSurvivesSnippetFailure(t *testing.T) {
	logs := &bytes.Buffer{}
	fake := &fakeClient{
		graphQLJSON: `{"repository":{"pullRequest":{"reviewThreads":{
			"nodes":[{"id":"t1","path":"file.go","comments":{"nodes":[
				{"id":"c1","body":"a comment","originalLine":2,"path":"file.go",
				 "originalCommit":{"oid":"abc"}}
			],"pageInfo":{"hasNextPage":false}}}],
			"pageInfo":{"hasNextPage":false}}}}}`,
		fileLinesFn: func(string, string) ([]string, error) {
			return nil, errors.New("API rate limit exceeded")
		},
	}
	svc := NewService(fake, nil, &stubCache{}, logs)

	result, err := svc.FetchReviewThreads(context.Background(), Context{Owner: "o", Repo: "r"}, true)

	if err != nil {
		t.Fatalf("a snippet failure must not abort the command, got %v", err)
	}
	if len(result) != 1 || len(result[0].Comments) != 1 {
		t.Fatalf("expected the thread and its comment to survive, got %+v", result)
	}
	if result[0].Comments[0].Body != "a comment" {
		t.Fatalf("expected the comment body to survive, got %q", result[0].Comments[0].Body)
	}
	if result[0].Comments[0].Snippet != nil {
		t.Fatalf("expected no snippet after a failed fetch")
	}
	if !strings.Contains(logs.String(), "Warning") {
		t.Fatalf("expected a warning to be logged, got %q", logs.String())
	}
}

func TestFetchReviewThreadsRequestsAndMapsOriginalStartLine(t *testing.T) {
	fake := &fakeClient{
		graphQLJSON: `{"repository":{"pullRequest":{"reviewThreads":{
			"nodes":[{"id":"t1","path":"file.go","comments":{"nodes":[
				{"id":"c1","body":"b","path":"file.go",
				 "line":118,"startLine":116,
				 "originalLine":120,"originalStartLine":115}
			],"pageInfo":{"hasNextPage":false}}}],
			"pageInfo":{"hasNextPage":false}}}}}`,
	}
	svc := NewService(fake, nil, &stubCache{}, io.Discard)

	result, err := svc.FetchReviewThreads(context.Background(), Context{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, query := range fake.graphQLCalls {
		if !strings.Contains(query, "originalStartLine") {
			t.Fatalf("expected the query to request originalStartLine, got:\n%s", query)
		}
	}
	comment := result[0].Comments[0]
	if comment.OriginalStartLine == nil || *comment.OriginalStartLine != 115 {
		t.Fatalf("expected originalStartLine to be mapped, got %v", comment.OriginalStartLine)
	}
	if comment.StartLine == nil || *comment.StartLine != 116 {
		t.Fatalf("expected startLine to still be mapped, got %v", comment.StartLine)
	}
}

func TestCacheVersionRejectsOlderEntries(t *testing.T) {
	if cacheVersion < 2 {
		t.Fatalf("cacheVersion must be bumped so entries without OriginalStartLine are dropped, got %d", cacheVersion)
	}
}
