package gitremote

import (
	"context"
	"errors"
	"testing"
)

type fakeFetcher struct {
	calls []string
	fn    func(commit, path string) ([]string, error)
}

func (f *fakeFetcher) FileLines(ctx context.Context, owner, repo, commit, path string) ([]string, error) {
	f.calls = append(f.calls, commit+":"+path)
	if f.fn != nil {
		return f.fn(commit, path)
	}
	return nil, nil
}

func TestGetLinesCachesNegativeResult(t *testing.T) {
	fetcher := &fakeFetcher{fn: func(string, string) ([]string, error) { return nil, nil }}
	cache := New(fetcher)

	for i := 0; i < 3; i++ {
		lines, found, err := cache.GetLines(context.Background(), "o", "r", "abc", "gone.go")
		if err != nil {
			t.Fatalf("a missing blob is not an error, got %v", err)
		}
		if found || lines != nil {
			t.Fatalf("expected found=false for a missing blob, got found=%v lines=%v", found, lines)
		}
	}

	if len(fetcher.calls) != 1 {
		t.Fatalf("expected the negative result to be cached, got %d calls", len(fetcher.calls))
	}
}

func TestGetLinesDoesNotCacheErrors(t *testing.T) {
	fetcher := &fakeFetcher{fn: func(string, string) ([]string, error) {
		return nil, errors.New("API rate limit exceeded")
	}}
	cache := New(fetcher)

	for i := 0; i < 2; i++ {
		if _, _, err := cache.GetLines(context.Background(), "o", "r", "abc", "file.go"); err == nil {
			t.Fatal("expected the fetch error to propagate")
		}
	}

	if len(fetcher.calls) != 2 {
		t.Fatalf("a transient error must be retried, got %d calls", len(fetcher.calls))
	}
}

func TestGetLinesCachesPositiveResultPerKey(t *testing.T) {
	fetcher := &fakeFetcher{fn: func(commit, path string) ([]string, error) {
		return []string{path + "@" + commit}, nil
	}}
	cache := New(fetcher)

	first, found, err := cache.GetLines(context.Background(), "o", "r", "abc", "a.go")
	if err != nil || !found || first[0] != "a.go@abc" {
		t.Fatalf("unexpected first result: %v %v %v", first, found, err)
	}
	if _, _, err := cache.GetLines(context.Background(), "o", "r", "abc", "a.go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	other, _, err := cache.GetLines(context.Background(), "o", "r", "def", "a.go")
	if err != nil || other[0] != "a.go@def" {
		t.Fatalf("distinct commits must not collide, got %v", other)
	}

	if len(fetcher.calls) != 2 {
		t.Fatalf("expected one fetch per distinct key, got %d calls: %v", len(fetcher.calls), fetcher.calls)
	}
}

func TestGetLinesRejectsEmptyCommitOrPath(t *testing.T) {
	fetcher := &fakeFetcher{}
	cache := New(fetcher)

	if _, _, err := cache.GetLines(context.Background(), "o", "r", "", "a.go"); err == nil {
		t.Fatal("expected an error for an empty commit")
	}
	if _, _, err := cache.GetLines(context.Background(), "o", "r", "abc", ""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
	if len(fetcher.calls) != 0 {
		t.Fatalf("expected no fetch for invalid input, got %v", fetcher.calls)
	}
}
