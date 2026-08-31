package gitremote

import (
	"context"
	"errors"
	"strings"
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

func TestBlocksCachesNegativeResult(t *testing.T) {
	fetcher := &fakeFetcher{fn: func(string, string) ([]string, error) { return nil, nil }}
	cache := New(fetcher, 7)

	for i := 0; i < 3; i++ {
		blocks, err := cache.Blocks(context.Background(), "o", "r", "abc", "gone.go", []int{1, 2})
		if err != nil {
			t.Fatalf("a missing blob is not an error, got %v", err)
		}
		if len(blocks) != 0 {
			t.Fatalf("expected no blocks for a missing blob, got %v", blocks)
		}
	}

	if len(fetcher.calls) != 1 {
		t.Fatalf("expected the negative result to be cached, got %d calls", len(fetcher.calls))
	}
}

func TestBlocksDoesNotCacheErrors(t *testing.T) {
	fetcher := &fakeFetcher{fn: func(string, string) ([]string, error) {
		return nil, errors.New("API rate limit exceeded")
	}}
	cache := New(fetcher, 7)

	for i := 0; i < 2; i++ {
		if _, err := cache.Blocks(context.Background(), "o", "r", "abc", "file.go", []int{1}); err == nil {
			t.Fatal("expected the fetch error to propagate")
		}
	}

	if len(fetcher.calls) != 2 {
		t.Fatalf("a transient error must be retried, got %d calls", len(fetcher.calls))
	}
}

func TestBlocksCachesPerKeyAndLine(t *testing.T) {
	fetcher := &fakeFetcher{fn: func(commit, path string) ([]string, error) {
		return []string{path + "@" + commit, "second", "third"}, nil
	}}
	cache := New(fetcher, 0)
	ctx := context.Background()

	first, err := cache.Blocks(ctx, "o", "r", "abc", "a.go", []int{1})
	if err != nil || first[1].Lines[0] != "a.go@abc" {
		t.Fatalf("unexpected first result: %v %v", first, err)
	}
	if _, err := cache.Blocks(ctx, "o", "r", "abc", "a.go", []int{1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fetcher.calls) != 1 {
		t.Fatalf("expected a cached line to be served from memory, got %v", fetcher.calls)
	}

	if _, err := cache.Blocks(ctx, "o", "r", "abc", "a.go", []int{1, 3}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fetcher.calls) != 2 {
		t.Fatalf("expected an uncached line to be fetched, got %v", fetcher.calls)
	}

	other, err := cache.Blocks(ctx, "o", "r", "def", "a.go", []int{1})
	if err != nil || other[1].Lines[0] != "a.go@def" {
		t.Fatalf("distinct commits must not collide, got %v", other)
	}
}

func TestBlocksRejectsEmptyCommitOrPath(t *testing.T) {
	fetcher := &fakeFetcher{}
	cache := New(fetcher, 7)

	if _, err := cache.Blocks(context.Background(), "o", "r", "", "a.go", []int{1}); err == nil {
		t.Fatal("expected an error for an empty commit")
	}
	if _, err := cache.Blocks(context.Background(), "o", "r", "abc", "", []int{1}); err == nil {
		t.Fatal("expected an error for an empty path")
	}
	if len(fetcher.calls) != 0 {
		t.Fatalf("expected no fetch for invalid input, got %v", fetcher.calls)
	}
}

func TestBlocksRetainOnlyTheCutLines(t *testing.T) {
	file := make([]string, 2000)
	for i := range file {
		file[i] = strings.Repeat("x", 80)
	}
	fetcher := &fakeFetcher{fn: func(string, string) ([]string, error) { return file, nil }}
	cache := New(fetcher, 7)

	blocks, err := cache.Blocks(context.Background(), "o", "r", "abc", "big.go", []int{100, 900})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected a block per requested line, got %d", len(blocks))
	}

	var retained int
	for _, block := range cache.blocks {
		for _, line := range block.Lines {
			retained += len(line)
		}
	}
	if retained > 2*15*81 {
		t.Fatalf("the cache retained %d bytes, which is more than the cut blocks need", retained)
	}
}

func TestCutClampsAndCentresTheTarget(t *testing.T) {
	lines := []string{"1", "2", "3", "4", "5"}

	block, ok := Cut(lines, 3, 1)
	if !ok || block.StartLine != 2 || block.HighlightLine != 3 || len(block.Lines) != 3 {
		t.Fatalf("expected lines 2-4 around 3, got %+v", block)
	}

	block, _ = Cut(lines, 1, 2)
	if block.StartLine != 1 || len(block.Lines) != 3 {
		t.Fatalf("expected the block to stop at the start of the file, got %+v", block)
	}

	block, _ = Cut(lines, 99, 1)
	if block.HighlightLine != 5 || block.StartLine != 4 {
		t.Fatalf("expected a past-the-end target to clamp to the last line, got %+v", block)
	}

	if _, ok := Cut(nil, 1, 7); ok {
		t.Fatal("expected no block for an empty file")
	}
}
