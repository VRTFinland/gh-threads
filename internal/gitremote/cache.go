package gitremote

import (
	"context"
	"errors"
)

// FileLinesFetcher is the subset of the GitHub client the cache needs.
type FileLinesFetcher interface {
	FileLines(ctx context.Context, owner, repo, commit, path string) ([]string, error)
}

type Cache struct {
	client  FileLinesFetcher
	entries map[fileKey]cacheEntry
}

type fileKey struct {
	commit string
	path   string
}

type cacheEntry struct {
	lines []string
	found bool
}

func New(client FileLinesFetcher) *Cache {
	return &Cache{
		client:  client,
		entries: make(map[fileKey]cacheEntry),
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
	if entry, ok := c.entries[key]; ok {
		return entry.lines, entry.found, nil
	}

	fetched, err := c.client.FileLines(ctx, owner, repo, commit, path)
	if err != nil {
		return nil, false, err
	}
	entry := cacheEntry{lines: fetched, found: len(fetched) > 0}
	c.entries[key] = entry
	return entry.lines, entry.found, nil
}
