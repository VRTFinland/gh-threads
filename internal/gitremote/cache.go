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

// Block is the run of lines cut around one target line of a file.
type Block struct {
	StartLine     int
	HighlightLine int
	Lines         []string
}

// Cache turns remote files into the small blocks its callers actually display.
// A fetched file is cut and then dropped: keeping it would retain a whole
// source file, often tens of kilobytes, to serve the dozen or so lines around
// one comment, for as long as the process lives.
type Cache struct {
	client FileLinesFetcher
	radius int

	mu sync.Mutex
	// blocks holds the cut context per requested line, missing the files that
	// do not exist at that commit. Both survive for the process lifetime; see
	// Blocks for why errors do not.
	blocks  map[blockKey]Block
	missing map[FileKey]bool
}

// FileKey identifies one blob: a path as it stood at one commit. Callers group
// their work by it before fetching, so it is exported rather than mirrored.
type FileKey struct {
	Commit string
	Path   string
}

type blockKey struct {
	file FileKey
	line int
}

// New returns a cache that cuts radius lines of context on either side of every
// requested line. The radius is fixed for the cache's lifetime because blocks
// are cached per line: a caller that varied it would be served the first cut.
func New(client FileLinesFetcher, radius int) *Cache {
	if radius < 0 {
		radius = 0
	}
	return &Cache{
		client:  client,
		radius:  radius,
		blocks:  make(map[blockKey]Block),
		missing: make(map[FileKey]bool),
	}
}

// Blocks returns the cut context for each requested line of one file. Lines
// with no block are absent from the result: the file may not exist at that
// commit, which is not an error and is remembered so a missing file is fetched
// once per process. Errors are deliberately not cached, since a rate-limited or
// interrupted fetch should be retried on the next refresh.
func (c *Cache) Blocks(ctx context.Context, owner, repo, commit, path string, lines []int) (map[int]Block, error) {
	if commit == "" || path == "" {
		return nil, errors.New("invalid commit or path")
	}
	file := FileKey{Commit: commit, Path: path}

	found := make(map[int]Block, len(lines))
	missing := c.cached(file, lines, found)
	if len(missing) == 0 {
		return found, nil
	}

	// Fetch outside the lock: holding it across a subprocess and a network round
	// trip would queue concurrent callers back into a single file. Two callers
	// racing on the same key would duplicate one fetch, which callers avoid by
	// deduplicating keys before fanning out.
	content, err := c.client.FileLines(ctx, owner, repo, commit, path)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(content) == 0 {
		c.missing[file] = true
		return found, nil
	}
	for _, line := range missing {
		block, ok := Cut(content, line, c.radius)
		if !ok {
			continue
		}
		c.blocks[blockKey{file: file, line: line}] = block
		found[line] = block
	}
	return found, nil
}

// cached fills found from memory and returns the lines still to be fetched.
func (c *Cache) cached(file FileKey, lines []int, found map[int]Block) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.missing[file] {
		return nil
	}
	var missing []int
	for _, line := range lines {
		if block, ok := c.blocks[blockKey{file: file, line: line}]; ok {
			found[line] = block
			continue
		}
		missing = append(missing, line)
	}
	return missing
}

// Cut returns the lines around target with radius lines of context on either
// side. A target outside the file is clamped to it, so a stale line number
// still yields context; an empty file yields no block.
func Cut(lines []string, target, radius int) (Block, bool) {
	if len(lines) == 0 {
		return Block{}, false
	}
	if target < 1 {
		target = 1
	}
	if target > len(lines) {
		target = len(lines)
	}
	index := target - 1
	start := max(index-radius, 0)
	end := min(index+radius+1, len(lines))
	block := make([]string, end-start)
	copy(block, lines[start:end])
	return Block{
		StartLine:     start + 1,
		HighlightLine: target,
		Lines:         block,
	}, true
}
