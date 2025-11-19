package threads

import (
	"context"
	"errors"
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
	issueUpdates  bool
	reviewUpdates bool
	graphQLCalls  []string
	restPath      string
	restBody      map[string]string
}

func (f *fakeClient) CallGraphQL(ctx context.Context, query string, variables map[string]string, target any) error {
	f.graphQLCalls = append(f.graphQLCalls, query)
	return nil
}
func (f *fakeClient) HasIssueCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error) {
	return f.issueUpdates, nil
}
func (f *fakeClient) HasReviewCommentUpdates(ctx context.Context, owner, repo string, prNumber int, since string) (bool, error) {
	return f.reviewUpdates, nil
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
