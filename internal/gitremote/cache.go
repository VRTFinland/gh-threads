package gitremote

import (
	"context"
	"errors"
	"sync"
)

// FileLinesFetcher is the subset of the GitHub client the cache needs.
type FileLinesFetcher interface {
	FileLines(ctx context.Context, owner, repo, commit, path string) ([]string, error)
}

type Cache struct {
	client FileLinesFetcher

	mu      sync.Mutex
	entries map[fileKey][]string
}

type fileKey struct {
	commit string
	path   string
}

func New(client FileLinesFetcher) *Cache {
	return &Cache{
		client:  client,
		entries: make(map[fileKey][]string),
	}
}

// GetLines reports found=false, with a nil error, when the blob does not exist
// at that commit. The negative result is cached so a missing file is fetched
// once per process; errors are deliberately not cached, since a rate-limited or
// interrupted fetch should be retried on the next refresh.
func (c *Cache) GetLines(ctx context.Context, owner, repo, commit, path string) (lines []string, found bool, err error) {
	if commit == "" || path == "" {
		return nil, false, errors.New("invalid commit or path")
	}
	key := fileKey{commit: commit, path: path}
	c.mu.Lock()
	cached, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		return cached, len(cached) > 0, nil
	}

	// Fetch outside the lock: holding it across a subprocess and a network round
	// trip would queue concurrent callers back into a single file. Two callers
	// racing on the same key would duplicate one fetch, which callers avoid by
	// deduplicating keys before fanning out.
	lines, err = c.client.FileLines(ctx, owner, repo, commit, path)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	c.entries[key] = lines
	c.mu.Unlock()
	return lines, len(lines) > 0, nil
}
